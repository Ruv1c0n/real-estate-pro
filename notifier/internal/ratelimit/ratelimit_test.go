package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestPerIPAllowsBurstThenDenies(t *testing.T) {
	// 3 события в минуту на IP: burst=3 — три запроса подряд проходят,
	// четвёртый упирается в пустой bucket (аудит P2-25).
	p := NewPerMinute(3)
	for i := 0; i < 3; i++ {
		if !p.allow("192.0.2.1") {
			t.Fatalf("запрос %d отклонён: bucket должен позволить burst=3", i+1)
		}
	}
	if p.allow("192.0.2.1") {
		t.Fatal("4-й запрос подряд прошёл: лимит 3/мин на IP не работает")
	}
	// Другой IP — свой bucket: чужой перебор не съедает лимит честного клиента.
	if !p.allow("192.0.2.2") {
		t.Fatal("второй IP отклонён: bucket'ы должны быть независимыми")
	}
}

func TestPerIPDenialDoesNotConsumeToken(t *testing.T) {
	// Отказ не должен сбрасывать bucket: после паузы, дающей один токен,
	// ровно один запрос проходит. Если бы отказы тратили токены, клиент
	// под лимитом навсегда остался бы заблокированным.
	p := NewPerIP(rate.Limit(100), 1) // высокая частота: 1 токен за 10 мс
	if !p.allow("192.0.2.9") {
		t.Fatal("первый запрос в burst=1 должен пройти")
	}
	if p.allow("192.0.2.9") {
		t.Fatal("второй мгновенный запрос должен быть отклонён (burst=1)")
	}
	time.Sleep(15 * time.Millisecond)
	if !p.allow("192.0.2.9") {
		t.Fatal("после пополнения bucket'а запрос должен пройти")
	}
}

func TestMiddlewareReturns429WithRetryAfter(t *testing.T) {
	p := NewPerMinute(1)
	handler := p.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/debug/event", nil)
	req.RemoteAddr = "192.0.2.10:1234"

	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("первый запрос вернул %d, ожидается 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("второй запрос вернул %d, ожидается 429", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Fatal("429 без Retry-After: клиент не узнаёт, когда повторять")
	}
	if body := rec.Body.String(); !strings.Contains(body, `"rate_limited"`) {
		t.Fatalf("тело 429 без json-ошибки: %q", body)
	}
}

func TestSweepDropsIdleEntries(t *testing.T) {
	oldEvery, oldTTL := sweepEvery, idleTTL
	sweepEvery, idleTTL = 4, time.Millisecond
	t.Cleanup(func() { sweepEvery, idleTTL = oldEvery, oldTTL })

	p := NewPerMinute(100)
	p.allow("192.0.2.20")
	p.allow("192.0.2.21")
	time.Sleep(5 * time.Millisecond) // обе записи старше idleTTL

	// sweepEvery=4: четыре обращения к разным IP запускают чистку,
	// которая обязана убрать протухшие записи.
	for i := 0; i < 6; i++ {
		p.allow("192.0.2.30")
	}

	p.mu.Lock()
	got := len(p.entries)
	p.mu.Unlock()
	if got != 1 {
		t.Fatalf("после чистки в карте %d записей, ожидается 1 (только активный IP)", got)
	}
}

func TestClientIPFromRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.7:5678"
	if ip := ClientIP(req); ip != "198.51.100.7" {
		t.Fatalf("ClientIP = %q, want 198.51.100.7", ip)
	}

	// RemoteAddr без порта (некоторые транспорта) — не паникуем.
	req.RemoteAddr = "198.51.100.9"
	if ip := ClientIP(req); ip != "198.51.100.9" {
		t.Fatalf("ClientIP без порта = %q, want как есть", ip)
	}
}
