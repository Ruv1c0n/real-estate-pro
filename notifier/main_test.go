// Тесты HTTP-эндпоинтов /health и /health/ready (ФТ-3.7).
//
// Redis нужен настоящий: go-redis при подключении шлёт HELLO, ответ которого
// фейковым TCP-сервером надёжно не повторить. Как и в Pg-тестах
// (NOTIFIER_TEST_DSN), тесты с живым Redis скипаются в CI — там Redis нет.
// Адрес берётся из NOTIFIER_TEST_REDIS_ADDR (по умолчанию localhost:6379,
// как в docker-compose).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"

	"notifier/internal/dicts"
	"notifier/internal/metrics"
	"notifier/internal/notify"
	"notifier/internal/store"
	"notifier/internal/stream"
)

// newDictsFake поднимает HTTP-заглушку Core API: GET /api/v1/countries
// отвечает пустым справочником — Healthy() становится true.
func newDictsFake(t *testing.T) *dicts.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	return dicts.New(srv.URL)
}

// testRedisAddr возвращает адрес живого Redis или скипает тест.
func testRedisAddr(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("NOTIFIER_TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	pctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rdb.Ping(pctx).Err(); err != nil {
		t.Cleanup(func() { rdb.Close() })
		t.Skip("живой Redis недоступен — тесты /health скипнуты (в CI это норма)")
	}
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

// testPool даёт пул к живому Postgres из NOTIFIER_TEST_DSN (тот же контракт,
// что у Pg-тестов в internal/notify) или скипает тест.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("NOTIFIER_TEST_DSN")
	if dsn == "" {
		t.Skip("NOTIFIER_TEST_DSN не задан — db-тесты /health/ready пропущены (в CI это норма)")
	}
	pctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pool, err := pgxpool.New(pctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	return pool
}

// decodeHealth читает тело ответа как карту полей.
func decodeHealth(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("тело ответа не JSON: %v", err)
	}
	return body
}

// /health: всегда 200, поле db присутствует, прежние поля не сломаны.
// dicts в liveness берётся из кэша клиента без сети, поэтому перед запросом
// кэш прогревается одним сетевым вызовом к заглушке.
func TestHealthAlwaysOkWithDbField(t *testing.T) {
	rdb := testRedisAddr(t)
	dictsClient := newDictsFake(t)

	// Прогрев кэша справочников (см. TestHealthLivenessNoNetwork).
	wctx, wcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer wcancel()
	if !dictsClient.Healthy(wctx) {
		t.Fatal("заглушка Core API должна быть здорова при прогреве кэша")
	}

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog, defaultDebugRateLimitPerMin) // memory-режим: pool == nil
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/health вернул %d, ожидается 200 (liveness)", rec.Code)
	}
	body := decodeHealth(t, rec)
	if body["status"] != "ok" || body["redis"] != true || body["dicts"] != true {
		t.Fatalf("неожиданное тело /health: %v", body)
	}
	// memory-режим: пул отсутствует, db — false, но это не валит /health.
	if body["db"] != false {
		t.Fatalf("поле db в memory-режиме должно быть false: %v", body["db"])
	}
	if _, ok := body["mode"]; !ok {
		t.Fatal("поле mode пропало из /health")
	}
}

// /health (liveness) не ходит в Core API по сети: при пустом кэше клиента
// dicts в теле false, ответ остаётся 200, и ни одного запроса к Core API
// не происходит. Живой Redis не нужен: redis:false не мешает 200 liveness.
func TestHealthLivenessNoNetwork(t *testing.T) {
	// Redis на «мёртвом» порту: ping вернёт false, liveness остаётся 200.
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { rdb.Close() })

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	t.Cleanup(srv.Close)
	dictsClient := dicts.New(srv.URL) // кэш не прогрет: CachedHealthy() == false

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog, defaultDebugRateLimitPerMin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/health при пустом кэше dicts вернул %d, ожидается 200 (liveness)", rec.Code)
	}
	if body := decodeHealth(t, rec); body["dicts"] != false || body["status"] != "degraded" {
		t.Fatalf("dicts в /health по пустому кэшу должен быть false, тело: %v", body)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("/health сделал %d сетевых вызовов к Core API, ожидается 0 (liveness без сети)", n)
	}
}

// /health/ready в memory-режиме: redis+dicts ок → 200, db:false не мешает.
func TestHealthReadyMemoryModeOk(t *testing.T) {
	rdb := testRedisAddr(t)
	dictsClient := newDictsFake(t)

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog, defaultDebugRateLimitPerMin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/health/ready в memory-режиме вернул %d, ожидается 200", rec.Code)
	}
	body := decodeHealth(t, rec)
	if body["db"] != false || body["status"] != "ok" {
		t.Fatalf("неожиданное тело /health/ready: %v", body)
	}
}

