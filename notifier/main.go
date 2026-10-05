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

	"notifier/internal/bot"
	"notifier/internal/model"
	"notifier/internal/store"
	"notifier/internal/telegram"
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

	subStore := store.NewMemory()

	var tgBot *bot.Bot
	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		tgBot = bot.New(telegram.NewClient(token), subStore)
		go tgBot.Run(ctx)
	} else {
		log.Println("TELEGRAM_BOT_TOKEN is empty: matches are only logged, bot is disabled")
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
			for _, s := range model.MatchAll(event, subStore.All()) {
				log.Printf("MATCH: subscription %d (chat %d) <- listing %d (%s, $%.0f) %s",
					s.ID, s.ChatID, event.ID, event.City, event.PriceUSD, event.URL)
				if tgBot != nil {
					go tgBot.Notify(ctx, s.ChatID, event)
				}
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