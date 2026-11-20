// Package metrics — Prometheus-метрики notifier (карточка 01 плана
// prod-10of10; аудит 20.11: «нет метрик… observability пуста»). Один
// default-реестр на процесс: сервис маленький, раздельные реестры не
// дают ничего, кроме проблем со сбором.
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
		Help: "Строки deferred в notifications_log (только db-режим; в memory-режиме не обновляется).",
	})

	// DeliveryDuration — время отправки уведомления в Telegram (оба пути:
	// прямой dispatch и flush): основа SLO НФТ-11 (p95 < 5 с).
	DeliveryDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "notifier_delivery_duration_seconds",
		Help:    "Длительность отправки уведомления в Telegram.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	})

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
	prometheus.MustRegister(
		StreamPending, StreamLength, DeferredJobs,
		DeliveryDuration, TelegramRetryAfter, DeadLetter, ProcessErrors,
		HTTPRequests, HTTPDuration, HTTPInFlight,
	)
}

// Handler — /metrics: стандартный promhttp с предвычисленным gzip и
// OpenMetrics-переключением. Публичный на mux: значений секретов не
// содержит (счётчики и гейджи без PII), при выносе сервиса из внутренней
// сети ставится под ключ или отдельный порт.
func Handler() http.Handler {
	return promhttp.Handler()
}
