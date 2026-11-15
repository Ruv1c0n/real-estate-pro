// Тесты HTTP-эндпоинтов /health и /health/ready (ФТ-3.7).
//
// Redis нужен настоящий: go-redis при подключении шлёт HELLO, ответ которого
// фейковым TCP-сервером надёжно не повторить. Как и в Pg-тестах
// (NOTIFIER_TEST_DSN), тесты с живым Redis скипаются в CI — там Redis нет.
// Адрес берётся из NOTIFIER_TEST_REDIS_ADDR (по умолчанию localhost:6379,
// как в docker-compose).
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"notifier/internal/dicts"
	"notifier/internal/notify"
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

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog) // memory-режим: pool == nil
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

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog)
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

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog)
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

	mux := newMux(rdb, dictsClient, pool, notify.ModeLog)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/health/ready в db-режиме вернул %d, ожидается 200", rec.Code)
	}
	if body := decodeHealth(t, rec); body["db"] != true {
		t.Fatalf("поле db в db-режиме должно быть true: %v", body)
	}
}

// /health/ready: Redis недоступен → 503 (тест не зависит от Redis).
func TestHealthReadyRedisDown(t *testing.T) {
	dictsClient := newDictsFake(t)

	// Redis на «мёртвом» порту: соединение сразу отклоняется.
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { rdb.Close() })

	mux := newMux(rdb, dictsClient, nil, notify.ModeLog)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/health/ready при недоступном Redis вернул %d, ожидается 503", rec.Code)
	}
	if body := decodeHealth(t, rec); body["redis"] != false || body["status"] != "degraded" {
		t.Fatalf("неожиданное тело /health/ready при Redis down: %v", body)
	}
}
