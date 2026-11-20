package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestCountersAndGaugesRegister — все коллекторы зарегистрированы в
// default-реестре и изменяются: инкремент/наблюдение не паникуют, значения
// читаются testutil'ом (регистрация в init проверяется самим фактом чтения).
func TestCountersAndGaugesRegister(t *testing.T) {
	DeadLetter.Inc()
	DeadLetter.Inc()
	if got := testutil.ToFloat64(DeadLetter); got != 2 {
		t.Fatalf("DeadLetter = %v, want 2", got)
	}

	TelegramRetryAfter.Inc()
	if got := testutil.ToFloat64(TelegramRetryAfter); got != 1 {
		t.Fatalf("TelegramRetryAfter = %v, want 1", got)
	}

	StreamLength.Set(42)
	if got := testutil.ToFloat64(StreamLength); got != 42 {
		t.Fatalf("StreamLength = %v, want 42", got)
	}

	DeliveryDuration.Observe(0.25)
	if got := testutil.CollectAndCount(DeliveryDuration); got != 1 {
		t.Fatalf("DeliveryDuration серий собрано %d, want 1", got)
	}
}

// TestHandlerServesMetrics — /metrics отдаёт пром-формат с нашими метриками
// (проверка регистрации и формата одним проходом).
func TestHandlerServesMetrics(t *testing.T) {
	DeadLetter.Inc()
	// Vec-коллекторы не экспонируют серию до первого ребёнка: создаём
	// один (как это делает InstrumentHandlerCounter на первом запросе).
	HTTPRequests.WithLabelValues("200", "GET").Inc()
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics вернул %d, ожидается 200", rec.Code)
	}
	body := rec.Body.String()
	for _, name := range []string{
		"notifier_stream_pending_events",
		"notifier_stream_length",
		"notifier_deferred_jobs",
		"notifier_delivery_duration_seconds",
		"notifier_telegram_retry_after_total",
		"notifier_dead_letter_total",
		"notifier_event_process_errors_total",
		"notifier_http_requests_total",
	} {
		if !strings.Contains(body, name) {
			t.Errorf("в /metrics нет метрики %s", name)
		}
	}
	if !strings.Contains(body, "go_goroutines") {
		t.Error("в /metrics нет runtime-метрик Go (go_*)")
	}
}
