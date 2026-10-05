package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"

	"notifier/internal/model"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	opt, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		log.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()

	// Временно: подписки в памяти. Позже будут читаться из Postgres.
	subs := []model.Subscription{
		{ID: 1, ChatID: 0, City: "Minsk", Rooms: model.IntPtr(1), MaxPriceUSD: model.FloatPtr(400)},
	}

	go func() {
		sub := rdb.Subscribe(ctx, "listings.new")
		defer sub.Close()
		log.Println("subscribed to listings.new")
		for msg := range sub.Channel() {
			event, err := model.ParseEvent([]byte(msg.Payload))
			if err != nil {
				log.Printf("bad event skipped: %v", err)
				continue
			}
			for _, s := range model.MatchAll(event, subs) {
				log.Printf("MATCH: subscription %d <- listing %d (%s, $%.0f) %s",
					s.ID, event.ID, event.City, event.PriceUSD, event.URL)
			}
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
}