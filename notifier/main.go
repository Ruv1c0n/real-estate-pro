package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"notifier/internal/bot"
	"notifier/internal/db"
	"notifier/internal/dicts"
	"notifier/internal/metrics"
	"notifier/internal/migrate"
	"notifier/internal/model"
	"notifier/internal/notify"
	"notifier/internal/ratelimit"
	"notifier/internal/store"
	"notifier/internal/stream"
	"notifier/internal/telegram"
)

// Параметры доставки (ТЗ 3.6 и contracts.md §2). Численные параметры
// (лимит 30/час, воркеры, порог WARN, период очистки) читаются из
// окружения с дефолтами по ТЗ — см. envPositiveInt/envPositiveDuration.
const (
	defaultNotificationsPerHour = 30
	flushInterval               = 30 * time.Second
	debugBodyLimit              = 1 << 20 // 1 МБ на демо-событие
	// Лимит /debug/event на клиентский IP (аудит 20.11, P2-25: без него
	// X-Admin-Key брутфорсится с LAN бесплатно). Демо-сценарий отправляет
	// событие раз в несколько секунд, поэтому 30/мин с запасом покрывают
	// честное использование и режут перебор.
	defaultDebugRateLimitPerMin = 30
	// Интервал поллера гейджей Prometheus: скрейп в Redis/PG не ходит
	// (карточка 01 плана prod-10of10), поэтому частота определяется здесь,
	// а не нагрузкой от Prometheus.
	metricsPollEvery = 15 * time.Second
)

// Тайминги health-проверок: у каждой проверки свой дедлайн, сбор результатов
// ограничен общим бюджетом, а результат кэшируется против шторма повторных
// healthcheck-запросов (анти-усиление).
const (
	probeCheckTimeout = 1 * time.Second // дедлайн одной проверки
	probeBudget       = 2 * time.Second // общий бюджет probe
	probeCacheTTL     = 4 * time.Second // кэш результата probe
)