// /health/ready в db-режиме: redis+dicts+db живы → 200.
func TestHealthReadyDbModeOk(t *testing.T) {
	rdb := testRedisAddr(t)
	dictsClient := newDictsFake(t)
	pool := testPool(t)
	defer pool.Close()

	mux := newMux(rdb, dictsClient, pool, notify.ModeLog, defaultDebugRateLimitPerMin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/health/ready в db-режиме вернул %d, ожидается 200", rec.Code)
	}
	if body := decodeHealth(t, rec); body["db"] != true {
		t.Fatalf("поле db в db-режиме должно быть true: %v", body)
	}
}

// /health/ready в db-режиме при недоступной БД: 503, status degraded, db:false.
// Пул закрывается сразу после создания: ping по закрытому пулу падает
// детерминированно и без второй БД — случай «Postgres умер после старта».
func TestHealthReadyDbDown(t *testing.T) {
	rdb := testRedisAddr(t)
	dictsClient := newDictsFake(t)
	pool := testPool(t)
	pool.Close()

	mux := newMux(rdb, dictsClient, pool, notify.ModeLog, defaultDebugRateLimitPerMin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/health/ready при закрытом пуле БД вернул %d, ожидается 503", rec.Code)
	}
	if body := decodeHealth(t, rec); body["db"] != false || body["status"] != "degraded" {
		t.Fatalf("неожиданное тело /health/ready при db down: %v", body)
	}
}

// /health/ready: Redis недоступен → 503; /health на том же mux — 200 со
// статусом degraded в теле (тест не зависит от Redis).
func TestHealthReadyRedisDown(t *testing.T) {
	dictsClient := newDictsFake(t)

	// Redis на «мёртвом» порту: соединение сразу отклоняется.
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { rdb.Close() })

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog, defaultDebugRateLimitPerMin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/health/ready при недоступном Redis вернул %d, ожидается 503", rec.Code)
	}
	if body := decodeHealth(t, rec); body["redis"] != false || body["status"] != "degraded" {
		t.Fatalf("неожиданное тело /health/ready при Redis down: %v", body)
	}

	// Liveness на том же mux: код всегда 200, но статус отражает деградацию.
	// Кэши ready/live раздельные — результат readiness не подмешивается к
	// liveness, повторный probe выполняется заново.
	recLive := httptest.NewRecorder()
	mux.ServeHTTP(recLive, httptest.NewRequest(http.MethodGet, "/health", nil))

	if recLive.Code != http.StatusOK {
		t.Fatalf("/health при недоступном Redis вернул %d, ожидается 200 (liveness)", recLive.Code)
	}
	if body := decodeHealth(t, recLive); body["status"] != "degraded" || body["redis"] != false {
		t.Fatalf("неожиданное тело /health при Redis down: %v", body)
	}
}

// buildStores в memory-режиме (pool == nil): все четыре реализации — memory-*.
// Полностью офлайн, живая БД не нужна.
func TestBuildStoresMemoryMode(t *testing.T) {
	subs, journal, limiter, queue := buildStores(nil, 30)

	if _, ok := subs.(*store.Memory); !ok {
		t.Fatalf("subs в memory-режиме: %T, ожидается *store.Memory", subs)
	}
	if _, ok := journal.(*notify.MemoryJournal); !ok {
		t.Fatalf("journal в memory-режиме: %T, ожидается *notify.MemoryJournal", journal)
	}
	if _, ok := limiter.(*notify.MemoryLimiter); !ok {
		t.Fatalf("limiter в memory-режиме: %T, ожидается *notify.MemoryLimiter", limiter)
	}
	if _, ok := queue.(*notify.MemoryQueue); !ok {
		t.Fatalf("queue в memory-режиме: %T, ожидается *notify.MemoryQueue", queue)
	}
}

// buildStores в db-режиме обязан вернуть Pg-стек по всем четырём точкам:
// wiring «memory-реализации при живом пуле» — класс бага #51, уехавшего на
// e2e-прогоне T8. pgxpool.Pool — конкретный тип без реализации, фейковый пул
// построить нельзя, поэтому Pg-ветка проверяется против живого Postgres из
// NOTIFIER_TEST_DSN (в CI без DSN скипается — это норма).
func TestBuildStoresDbModePgStack(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()

	subs, journal, limiter, queue := buildStores(pool, 30)

	if _, ok := subs.(*store.Postgres); !ok {
		t.Fatalf("subs в db-режиме: %T, ожидается *store.Postgres", subs)
	}
	if _, ok := journal.(*notify.PgJournal); !ok {
		t.Fatalf("journal в db-режиме: %T, ожидается *notify.PgJournal", journal)
	}
	if _, ok := limiter.(*notify.PgLimiter); !ok {
		t.Fatalf("limiter в db-режиме: %T, ожидается *notify.PgLimiter", limiter)
	}
	if _, ok := queue.(*notify.PgQueue); !ok {
		t.Fatalf("queue в db-режиме: %T, ожидается *notify.PgQueue", queue)
	}
}

