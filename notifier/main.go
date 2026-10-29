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

	"github.com/redis/go-redis/v9"

	"notifier/internal/bot"
	"notifier/internal/dicts"
	"notifier/internal/model"
	"notifier/internal/notify"
	"notifier/internal/store"
	"notifier/internal/stream"
	"notifier/internal/telegram"
)

// Параметры доставки (ТЗ 3.6 и contracts.md §2).
const (
	notificationsPerHour = 30
	journalTTL           = 24 * time.Hour
	flushInterval        = 30 * time.Second
	debugBodyLimit       = 1 << 20 // 1 МБ на демо-событие
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	opt, err := redis.ParseURL(env("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		log.Fatal(err)
	}
	redisClient := redis.NewClient(opt)
	defer redisClient.Close()

	subs := store.NewMemory()
	journal := notify.NewJournal(journalTTL)
	limiter := notify.NewLimiter(notificationsPerHour, time.Hour)
	dictsClient := dicts.New(env("CORE_API_URL", "http://localhost:8000"))

	var sender notify.Sender
	var tgBot *bot.Bot
	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		tgBot = bot.New(telegram.NewClient(token), subs, dictsClient)
		go tgBot.Run(ctx)
		sender = tgBot.Send
	} else {
		// Режим «лог» (ФТ-3.6): совпадения пишутся в stdout, без Telegram.
		log.Println("TELEGRAM_BOT_TOKEN is empty: log mode, bot disabled")
		sender = func(_ context.Context, chatID int64, e model.Event) error {
			log.Printf("MATCH: chat %d <- listing %d (%s, $%d) %s", chatID, e.ID, e.City, e.PriceUSD, e.URL)
			return nil
		}
	}

	dispatcher := notify.NewDispatcher(subs, journal, limiter, sender)
	go dispatcher.RunFlusher(ctx, flushInterval)

	// Ошибка создания consumer group делает сервис слепым — это фатально.
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		if err := stream.NewConsumer(redisClient, stream.ConsumerName(), dispatcher.Handle).Run(ctx); err != nil {
			log.Fatalf("stream consumer: %v", err)
		}
	}()

	srv := &http.Server{Addr: ":8080", Handler: newMux(redisClient, dictsClient, tgBot != nil)}
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
	log.Println("stopped")
}

// newMux собирает HTTP-обработчики: /health (ФТ-3.7) и /debug/event (НФТ-3).
func newMux(rdb *redis.Client, dictsClient *dicts.Client, botEnabled bool) *http.ServeMux {
	adminKey := os.Getenv("ADMIN_KEY")

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		hctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		redisOK := rdb.Ping(hctx).Err() == nil
		dictsOK := dictsClient.Healthy(hctx)
		status := "ok"
		if !redisOK || !dictsOK {
			status = "degraded"
		}
		mode := "log"
		if botEnabled {
			mode = "telegram"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": status,
			"redis":  redisOK,
			"dicts":  dictsOK,
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