func main() {
	// Уровень лога и формат до всего остального: мусорный уровень —
	// fail-fast через обычный log (slog ещё не установлен — начало
	// chicken-and-egg; единственное место, где JSON-структуры ещё нет).
	level, err := parseLevel(env("NOTIFIER_LOG_LEVEL", "info"))
	if err != nil {
		log.Fatal(err)
	}
	// JSON-логи (карточка 02 плана prod-10of10): единая структура для
	// сбора и алертов. Корреляция сквозного пути — по event_id, он и так
	// в контракте listings.new (ТЗ 4.3): контракт не меняется.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Численные параметры (ТЗ 3.6 «настраиваются»): дефолты совпадают со
	// значениями ТЗ, мусорные значения — fail-fast до поднятия компонентов.
	perHour, err := envPositiveInt("NOTIFIER_NOTIFICATIONS_PER_HOUR", defaultNotificationsPerHour)
	if err != nil {
		fatal("config: notifications per hour", "error", err)
	}
	workers, err := envPositiveInt("NOTIFIER_WORKERS", stream.DefaultWorkers())
	if err != nil {
		fatal("config: workers", "error", err)
	}
	lenWarn, err := envPositiveInt64("NOTIFIER_STREAM_LEN_THRESHOLD", stream.DefaultLenWarnThreshold())
	if err != nil {
		fatal("config: stream len warn threshold", "error", err)
	}
	cleanupEvery, err := envPositiveDuration("NOTIFIER_STREAM_CLEANUP_INTERVAL", stream.DefaultCleanupInterval())
	if err != nil {
		fatal("config: stream cleanup interval", "error", err)
	}
	debugRatePerMin, err := envPositiveInt("NOTIFIER_DEBUG_RATE_LIMIT_PER_MIN", defaultDebugRateLimitPerMin)
	if err != nil {
		fatal("config: debug rate limit", "error", err)
	}
	slog.Info("config",
		"notifications_per_hour", perHour,
		"workers", workers,
		"stream_len_warn_threshold", lenWarn,
		"stream_cleanup_interval", cleanupEvery.String(),
		"debug_rate_limit_per_min", debugRatePerMin)

	// Пул БД создаётся первым и передаётся компонентам по мере перевода
	// хранилищ на Postgres (issue #31, этап 2).
	pool, err := db.Connect(ctx)
	if err != nil {
		fatal("db: connect failed", "error", err)
	}

	// Миграции накатываются до всего остального: consumer, бот и HTTP-сервер
	// не должны прочесть пустые таблицы. Ошибка применения фатальна — без
	// схемы notifier работать не может (применение при деплое — зона
	// Даниила, раннер схему не меняет).
	if pool != nil {
		if err := migrate.Apply(ctx, pool); err != nil {
			fatal("migrate: apply failed", "error", err)
		}
	}

	opt, err := redis.ParseURL(env("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		fatal("config: REDIS_URL invalid", "error", err)
	}
	redisClient := redis.NewClient(opt)
	defer redisClient.Close()

	dictsClient := dicts.New(env("CORE_API_URL", "http://localhost:8000"))

	subs, journal, limiter, queue := buildStores(pool, perHour)

	// /stop чистит отложенные задания чата (ТЗ 4.2), если очередь реализует
	// ChatCleaner (MemoryQueue и PgQueue реализуют); боту передаётся
	// структурный интерфейс — bot не импортирует notify.
	var chatCleaner bot.ChatCleaner
	if cc, ok := queue.(notify.ChatCleaner); ok {
		chatCleaner = cc
	}

	var sender notify.Sender
	var mode string
	var tgBot *bot.Bot
	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		mode = notify.ModeTelegram
		tgBot = bot.New(telegram.NewClient(token), subs, dictsClient).WithChatCleaner(chatCleaner)
		go tgBot.Run(ctx)
		sender = tgBot.Send
	} else {
		// Режим «лог» (ФТ-3.6): совпадения пишутся в stdout, без Telegram.
		slog.Info("telegram token is empty: log mode, bot disabled")
		mode = notify.ModeLog
		sender = func(_ context.Context, chatID int64, e model.Event) error {
			slog.Info("match: listing delivered in log mode",
				"chat_id", chatID,
				"listing_id", e.ID,
				"city", e.City,
				"price_usd", e.PriceUSD,
				"url", e.URL)
			return nil
		}
	}

	dispatcher := notify.NewDispatcher(subs, journal, limiter, queue, mode, sender)
	go dispatcher.RunFlusher(ctx, flushInterval)

	// Ошибка создания consumer group делает сервис слепым — это фатально,
	// но log.Fatal в горуте убивал бы процесс в обход defer (пул и redis
	// остались бы незакрытыми): ошибка уходит в main по каналу, main делает
	// корректный graceful shutdown и только потом выходит с кодом 1.
	consumerDone := make(chan struct{})
	consumerErr := make(chan error, 1)
	go func() {
		defer close(consumerDone)
		if err := stream.NewConsumer(redisClient, stream.ConsumerName(), workers, lenWarn, dispatcher.Handle).Run(ctx); err != nil && ctx.Err() == nil {
			// Ошибка шлётся в main только если ctx ещё жив: при штатной
			// остановке (SIGTERM) Run завершается «context canceled» — это
			// не фатальный сбой, и ложный exit 1 портить не нужно.
			consumerErr <- err
		}
	}()

	// Безопасная очистка стрима (ТЗ 4.3 «Хранение»): удаляет только
	// подтверждённые записи за границей min-pending. Ошибки прохода
	// не фатальны — без очистки стрим просто не режется.
	go stream.NewReaper(redisClient, cleanupEvery).Run(ctx)

	// Гейджи Prometheus (карточка 01): pending/длина стрима/отложенные
	// обновляются раз в metricsPollEvery — скрейп в Redis/PG не ходит.
	go pollGauges(ctx, redisClient, pool, metricsPollEvery)

	srv := &http.Server{
		Addr: ":8080",
		// Обвязка снаружи внутрь: slog-лог → HTTP-метрики Prometheus → mux.
		Handler: requestLogger(promhttp.InstrumentHandlerInFlight(metrics.HTTPInFlight,
			promhttp.InstrumentHandlerDuration(metrics.HTTPDuration,
				promhttp.InstrumentHandlerCounter(metrics.HTTPRequests,
					newMux(redisClient, dictsClient, pool, mode, debugRatePerMin))))),
		ReadHeaderTimeout: 5 * time.Second, // медленные заголовки не держат коннект (slowloris)
	}
	srvErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
	}()

	// fatalErr — фатальная ошибка старта (HTTP-сервер или consumer group):
	// выходим кодом 1 уже после корректной остановки всех компонентов.
	var fatalErr error
	select {
	case <-ctx.Done():
	case err := <-srvErr:
		fatalErr = err
	case err := <-consumerErr:
		fatalErr = fmt.Errorf("stream consumer: %w", err)
	}

	// Порядок остановки: отмена ctx → HTTP → консьюмер → сброс отложенных
	// уведомлений → закрытие пула и redis.
	stop() // отменяем ctx: консьюмер и флашер завершают циклы
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	<-consumerDone
	dispatcher.Flush(shutdownCtx)
	if pool != nil {
		pool.Close()
	}
	// redisClient.Close() дублируется с defer: fatal ниже обходит defer,
	// поэтому при фатальном выходе закрываем клиент явно.
	redisClient.Close()
	if fatalErr != nil {
		slog.Error("fatal: shutting down", "error", fatalErr)
		os.Exit(1)
	}
	slog.Info("stopped")
}

