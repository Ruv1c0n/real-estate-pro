// Package metrics — Prometheus-метрики notifier (карточка 01 плана
// prod-10of10; аудит 20.11: «нет метрик… observability пуста»). Свой
// реестр вместо дефолтного (п.1 ревью #125): /metrics экспортирует только
// notifier_* — go_* и process_* дефолтного реестра (разведка ресурсов
// процесса) с публичного порта 8080 не уходят.
//
// Гейджи (pending/длина стрима/отложенные) обновляются фоновым поллером
// из main, а не на скрейпе: скрейп не должен ходить в Redis/Postgres —
// прометей сам решает, когда опрашивать, и пачка скрейпов превратилась
// бы в шторм запросов к зависимостям.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// reg — реестр /metrics: наполняется только коллекторами notifier_*,
// чтобы публичный эндпоинт не экспонировал runtime-метрики Go и процесса
// (go_*/process_* достаются из дефолтного реестра).
var reg = prometheus.NewRegistry()

var (
	// StreamPending — записи в pending consumer group: растёт при
	// залипании воркеров; НФТ-9 требует видеть pending в состоянии сервиса.
	StreamPending = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "notifier_stream_pending_events",
		Help: "Записи в pending группы notifier стрима listings.new.",
	})

	// StreamLength — длина стрима listings.new: парный к WARN-порогу
	// (ТЗ 3.6, мягкий порог 10 000) — алерт по нему заменяет grep логов.
	StreamLength = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "notifier_stream_length",
		Help: "Длина стрима listings.new (XLEN).",
	})

	// DeferredJobs — отложенные задания 30/час (ТЗ 3.6): хвост, который
	// флешер дренажирует по мере освобождения окна.
	DeferredJobs = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "notifier_deferred_jobs",
		Help: "Строки deferred в notifications_log (только db-режим; в memory-режиме не экспонируется).",
	})

	// QuarantinedJobs — карантин (п.4 ревью #125): строки failed в
	// notifications_log — пары, заблокированные навсегда из-за нечитаемого
	// payload при PopBatch (ТЗ 3.6: задания не отбрасываются). Рост выше
	// нуля = битые данные, нужен разбор.
	QuarantinedJobs = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "notifier_quarantined_jobs",
		Help: "Строки failed (карантин) в notifications_log (только db-режим; в memory-режиме не экспонируется).",
	})

	// GaugePollErrors — неудачные проходы поллера гейджей (п.3 ревью
	// #125): при недоступном Redis/PG гейджи замирают на прошлом значении,
	// а WARN раз в 15 с алертом не отличить. Счётчик растёт на каждый
	// проход, в котором ошиблась хоть одна ветка pollOnce.
	GaugePollErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "notifier_gauge_poll_errors_total",
		Help: "Неудачные проходы поллера гейджей (Redis/PG недоступны, гейджи замерли).",
	})

	// DeliveryDuration — время отправки уведомления в Telegram (оба пути:
	// прямой dispatch и flush): основа SLO НФТ-11 (p95 < 5 с). Лейбл
	// outcome разделяет исходы (п.2 ревью #125): p95 считают по sent,
	// 429 и неизвестные исходы не должны смазывать гистограмму.
	DeliveryDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "notifier_delivery_duration_seconds",
		Help:    "Длительность отправки уведомления в Telegram по исходу (outcome).",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	}, []string{"outcome"})

	// TelegramRetryAfter — ответы Telegram 429: частота прямо влияет на
	// задержку доставки и дренаж отложенных (ТЗ 4.3, Retry-After).
	TelegramRetryAfter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "notifier_telegram_retry_after_total",
		Help: "Полученные ответы Telegram 429 Retry-After.",
	})

	// DeadLetter — записи, уложенные в listings.dead с причиной (ТЗ 4.3):
	// рост выше нуля = битые payload'ы, повод разбираться.
	DeadLetter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "notifier_dead_letter_total",
		Help: "Записи, перемещённые в listings.dead.",
	})

	// ProcessErrors — обработка события упала на временной
	// инфраструктурный ошибке: запись не подтверждена и вернётся по
	// XAUTOCLAIM (ТЗ 4.3). Рост — инфраструктурный шторм.
	ProcessErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "notifier_event_process_errors_total",
		Help: "Обработка события не удалась (запись осталась в pending).",
	})

	// HTTP-метрики запросов — через promhttp.InstrumentHandler* вокруг mux.
	HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "notifier_http_requests_total",
		Help: "HTTP-запросы по коду, методу и пути.",
	}, []string{"code", "method"})
	HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "notifier_http_request_duration_seconds",
		Help:    "Длительность обработки HTTP-запроса.",
		Buckets: []float64{0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2},
	}, []string{"code", "method"})
	HTTPInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "notifier_http_requests_in_flight",
		Help: "HTTP-запросы в обработке прямо сейчас.",
	})
)

func init() {
	reg.MustRegister(
		StreamPending, StreamLength, DeferredJobs, QuarantinedJobs, GaugePollErrors,
		DeliveryDuration, TelegramRetryAfter, DeadLetter, ProcessErrors,
		HTTPRequests, HTTPDuration, HTTPInFlight,
	)
}

// Handler — /metrics: HandlerFor по своему реестру (только notifier_*,
// без go_*/process_* и promhttp_* дефолтного; gzip по Accept-Encoding,
// без OpenMetrics-переговоров). Публичный на mux: значений секретов и PII
// не содержит, но при выносе сервиса из внутренней сети ставится под ключ
// или отдельный порт.
func Handler() http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}
