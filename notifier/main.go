package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"notifier/internal/bot"
	"notifier/internal/db"
	"notifier/internal/dicts"
	"notifier/internal/migrate"
	"notifier/internal/model"
	"notifier/internal/notify"
	"notifier/internal/store"
	"notifier/internal/stream"
	"notifier/internal/telegram"
)

// Параметры доставки (ТЗ 3.6 и contracts.md §2).
const (
	notificationsPerHour = 30
	flushInterval        = 30 * time.Second
	debugBodyLimit       = 1 << 20 // 1 МБ на демо-событие
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Пул БД создаётся первым и передаётся компонентам по мере перевода
	// хранилищ на Postgres (issue #31, этап 2).
	pool, err := db.Connect(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Миграции накатываются до всего остального: consumer, бот и HTTP-сервер
	// не должны прочесть пустые таблицы. Ошибка применения фатальна — без
	// схемы notifier работать не может (применение при деплое — зона
	// Даниила, раннер схему не меняет).
	if pool != nil {
		if err := migrate.Apply(ctx, pool); err != nil {
			log.Fatal(err)
		}
	}

	opt, err := redis.ParseURL(env("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		log.Fatal(err)
	}
	redisClient := redis.NewClient(opt)
	defer redisClient.Close()

	var subs store.Store = store.NewMemory()
	dictsClient := dicts.New(env("CORE_API_URL", "http://localhost:8000"))

	// Хранилища: в Postgres, когда БД доступна (подписки и users — ФТ-3.1,
	// ФТ-3.2; журнал, лимит 30/час и очередь отложенных — ФТ-3.5, ТЗ 3.6),
	// иначе in-memory для режима без БД.
	var journal notify.Journal
	var limiter notify.Limiter
	var queue notify.DeferredQueue
	if pool != nil {
		subs = store.NewPostgres(pool)
		journal = notify.NewPgJournal(pool)
		limiter = notify.NewPgLimiter(pool, notificationsPerHour)
		queue = notify.NewPgQueue(pool)
	} else {
		journal = notify.NewMemoryJournal()
		limiter = notify.NewMemoryLimiter(notificationsPerHour, time.Hour)
		queue = notify.NewMemoryQueue()
	}

	var sender notify.Sender
	var mode string
	var tgBot *bot.Bot
	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		mode = notify.ModeTelegram
		tgBot = bot.New(telegram.NewClient(token), subs, dictsClient)
		go tgBot.Run(ctx)
		sender = tgBot.Send
	} else {
		// Режим «лог» (ФТ-3.6): совпадения пишутся в stdout, без Telegram.
		log.Println("TELEGRAM_BOT_TOKEN is empty: log mode, bot disabled")
		mode = notify.ModeLog
		sender = func(_ context.Context, chatID int64, e model.Event) error {
			log.Printf("MATCH: chat %d <- listing %d (%s, $%d) %s", chatID, e.ID, e.City, e.PriceUSD, e.URL)
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
		if err := stream.NewConsumer(redisClient, stream.ConsumerName(), dispatcher.Handle).Run(ctx); err != nil {
			consumerErr <- err
		}
	}()

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           newMux(redisClient, dictsClient, pool, mode),
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
	// redisClient.Close() дублируется с defer: log.Fatal ниже обходит defer,
	// поэтому при фатальном выходе закрываем клиент явно.
	redisClient.Close()
	if fatalErr != nil {
		log.Fatal(fatalErr)
	}
	log.Println("stopped")
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
	res = p.run(ctx, true)
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
	res = p.run(ctx, false)
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
// учитывается как false).
func parallelProbe(ctx context.Context, checks []func(context.Context) bool) []bool {
	results := make([]bool, len(checks))
	ch := make(chan checkResult, len(checks))

	var wg sync.WaitGroup
	for i, check := range checks {
		wg.Add(1)
		go func(i int, check func(context.Context) bool) {
			defer wg.Done()
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
// вычисленный в main.
func newMux(rdb *redis.Client, dictsClient *dicts.Client, pool *pgxpool.Pool, mode string) *http.ServeMux {
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

	// POST /debug/event кладёт демо-событие в стрим — оно проходит весь
	// обычный путь через consumer group. Без ADMIN_KEY эндпоинт выключен.
	mux.HandleFunc("/debug/event", func(w http.ResponseWriter, r *http.Request) {
		if adminKey == "" {
			http.NotFound(w, r)
			return
		}
		got := r.Header.Get("X-Admin-Key")
		if subtle.ConstantTimeCompare([]byte(got), []byte(adminKey)) != 1 {
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
	})

	return mux
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