// buildStores — точка wiring хранилищ: db-режим (pool != nil) обязан
// использовать Pg-стек (подписки и users — ФТ-3.1, ФТ-3.2; журнал, лимит
// 30/час и очередь отложенных — ФТ-3.5, ТЗ 3.6), memory-режим — in-memory
// реализации. perHour — лимит уведомлений за скользящий час, читаемый
// из NOTIFIER_NOTIFICATIONS_PER_HOUR. Класс бага #51: wiring в main
// разъезжается с ветками db/memory, и db-режим молча работает in-memory —
// уехало на e2e-прогоне. Вынесено из main, чтобы обе ветки wiring
// проверялись тестами напрямую.
func buildStores(pool *pgxpool.Pool, perHour int) (
	subs store.Store,
	journal notify.Journal,
	limiter notify.Limiter,
	queue notify.DeferredQueue,
) {
	if pool != nil {
		return store.NewPostgres(pool),
			notify.NewPgJournal(pool),
			notify.NewPgLimiter(pool, perHour),
			notify.NewPgQueue(pool)
	}
	return store.NewMemory(),
		notify.NewMemoryJournal(),
		notify.NewMemoryLimiter(perHour, time.Hour),
		notify.NewMemoryQueue()
}

// probeResult — снимок состояния зависимостей для тела health-ответов.
type probeResult struct {
	status  string
	redisOK bool
	dictsOK bool
	dbOK    bool
}

// healthProber проверяет зависимости notifier и кэширует результаты:
// ready() — полный probe с сетевой проверкой справочников (readiness),
// live() — локальный probe без сети к Core API (liveness). Кэши раздельные,
// чтобы результат liveness не подмешивался к readiness и наоборот.
type healthProber struct {
	rdb   *redis.Client
	dicts *dicts.Client
	pool  *pgxpool.Pool

	mu       sync.Mutex
	readyRes probeResult
	readyAt  time.Time
	liveRes  probeResult
	liveAt   time.Time
}

// ready — полный probe (redis, dicts по сети, db) с кэшем на probeCacheTTL.
// Readiness обязан проверять справочники сетевым вызовом (ФТ-3.7, НФТ-9).
func (p *healthProber) ready(ctx context.Context) probeResult {
	p.mu.Lock()
	res, cached := p.readyRes, time.Since(p.readyAt) < probeCacheTTL
	p.mu.Unlock()
	if cached {
		return res
	}
	// WithoutCancel: обрыв соединения клиентом (таймаут healthcheck-клиента)
	// не должен отравлять кэш деградированным снимком на probeCacheTTL —
	// probe доводится до конца на неотменяемом контексте.
	res = p.run(context.WithoutCancel(ctx), true)
	p.mu.Lock()
	p.readyRes, p.readyAt = res, time.Now()
	p.mu.Unlock()
	return res
}

