package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/redis/go-redis/v9"

	"notifier/internal/bot"
	"notifier/internal/model"
	"notifier/internal/store"
	"notifier/internal/stream"
	"notifier/internal/telegram"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	opt, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		log.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()

	subStore := store.NewMemory()

	var tgBot *bot.Bot
	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		tgBot = bot.New(telegram.NewClient(token), subStore)
		go tgBot.Run(ctx)
	} else {
		log.Println("TELEGRAM_BOT_TOKEN is empty: matches are only logged, bot is disabled")
	}

	// Идемпотентный обработчик: при повторной доставке события из стрима
	// MarkSent не даст отправить то же объявление тому же чату второй раз.
	handle := func(ctx context.Context, event model.Event) error {
		for _, s := range model.MatchAll(event, subStore.All()) {
			if !subStore.MarkSent(s.ChatID, event.ID) {
				continue
			}
			log.Printf("MATCH: subscription %d (chat %d) <- listing %d (%s, $%.0f) %s",
				s.ID, s.ChatID, event.ID, event.City, event.PriceUSD, event.URL)
			if tgBot != nil {
				tgBot.Notify(ctx, s.ChatID, event)
			}
		}
		return nil
	}

	host, _ := os.Hostname()
	consumer := stream.New(rdb, stream.Config{
		Stream:   env("STREAM_NAME", "listings.new"),
		Group:    env("STREAM_GROUP", "notifier"),
		Consumer: host,
		Workers:  envInt("WORKERS", 8),
	}, handle)

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		if err := consumer.Run(ctx); err != nil {
			log.Printf("stream consumer stopped: %v", err)
			stop()
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: ":8080", Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-consumerDone
}