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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"notifier/internal/dicts"
	"notifier/internal/notify"
	"notifier/internal/store"
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
// авторизации, rate limit per-IP до авторизации (аудит 20.11, P2-25 —
// брутфорс X-Admin-Key раньше был бесплатным и молчаливым) и прогрессивный
// бан за перебор ключа. Publish-путь эндпоинта здесь не проверяется —
// он не менялся этим PR и покрыт своими интеграционными тестами.

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

	// Захват slog-логера через прод-JSON-хендлер (карточка 02): смысл правки
	// в том, что отказ авторизации больше не молчаливый — фиксируем прод-формат
	// записи с client_ip, на который могут опираться алерты.
	buf := captureSlog(t)

	req := httptest.NewRequest(http.MethodPost, "/debug/event", nil)
	req.Header.Set("X-Admin-Key", "wrong-key")
	req.RemoteAddr = "192.0.2.33:4444"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("неверный ключ: %d, ожидается 401", rec.Code)
	}
	logged := slogRecord(t, buf, "unauthorized attempt")
	if logged["client_ip"] != "192.0.2.33" {
		t.Fatalf("в логе client_ip = %v, ожидается 192.0.2.33: %v", logged["client_ip"], logged)
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
	if ra := rec.Header().Get("Retry-After"); ra != "30" {
		t.Fatalf("Retry-After = %q, ожидается 30 (ceil(60/2): лимит 2/мин)", ra)
	}
}

// Прогрессивный бан за брутфорс (п.1 ревью #123): 5 неудач авторизации
// подряд от одного IP — и шестая попытка получает 429 раньше токен-бакета
// и авторизации. Лимит 100/мин гарантирует, что bucket не пуст: отказ
// может быть только баном, а Retry-After — оставшееся время свежего
// 5-минутного бана.
func TestDebugEventBruteForceTriggersBan(t *testing.T) {
	t.Setenv("ADMIN_KEY", "correct-key")
	dictsClient := newDictsFake(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { rdb.Close() })

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog, 100)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/debug/event", nil)
		req.Header.Set("X-Admin-Key", "wrong")
		req.RemoteAddr = "192.0.2.90:1000"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("попытка %d: %d, ожидается 401 (бан включается на 5-й)", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/debug/event", nil)
	req.Header.Set("X-Admin-Key", "wrong")
	req.RemoteAddr = "192.0.2.90:1000"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("шестая попытка после 5 неудач: %d, ожидается 429 (прогрессивный бан)", rec.Code)
	}
	ra, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || ra < 250 || ra > 301 {
		t.Fatalf("Retry-After = %q (%d, %v), ожидается оставшееся время 5-минутного бана", rec.Header().Get("Retry-After"), ra, err)
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

// captureSlog подменяет стандартный slog-логер на JSON-хендлер с записью в
// буфер — тот же формат, что в проде (main ставит JSONHandler на stdout):
// тесты проверяют прод-формат, а не fallback-рендеринг дефолтного хендлера.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// slogRecord декодирует первую JSON-запись лога, msg которой содержит part.
func slogRecord(t *testing.T, buf *bytes.Buffer, part string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(buf.String(), "\n") {
		if !strings.Contains(line, part) {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("строка лога не JSON: %v: %q", err, line)
		}
		return rec
	}
	t.Fatalf("в логе нет записи с %q: %q", part, buf.String())
	return nil
}

// Тесты requestLogger (карточка 02, флот-ревью): эхо/генерация request-id
// и JSON-лог завершения запроса со статусом. Содержимое лога берётся из
// прод-JSON-хендлера, а не из fallback-рендеринга дефолтного.

// Входящий X-Request-ID возвращается в ответе как есть — сквозной trace
// может начинаться с клиента. Граница капа: ровно 64 печатных символа
// (validRequestID) принимаются как есть.
func TestRequestLoggerEchoesIncomingID(t *testing.T) {
	captureSlog(t) // лог завершения уходит в буфер, здесь не проверяется
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-ID", "client-trace-42")
	rec := httptest.NewRecorder()
	requestLogger(handler).ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "client-trace-42" {
		t.Fatalf("X-Request-ID в ответе = %q, ожидается эхо входящего %q", got, "client-trace-42")
	}

	req64 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req64.Header.Set("X-Request-ID", strings.Repeat("a", 64))
	rec64 := httptest.NewRecorder()
	requestLogger(handler).ServeHTTP(rec64, req64)

	if got := rec64.Header().Get("X-Request-ID"); got != strings.Repeat("a", 64) {
		t.Fatalf("X-Request-ID из 64 печатных символов заменён: %q (кап не должен резать границу)", got)
	}
}

// Без заголовка ответ содержит непустой сгенерированный id (16-hex).
func TestRequestLoggerGeneratesID(t *testing.T) {
	captureSlog(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	rec := httptest.NewRecorder()
	requestLogger(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	rid := rec.Header().Get("X-Request-ID")
	if rid == "" {
		t.Fatal("X-Request-ID в ответе пуст — id не сгенерирован")
	}
	if matched, err := regexp.MatchString(`^[0-9a-f]{16}$`, rid); err != nil || !matched {
		t.Fatalf("X-Request-ID = %q, ожидается сгенерированный 16-hex (%v)", rid, err)
	}
}

// Handler с WriteHeader(500): статус попадает в JSON-лог завершения запроса.
func TestRequestLoggerLogsStatus(t *testing.T) {
	buf := captureSlog(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	rec := httptest.NewRecorder()
	requestLogger(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/event", nil))

	logged := slogRecord(t, buf, "http request")
	if got, ok := logged["status"].(float64); !ok || got != 500 {
		t.Fatalf("в логе status = %v, ожидается 500", logged["status"])
	}
}

// Кап X-Request-ID (флот-ревью): слишком длинный id, id с непечатаемыми
// символами или кавычками заменяется сгенерированным 16-hex — входящее
// значение не должно раздувать строку лога (лимит 1 МБ) или менять её
// структуру инъекцией.
func TestRequestLoggerRejectsMalformedID(t *testing.T) {
	captureSlog(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	for name, rid := range map[string]string{
		"слишком длинный":    strings.Repeat("a", 100),
		"управляющий символ": "bad\x01char",
		"с кавычкой":         `id"quote`,
	} {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("X-Request-ID", rid)
		rec := httptest.NewRecorder()
		requestLogger(handler).ServeHTTP(rec, req)

		got := rec.Header().Get("X-Request-ID")
		if got == "" || got == rid {
			t.Fatalf("%s: X-Request-ID в ответе = %q, ожидается сгенерированный", name, got)
		}
		if matched, err := regexp.MatchString(`^[0-9a-f]{16}$`, got); err != nil || !matched {
			t.Fatalf("%s: X-Request-ID = %q, ожидается сгенерированный 16-hex (%v)", name, got, err)
		}
	}
}