// live — liveness-проверка с кэшем на probeCacheTTL: справочники берутся
// из кэша клиента (CachedHealthy) БЕЗ сетевого вызова, поэтому /health
// остаётся быстрым даже при деградации Core API (compose бьёт сюда wget'ом).
func (p *healthProber) live(ctx context.Context) probeResult {
	p.mu.Lock()
	res, cached := p.liveRes, time.Since(p.liveAt) < probeCacheTTL
	p.mu.Unlock()
	if cached {
		return res
	}
	// Без отмены входящего запроса — как в ready(): кэш не отравляется.
	res = p.run(context.WithoutCancel(ctx), false)
	p.mu.Lock()
	p.liveRes, p.liveAt = res, time.Now()
	p.mu.Unlock()
	return res
}

// run выполняет три проверки параллельно: у каждой свой дедлайн
// probeCheckTimeout, сбор ограничен бюджетом probeBudget. При dictsByNetwork
// справочники проверяются сетевым вызовом (Healthy), иначе — по кэшу клиента
// без сети (CachedHealthy). pool == nil — memory-режим: проверка db не
// выполняется, поле db остаётся false, но деградацией не считается.
func (p *healthProber) run(ctx context.Context, dictsByNetwork bool) probeResult {
	dictsCheck := func(ctx context.Context) bool { return p.dicts.Healthy(ctx) }
	if !dictsByNetwork {
		dictsCheck = func(context.Context) bool { return p.dicts.CachedHealthy() }
	}

	results := parallelProbe(ctx, []func(context.Context) bool{
		p.redisCheck,
		dictsCheck,
		p.dbCheck,
	})

	res := probeResult{
		redisOK: results[0],
		dictsOK: results[1],
		dbOK:    results[2],
	}
	res.status = "ok"
	if !res.redisOK || !res.dictsOK || (p.pool != nil && !res.dbOK) {
		res.status = "degraded"
	}
	return res
}

// redisCheck пингует Redis.
func (p *healthProber) redisCheck(ctx context.Context) bool {
	return p.rdb.Ping(ctx).Err() == nil
}

// dbCheck пингует Postgres; в memory-режиме (pool == nil) проверки нет.
func (p *healthProber) dbCheck(ctx context.Context) bool {
	if p.pool == nil {
		return false
	}
	return p.pool.Ping(ctx) == nil
}

// checkResult — результат одной проверки в канале результатов.
type checkResult struct {
	idx int
	ok  bool
}

// parallelProbe запускает проверки параллельно и собирает результаты.
// Канал результатов буферизован: горутины не блокируются и не дают гонок,
// если сборщик вышел по общему бюджету раньше их завершения — поздние
// результаты остаются в канале и игнорируются (недождавшаяся проверка
// учитывается как false). Время жизни горутин ограничено их собственными
// per-check дедлайнами, поэтому явного ожидания не нужно.
func parallelProbe(ctx context.Context, checks []func(context.Context) bool) []bool {
	results := make([]bool, len(checks))
	ch := make(chan checkResult, len(checks))

	for i, check := range checks {
		go func(i int, check func(context.Context) bool) {
			cctx, cancel := context.WithTimeout(ctx, probeCheckTimeout)
			defer cancel()
			ch <- checkResult{i, check(cctx)}
		}(i, check)
	}

	deadline := time.After(probeBudget)
	for i := 0; i < len(checks); i++ {
		select {
		case r := <-ch:
			results[r.idx] = r.ok
		case <-deadline:
			return results
		}
	}
	return results
}

