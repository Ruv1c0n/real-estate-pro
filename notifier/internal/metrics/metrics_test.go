package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestCountersAndGaugesRegister — все коллекторы изменяются: инкремент/
// наблюдение не паникуют, значения читаются testutil'ом. Регистрация в
// кастомном реестре проверяется TestCustomRegistryExposesOnlyNotifier.
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

	// outcome-серии гистограммы (п.2 ревью #125): каждый исход — свой
	// ребёнок Vec. Дельта, а не абсолют: тест не должен зависеть от
	// порядка выполнения (детей могли создать другие тесты пакета).
	before := testutil.CollectAndCount(DeliveryDuration)
	for _, outcome := range []string{"sent", "rate_limited", "unknown"} {
		DeliveryDuration.WithLabelValues(outcome).Observe(0.25)
	}
	if got := testutil.CollectAndCount(DeliveryDuration) - before; got != 3 {
		t.Fatalf("серий outcome в DeliveryDuration добавилось %d, want 3", got)
	}
}

// TestHandlerServesMetrics — /metrics отдаёт пром-формат с нашими метриками
// и БЕЗ runtime-коллекторов дефолтного реестра (п.1 ревью #125: go_*/process_*
// с публичного порта — разведка ресурсов процесса).
func TestHandlerServesMetrics(t *testing.T) {
	DeadLetter.Inc()
	GaugePollErrors.Inc()
	QuarantinedJobs.Set(1)
	// Vec-коллекторы не экспонируют серию до первого ребёнка: создаём
	// (как это делают timedSend/InstrumentHandler на первом событии).
	DeliveryDuration.WithLabelValues("sent").Observe(0.01)
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
		"notifier_quarantined_jobs",
		"notifier_gauge_poll_errors_total",
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
	// Регрессия на дефолтный promhttp.Handler(): его реестр тянет go_* и
	// process_* — в публичном /metrics их быть не должно. Проверка по началу
	// строки метрик: substring «process_» поймал бы и собственную
	// notifier_event_process_errors_total.
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "go_") || strings.HasPrefix(line, "process_") {
			t.Errorf("в /metrics есть runtime-серия %q: утечка дефолтного реестра", line)
		}
	}
}

// TestCustomRegistryExposesOnlyNotifier — Gather кастомного реестра содержит
// только серии notifier_*: go_*/process_* регистрируются дефолтным реестром
// и в наш Gather попасть не должны (регрессионный тест п.1 ревью #125).
func TestCustomRegistryExposesOnlyNotifier(t *testing.T) {
	// Гарантируем детей Vec-коллекторов: без детей Vec не экспонирует серию,
	// и проверка прошла бы даже при пустом реестре.
	HTTPDuration.WithLabelValues("200", "GET").Observe(0.01)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("реестр пуст — коллекторы не зарегистрированы")
	}
	for _, mf := range families {
		if name := mf.GetName(); !strings.HasPrefix(name, "notifier_") {
			t.Errorf("в реестре чужая серия %q: ожидаются только notifier_*", name)
		}
	}
}