// Тесты env-параметров (ТЗ 3.6 «настраиваются»): дефолты, валидные значения,
// fail-fast на мусор и неположительные числа.
func TestEnvPositiveInt(t *testing.T) {
	t.Setenv("NOTIFIER_X", "42")
	if v, err := envPositiveInt("NOTIFIER_X", 30); err != nil || v != 42 {
		t.Fatalf("envPositiveInt = %d, %v; want 42, nil", v, err)
	}

	if v, err := envPositiveInt("NOTIFIER_MISSING", 30); err != nil || v != 30 {
		t.Fatalf("дефолт = %d, %v; want 30, nil", v, err)
	}

	t.Setenv("NOTIFIER_JUNK", "abc")
	if _, err := envPositiveInt("NOTIFIER_JUNK", 30); err == nil {
		t.Fatal("мусорное значение должно давать ошибку (fail-fast)")
	}
	t.Setenv("NOTIFIER_ZERO", "0")
	if _, err := envPositiveInt("NOTIFIER_ZERO", 30); err == nil {
		t.Fatal("0 должно давать ошибку (fail-fast)")
	}
	t.Setenv("NOTIFIER_NEG", "-3")
	if _, err := envPositiveInt("NOTIFIER_NEG", 30); err == nil {
		t.Fatal("отрицательное значение должно давать ошибку (fail-fast)")
	}
}

func TestEnvPositiveDuration(t *testing.T) {
	t.Setenv("NOTIFIER_T", "90s")
	if v, err := envPositiveDuration("NOTIFIER_T", 5*time.Minute); err != nil || v != 90*time.Second {
		t.Fatalf("envPositiveDuration = %s, %v; want 1m30s, nil", v, err)
	}
	if v, err := envPositiveDuration("NOTIFIER_MISSING", 5*time.Minute); err != nil || v != 5*time.Minute {
		t.Fatalf("дефолт = %s, %v; want 5m, nil", v, err)
	}
	t.Setenv("NOTIFIER_JUNK", "five")
	if _, err := envPositiveDuration("NOTIFIER_JUNK", 5*time.Minute); err == nil {
		t.Fatal("мусорное значение должно давать ошибку (fail-fast)")
	}
}

// Тесты /debug/event (НФТ-3): fail-closed без ключа, логирование отказов
// авторизации и rate limit per-IP до авторизации (аудит 20.11, P2-25 —
// брутфорс X-Admin-Key раньше был бесплатным и молчаливым).

func TestDebugEventDisabledWithoutAdminKey(t *testing.T) {
	t.Setenv("ADMIN_KEY", "")
	dictsClient := newDictsFake(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { rdb.Close() })

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog, defaultDebugRateLimitPerMin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/debug/event", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("пустой ADMIN_KEY выключает эндпоинт: %d, ожидается 404", rec.Code)
	}
}

func TestDebugEventUnauthorizedAttemptIsLogged(t *testing.T) {
	t.Setenv("ADMIN_KEY", "correct-key")
	dictsClient := newDictsFake(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { rdb.Close() })

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog, defaultDebugRateLimitPerMin)

	// Захват стандартного логера: смысл правки в том, что отказ
	// авторизации больше не молчаливый — проверяем содержимое лога.
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	req := httptest.NewRequest(http.MethodPost, "/debug/event", nil)
	req.Header.Set("X-Admin-Key", "wrong-key")
	req.RemoteAddr = "192.0.2.33:4444"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("неверный ключ: %d, ожидается 401", rec.Code)
	}
	if !strings.Contains(buf.String(), "unauthorized attempt") || !strings.Contains(buf.String(), "192.0.2.33") {
		t.Fatalf("отказ авторизации не в логе или без IP: %q", buf.String())
	}
}

