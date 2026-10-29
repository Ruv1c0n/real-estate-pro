// Package stream читает события из Redis Streams (listings.new) через
// consumer group и раздаёт их пулу воркеров (ФТ-3.3, contracts.md §2).
package stream

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"notifier/internal/model"
)

const (
	// StreamName — стрим событий новых объявлений (contracts.md §2).
	StreamName = "listings.new"
	// GroupName — consumer group notifier; создание с MKSTREAM и позицией 0.
	GroupName = "notifier"
	// DeadStream — стрим для некорректных payload'ов.
	DeadStream = "listings.dead"

	workerCount  = 8                // пул воркеров, ТЗ 3.6
	minIdle      = 30 * time.Second // pending старше этого срока забирает XAUTOCLAIM
	claimEvery   = 15 * time.Second // период обхода pending
	readBlock    = 5 * time.Second  // блокирующее чтение XREADGROUP
	readCount    = int64(16)        // сообщений за одно чтение
	retryDelay   = 3 * time.Second  // пауза при ошибке Redis
	handleBudget = 5 * time.Second  // бюджет обработки одного события (НФТ-11)
)

// Handler обрабатывает одно событие. Обработка фиксирует доставку
// (журнал/отложенные задания), после чего консьюмер подтверждает запись.
type Handler func(ctx context.Context, e model.Event)

// Redis — операции go-redis, нужные консьюмеру; UniversalClient их удовлетворяет.
type Redis interface {
	XGroupCreateMkStream(ctx context.Context, stream, group, start string) *redis.StatusCmd
	XReadGroup(ctx context.Context, a *redis.XReadGroupArgs) *redis.XStreamSliceCmd
	XAutoClaim(ctx context.Context, a *redis.XAutoClaimArgs) *redis.XAutoClaimCmd
	XAck(ctx context.Context, stream, group string, ids ...string) *redis.IntCmd
	XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd
}

// Consumer читает стрим группой notifier: цикл XREADGROUP раздаёт записи
// пулу воркеров, фоновый XAUTOCLAIM забирает зависшие pending-записи.
type Consumer struct {
	rdb    Redis
	name   string
	handle Handler
}

func NewConsumer(rdb Redis, consumerName string, handle Handler) *Consumer {
	return &Consumer{rdb: rdb, name: consumerName, handle: handle}
}

// ConsumerName делает имя consumer'а уникальным для экземпляра (contracts.md §2).
func ConsumerName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

// Run работает до отмены контекста. Ошибка возвращается только при
// невозможности создать consumer group — без этого сервис слеп.
func (c *Consumer) Run(ctx context.Context) error {
	if err := c.rdb.XGroupCreateMkStream(ctx, StreamName, GroupName, "0").Err(); err != nil &&
		!isBusyGroup(err) {
		return fmt.Errorf("create consumer group %s: %w", GroupName, err)
	}
	log.Printf("consumer group %q ready on stream %q (consumer %q)", GroupName, StreamName, c.name)

	jobs := make(chan redis.XMessage, readCount)

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for msg := range jobs {
				c.process(ctx, msg)
			}
		}()
	}

	// claim-цикл может писать в jobs, поэтому закрываем канал только после
	// его остановки — иначе гонка «send on closed channel».
	claimDone := make(chan struct{})
	go func() {
		defer close(claimDone)
		c.claimLoop(ctx, jobs)
	}()

	err := c.readLoop(ctx, jobs)
	<-claimDone
	close(jobs)
	wg.Wait()
	return err
}

// readLoop читает новые записи (">") и раздаёт их воркерам.
func (c *Consumer) readLoop(ctx context.Context, jobs chan<- redis.XMessage) error {
	for {
		res, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    GroupName,
			Consumer: c.name,
			Streams:  []string{StreamName, ">"},
			Count:    readCount,
			Block:    readBlock,
		}).Result()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// redis.Nil — блокирующее чтение истекло без сообщений.
			if errors.Is(err, redis.Nil) {
				continue
			}
			log.Printf("xreadgroup: %v", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(retryDelay):
			}
			continue
		}
		for _, s := range res {
			for _, m := range s.Messages {
				select {
				case jobs <- m:
				case <-ctx.Done():
					return nil
				}
			}
		}
	}
}

// claimLoop забирает pending-записи, зависшие дольше 30 с (упавший воркер
// или перезапуск экземпляра), и возвращает их в обработку.
func (c *Consumer) claimLoop(ctx context.Context, jobs chan<- redis.XMessage) {
	t := time.NewTicker(claimEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cursor := "0-0"
		for {
			msgs, next, err := c.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   StreamName,
				Group:    GroupName,
				Consumer: c.name,
				MinIdle:  minIdle,
				Start:    cursor,
				Count:    readCount,
			}).Result()
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("xautoclaim: %v", err)
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
			if len(msgs) < int(readCount) {
				break // дошли до конца pending
			}
			cursor = next
		}
	}
}

// process обрабатывает одну запись стрима: валидный payload уходит хендлеру
// с подтверждением после обработки, битый — в listings.dead.
func (c *Consumer) process(ctx context.Context, msg redis.XMessage) {
	raw, ok := msg.Values["data"]
	if !ok {
		c.dead(ctx, msg, "", "запись без поля data")
		return
	}
	payload, ok := raw.(string)
	if !ok {
		c.dead(ctx, msg, fmt.Sprintf("%v", raw), "поле data не является строкой")
		return
	}

	event, err := model.ParseEvent([]byte(payload))
	if err != nil {
		c.dead(ctx, msg, payload, err.Error())
		return
	}

	// НФТ-11: уведомление не позднее 5 секунд после события.
	hctx, cancel := context.WithTimeout(ctx, handleBudget)
	c.handle(hctx, event)
	cancel()

	// XACK — только после обработки; redis-операции не должны зависеть от
	// истёкшего бюджета обработки, поэтому отдельный контекст.
	if err := c.rdb.XAck(context.WithoutCancel(ctx), StreamName, GroupName, msg.ID).Err(); err != nil {
		// Не подтверждено: запись останется в pending, XAUTOCLAIM вернёт её.
		log.Printf("xack %s: %v", msg.ID, err)
	}
}

// dead кладёт некорректную запись в listings.dead с причиной и подтверждает её.
// Контракт §2: сначала сохранение, потом подтверждение — при неудаче XADD
// запись остаётся в pending и будет обработана снова.
func (c *Consumer) dead(ctx context.Context, msg redis.XMessage, payload, reason string) {
	err := c.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: DeadStream,
		Values: map[string]any{
			"reason": reason,
			"data":   payload,
			"src_id": msg.ID,
		},
	}).Err()
	if err != nil {
		log.Printf("dead-letter %s (reason: %s): %v", msg.ID, reason, err)
		return
	}
	log.Printf("bad payload %s -> %s: %s", msg.ID, DeadStream, reason)
	if err := c.rdb.XAck(context.WithoutCancel(ctx), StreamName, GroupName, msg.ID).Err(); err != nil {
		log.Printf("xack dead %s: %v", msg.ID, err)
	}
}

// isBusyGroup распознаёт BUSYGROUP — группа уже создана (идемпотентный старт).
func isBusyGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}
