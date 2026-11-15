package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
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

	subs := store.NewMemory()
	dictsClient := dicts.New(env("CORE_API_URL", "http://localhost:8000"))

	// Хранилища: в Postgres, когда БД доступна (журнал, лимит 30/час и
	// очередь отложенных — ФТ-3.5, ТЗ 3.6), иначе in-memory для режима без БД.
	var journal notify.Journal
	var limiter notify.Limiter
	var queue notify.DeferredQueue
	if pool != nil {
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

	// Ошибка создания consumer group делает сервис слепым — это фатально.
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		if err := stream.NewConsumer(redisClient, stream.ConsumerName(), dispatcher.Handle).Run(ctx); err != nil {
			log.Fatalf("stream consumer: %v", err)
		}
	}()

	srv := &http.Server{Addr: ":8080", Handler: newMux(redisClient, dictsClient, pool, tgBot != nil)}
	srvErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-srvErr:
		log.Fatal(err)
	}

	// Порядок остановки: HTTP → консьюмер → сброс отложенных уведомлений.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	<-consumerDone
	dispatcher.Flush(shutdownCtx)
	if pool != nil {
		pool.Close()
	}
	log.Println("stopped")
}

// newMux собирает HTTP-обработчики: /health и /health/ready (ФТ-3.7) и
// /debug/event (НФТ-3).
func newMux(rdb *redis.Client, dictsClient *dicts.Client, pool *pgxpool.Pool, botEnabled bool) *http.ServeMux {
	adminKey := os.Getenv("ADMIN_KEY")

	// probe проверяет все зависимости и возвращает карту для тела ответа.
	// db: pool == nil означает memory-режим (сервис без БД) — в нём db
	// всегда false, но /health/ready не считает это деградацией (см. ниже).
	probe := func(ctx context.Context) (status string, redisOK, dictsOK, dbOK bool) {
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		redisOK = rdb.Ping(pctx).Err() == nil
		dictsOK = dictsClient.Healthy(pctx)
		dbOK = false
		if pool != nil {
			dbOK = pool.Ping(pctx) == nil
		}
		status = "ok"
		if !redisOK || !dictsOK || (pool != nil && !dbOK) {
			status = "degraded"
		}
		return status, redisOK, dictsOK, dbOK
	}

	mode := "log"
	if botEnabled {
		mode = "telegram"
	}

	mux := http.NewServeMux()

	// GET /health — liveness: всегда 200, детали деградации видны в теле.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		status, redisOK, dictsOK, dbOK := probe(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": status,
			"redis":  redisOK,
			"dicts":  dictsOK,
			"db":     dbOK,
			"mode":   mode,
		})
	})

	// GET /health/ready — readiness: 200 только когда сервис готов работать.
	// В db-режиме требуются redis, dicts и db; в memory-режиме (pool == nil)
	// БД не сконфигурирована и её отсутствие — не деградация: проверяются
	// только redis и dicts.
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		status, redisOK, dictsOK, dbOK := probe(r.Context())
		ready := redisOK && dictsOK && (pool != nil && dbOK || pool == nil)
		code := http.StatusOK
		if !ready {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{
			"status": status,
			"redis":  redisOK,
			"dicts":  dictsOK,
			"db":     dbOK,
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