func TestDebugEventRateLimitedBeforeAuth(t *testing.T) {
	t.Setenv("ADMIN_KEY", "correct-key")
	dictsClient := newDictsFake(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { rdb.Close() })

	// Лимит 2/мин на IP: первые два запроса доходят до авторизации (401 —
	// ключ неверный), третий упирается в bucket ещё до constant-time
	// compare. Заодно фиксируем, что неудачные авторизации не обходят
	// лимит — они тратят те же токены.
	mux := newMux(rdb, dictsClient, nil, notify.ModeLog, 2)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/debug/event", nil)
		req.Header.Set("X-Admin-Key", "wrong")
		req.RemoteAddr = "192.0.2.44:1000"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("запрос %d: %d, ожидается 401 (bucket ещё не пуст)", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/debug/event", nil)
	req.Header.Set("X-Admin-Key", "wrong")
	req.RemoteAddr = "192.0.2.44:1000"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("третий запрос: %d, ожидается 429 (лимит 2/мин)", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Fatal("429 без Retry-After: клиент не узнаёт, когда повторять")
	}
}

func TestDebugEventPublishesToStream(t *testing.T) {
	t.Setenv("ADMIN_KEY", "correct-key")
	dictsClient := newDictsFake(t)
	rdb := testRedisAddr(t) // скип без живого Redis

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog, defaultDebugRateLimitPerMin)

	body := `{"event_id":"550e8400-e29b-41d4-a716-446655440000","id":9001,"source":"mock","deal_type":"rent","country":"BY","city":"Minsk","rooms":2,"price":380,"currency":"USD","price_usd":380,"address":"ул. Тестовая, 1","url":"https://example.com/listing/9001","created_at":"2026-11-20T10:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/debug/event", strings.NewReader(body))
	req.Header.Set("X-Admin-Key", "correct-key")
	req.RemoteAddr = "192.0.2.50:1111"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("валидное событие с ключом: %d, ожидается 202; тело: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp["published"] != "true" || resp["stream_id"] == "" {
		t.Fatalf("неожиданное тело 202: %q (err=%v)", rec.Body.String(), err)
	}

	// Прибрать за собой: /debug/event кладёт запись в общий стрим, и
	// оставленная демо-запись ломает ожидания соседних тестов по XLEN.
	if err := rdb.XDel(context.Background(), stream.StreamName, resp["stream_id"]).Err(); err != nil {
		t.Fatalf("XDel тестовой записи: %v", err)
	}
}

// parseLevel — уровень NOTIFIER_LOG_LEVEL: распознаёт 4 уровня без учёта
// регистра, мусор — fail-fast (ТЗ 3.6, как остальные параметры).
func TestParseLevel(t *testing.T) {
	for raw, want := range map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"ERROR":   slog.LevelError,
		"":        slog.LevelInfo,
	} {
		got, err := parseLevel(raw)
		if err != nil || got != want {
			t.Fatalf("parseLevel(%q) = %v, %v; want %v, nil", raw, got, err, want)
		}
	}
	if _, err := parseLevel("verbose"); err == nil {
		t.Fatal("мусорный уровень должен давать ошибку (fail-fast)")
	}
}

// Тесты /metrics (карточка 01 плана prod-10of10): эндпоинт на mux отдаёт
// пром-формат; поллер гейджей переживает недоступный Redis и обновляет
// значения на живом.

func TestMetricsEndpointOnMux(t *testing.T) {
	t.Setenv("ADMIN_KEY", "")
	dictsClient := newDictsFake(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { rdb.Close() })

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog, defaultDebugRateLimitPerMin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics: %d, ожидается 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "notifier_stream_length") {
		t.Fatal("в /metrics нет наших метрик notifier_*")
	}
}

func TestPollOnceToleratesDeadRedis(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { rdb.Close() })

	// Ошибки Redis не паникуют и не поднимают гейджи: значения остаются
	// с прошлого прохода (в тесте — нулевые).
	pollOnce(context.Background(), rdb, nil)
	if got := testutil.ToFloat64(metrics.StreamLength); got != 0 {
		t.Fatalf("StreamLength после сбоя Redis = %v, want 0", got)
	}
}

func TestPollOnceSetsGauges(t *testing.T) {
	rdb := testRedisAddr(t) // скип без живого Redis

	// Наполняем стрим тремя записями и проверяем, что XLEN попал в гейдж.
	ctx := context.Background()
	ids := make([]string, 0, 3)
	t.Cleanup(func() { rdb.XDel(ctx, stream.StreamName, ids...) })
	for i := 0; i < 3; i++ {
		id, err := rdb.XAdd(ctx, &redis.XAddArgs{
			Stream: stream.StreamName,
			Values: map[string]any{"data": `{"event_id":"550e8400-e29b-41d4-a716-446655440000","id":1,"source":"mock","deal_type":"rent","country":"BY","city":"Minsk","rooms":1,"price":1,"currency":"USD","price_usd":1,"url":"https://example.com/1","created_at":"2026-11-20T10:00:00Z"}`},
		}).Result()
		if err != nil {
			t.Fatalf("XAdd: %v", err)
		}
		ids = append(ids, id)
	}

	pollOnce(ctx, rdb, nil)
	if got := testutil.ToFloat64(metrics.StreamLength); got < 3 {
		t.Fatalf("StreamLength = %v, ожидалось >= 3 (стрим общий, могли остаться чужие записи)", got)
	}
}