// newMux собирает HTTP-обработчики: /health и /health/ready (ФТ-3.7) и
// /debug/event (НФТ-3). mode — режим доставки (notify.ModeLog / ModeTelegram),
// вычисленный в main. debugRatePerMin — лимит /debug/event на клиентский IP
// в минуту (аудит 20.11, P2-25); 0 и отрицательные сюда не попадают —
// main фейлит старт раньше.
func newMux(rdb *redis.Client, dictsClient *dicts.Client, pool *pgxpool.Pool, mode string, debugRatePerMin int) *http.ServeMux {
	adminKey := os.Getenv("ADMIN_KEY")

	prober := &healthProber{rdb: rdb, dicts: dictsClient, pool: pool}

	mux := http.NewServeMux()

	// GET /health — liveness: всегда 200, детали деградации видны в теле.
	// dicts — из кэша клиента без сетевого вызова к Core API: liveness не
	// должен зависеть от его доступности и усиливать нагрузку штормом
	// healthcheck'ов.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		res := prober.live(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": res.status,
			"redis":  res.redisOK,
			"dicts":  res.dictsOK,
			"db":     res.dbOK,
			"mode":   mode,
		})
	})

	// GET /health/ready — readiness: 200 только когда сервис готов работать.
	// В db-режиме требуются redis, dicts и db; в memory-режиме (pool == nil)
	// БД не сконфигурирована и её отсутствие — не деградация: проверяются
	// только redis и dicts. dicts проверяется ПО СЕТИ (ФТ-3.7, НФТ-9).
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		res := prober.ready(r.Context())
		ready := res.redisOK && res.dictsOK && (pool != nil && res.dbOK || pool == nil)
		code := http.StatusOK
		if !ready {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{
			"status": res.status,
			"redis":  res.redisOK,
			"dicts":  res.dictsOK,
			"db":     res.dbOK,
			"mode":   mode,
		})
	})

	// /metrics — Prometheus (карточка 01 плана prod-10of10). Публичный на
	// mux: значений секретов и PII не содержит. Гейджи обновляются фоновым
	// поллером — скрейп в Redis/PG не ходит (см. internal/metrics).
	mux.Handle("/metrics", metrics.Handler())

	// POST /debug/event кладёт демо-событие в стрим — оно проходит весь
	// обычный путь через consumer group. Без ADMIN_KEY эндпоинт выключен.
	// Rate limit per-IP (аудит 20.11, P2-25) режет частоту ДО авторизации:
	// брутфорс X-Admin-Key упирается в bucket, а каждая неудачная попытка
	// остаётся в логах — раньше она молча терялась в 401.
	debugEvent := func(w http.ResponseWriter, r *http.Request) {
		if adminKey == "" {
			http.NotFound(w, r)
			return
		}
		got := r.Header.Get("X-Admin-Key")
		if subtle.ConstantTimeCompare([]byte(got), []byte(adminKey)) != 1 {
			slog.Warn("debug event: unauthorized attempt", "client_ip", ratelimit.ClientIP(r))
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, debugBodyLimit))
		if err != nil {
			http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
			return
		}
		if _, err := model.ParseEvent(body); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"error": "validation_error", "message": err.Error()})
			return
		}
		id, err := rdb.XAdd(r.Context(), &redis.XAddArgs{
			Stream: stream.StreamName,
			Values: map[string]any{"data": string(body)},
		}).Result()
		if err != nil {
			http.Error(w, `{"error":"redis_unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"published": "true", "stream_id": id})
	}
	// Лимит вешается только на включённый эндпоинт: выключенный (пустой
	// ключ) отвечает 404 как факт конфигурации, а не попытка авторизации,
	// и bucket для него не нужен.
	if adminKey != "" {
		debugEvent = ratelimit.NewPerMinute(debugRatePerMin).Middleware(debugEvent)
	}
	mux.HandleFunc("/debug/event", debugEvent)

	return mux
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envPositiveDuration читает длительность из окружения с дефолтом (ТЗ 3.6:
// параметры настраиваются). Некорректное значение (не длительность, <= 0) —
// ошибка старта (fail-fast): тихий дефолт при опечатке в .env маскировал бы
// misconfig весь аптайм, как и молчаливое подключение app/app в db.Connect.
func envPositiveDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %q — не длительность (пример: 5m, 30s)", key, raw)
	}
	if v <= 0 {
		return 0, fmt.Errorf("%s: %q должно быть положительным", key, raw)
	}
	return v, nil
}

// envPositiveInt — чтение положительного целого из окружения с дефолтом;
// мусорное значение — ошибка старта (fail-fast), как и у длительностей выше.
func envPositiveInt(key string, fallback int) (int, error) {
	v64, err := envPositiveInt64(key, int64(fallback))
	if err != nil {
		return 0, err
	}
	return int(v64), nil
}

func envPositiveInt64(key string, fallback int64) (int64, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %q — не целое число", key, raw)
	}
	if v <= 0 {
		return 0, fmt.Errorf("%s: %q должно быть положительным", key, raw)
	}
	return v, nil
}

// parseLevel разбирает NOTIFIER_LOG_LEVEL (debug/info/warn/error, регистр
// не важен). Список намеренно без уровней вроде TRACE: это та гранулярность,
// которую мы реально алертим и фильтруем.
func parseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("NOTIFIER_LOG_LEVEL: %q — не уровень (debug|info|warn|error)", raw)
	}
}

// fatal — slog.Error + os.Exit(1): замена log.Fatal для стартовых ошибок,
// чтобы они шли в ту же JSON-структуру. Как и log.Fatal, обходит defer —
// вызвать только до/после участков с defer-очисткой (main закрывает redis
// явно перед fatal-выходом).
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

// requestLogger — request-id middleware (карточка 02 плана prod-10of10):
// входящий X-Request-ID сохраняется (чтобы сквозной trace можно было начать
// с клиента), иначе генерируется 16-hex; id возвращается в ответе и попадает
// в JSON-лог завершения запроса.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			rid = newRequestID()
		}
		w.Header().Set("X-Request-ID", rid)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		slog.Info("http request",
			"request_id", rid,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

// statusRecorder ловит статус для лога: WriteHeader может не вызываться
// для пустых ответов — дефолт 200 проставлен при создании.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// newRequestID — 8 случайных байт в hex: 64 бита энтропии хватает для
// корреляции запросов одного стенда, полный UUID тут избыточен.
func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand отказал — аномалия, но HTTP-запрос всё равно надо
		// обработать: fallback на время достаточен для корреляции одной
		// пачки логов.
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// pollGauges обновляет гейджи internal/metrics раз в every: pending и
// длина стрима — из Redis, отложенные — из notifications_log (только
// db-режим; в memory-режиме нечего считать — см. metrics.DeferredJobs).
// Ошибки поллера не фатальны: гейдж остаётся с прошлым значением, сбой
// уходит WARN'ом — алерт по несвежим гейджам делается в карточке 03.
func pollGauges(ctx context.Context, rdb *redis.Client, pool *pgxpool.Pool, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pollOnce(ctx, rdb, pool)
		}
	}
}

// pollOnce — один проход поллера гейджей.
func pollOnce(ctx context.Context, rdb *redis.Client, pool *pgxpool.Pool) {
	if pending, err := rdb.XPending(ctx, stream.StreamName, stream.GroupName).Result(); err != nil {
		slog.Warn("metrics: xpending failed", "error", err)
	} else {
		metrics.StreamPending.Set(float64(pending.Count))
	}
	if n, err := rdb.XLen(ctx, stream.StreamName).Result(); err != nil {
		slog.Warn("metrics: xlen failed", "error", err)
	} else {
		metrics.StreamLength.Set(float64(n))
	}
	if pool == nil {
		return
	}
	var n int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM notifier.notifications_log WHERE status = 'deferred'").Scan(&n); err != nil {
		slog.Warn("metrics: deferred count failed", "error", err)
		return
	}
	metrics.DeferredJobs.Set(float64(n))
}
