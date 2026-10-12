// Package stream читает события о новых объявлениях из Redis Stream
// через consumer group: доставка «минимум один раз», события не теряются
// при перезапуске notifier. Формат сообщения: поле "data" = JSON-событие
package stream

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"notifier/internal/model"
)

// Handler обрабатывает событие. nil → сообщение подтверждается (XACK).
// Ошибка → остаётся в pending и будет доставлено повторно, поэтому
// обработчик обязан быть идемпотентным.
type Handler func(ctx context.Context, e model.Event) error

type Config struct {
	Stream   string // listings.new
	Group    string // notifier
	Consumer string // имя экземпляра, обычно hostname
	Workers  int    // размер пула воркеров (по ТЗ — 8)
}

const (
	reclaimEvery   = 15 * time.Second
	reclaimMinIdle = 30 * time.Second
	handleTimeout  = 30 * time.Second
)

type Consumer struct {
	rdb    *redis.Client
	cfg    Config
	handle Handler
}

func New(rdb *redis.Client, cfg Config, h Handler) *Consumer {
	if cfg.Workers <= 0 {
		cfg.Workers = 8
	}
	return &Consumer{rdb: rdb, cfg: cfg, handle: h}
}

// Run блокируется до отмены ctx, затем дорабатывает уже взятые сообщения.
func (c *Consumer) Run(ctx context.Context) error {
	// "0": при первом создании группы прочитать и историю стрима.
	// Дублей не будет — их отсекает журнал отправленных уведомлений.
	err := c.rdb.XGroupCreateMkStream(ctx, c.cfg.Stream, c.cfg.Group, "0").Err()
	if err != nil && !strings.HasPrefix(err.Error(), "BUSYGROUP") {
		return err
	}
	log.Printf("stream consumer %q: group %q on %q, workers=%d",
		c.cfg.Consumer, c.cfg.Group, c.cfg.Stream, c.cfg.Workers)

	jobs := make(chan redis.XMessage, c.cfg.Workers*2)

	var workers sync.WaitGroup
	for i := 0; i < c.cfg.Workers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for m := range jobs {
				c.process(m)
			}
		}()
	}

	var producers sync.WaitGroup
	producers.Add(2)
	go func() { defer producers.Done(); c.read(ctx, jobs) }()
	go func() { defer producers.Done(); c.reclaim(ctx, jobs) }()

	producers.Wait()
	close(jobs)
	workers.Wait()
	return nil
}

func (c *Consumer) read(ctx context.Context, jobs chan<- redis.XMessage) {
	for ctx.Err() == nil {
		res, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    c.cfg.Group,
			Consumer: c.cfg.Consumer,
			Streams:  []string{c.cfg.Stream, ">"},
			Count:    32,
			Block:    2 * time.Second,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) || ctx.Err() != nil {
				continue
			}
			log.Printf("XREADGROUP: %v", err)
			sleep(ctx, time.Second)
			continue
		}
		for _, s := range res {
			for _, m := range s.Messages {
				select {
				case jobs <- m:
				case <-ctx.Done():
					return // останется в pending, подберёт reclaim после рестарта
				}
			}
		}
	}
}

// reclaim подбирает сообщения, зависшие у упавших консьюмеров
// или не обработанные из-за ошибки handler'а.
func (c *Consumer) reclaim(ctx context.Context, jobs chan<- redis.XMessage) {
	t := time.NewTicker(reclaimEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		start := "0-0"
		for ctx.Err() == nil {
			msgs, next, err := c.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   c.cfg.Stream,
				Group:    c.cfg.Group,
				Consumer: c.cfg.Consumer,
				MinIdle:  reclaimMinIdle,
				Start:    start,
				Count:    32,
			}).Result()
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("XAUTOCLAIM: %v", err)
				}
				break
			}
			for _, m := range msgs {
				select {
				case jobs <- m:
				case <-ctx.Done():
					return
				}
			}
			if next == "0-0" || len(msgs) == 0 {
				break
			}
			start = next
		}
	}
}

func (c *Consumer) process(m redis.XMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
	defer cancel()

	raw, _ := m.Values["data"].(string)
	event, err := model.ParseEvent([]byte(raw))
	if err != nil {
		// «Ядовитое» сообщение: повторная доставка не поможет.
		log.Printf("stream msg %s: bad event dropped: %v", m.ID, err)
		c.ack(m.ID)
		return
	}
	if err := c.handle(ctx, event); err != nil {
		log.Printf("stream msg %s (listing %d): handler failed, will retry: %v", m.ID, event.ID, err)
		return
	}
	c.ack(m.ID)
}

func (c *Consumer) ack(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.rdb.XAck(ctx, c.cfg.Stream, c.cfg.Group, id).Err(); err != nil {
		log.Printf("XACK %s: %v", id, err)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}