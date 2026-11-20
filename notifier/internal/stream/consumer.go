// Package stream читает события из Redis Streams (listings.new) через
// consumer group и раздаёт их пулу воркеров (ФТ-3.3, contracts.md §2).
package stream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"notifier/internal/metrics"
	"notifier/internal/model"
)

const (
	// StreamName — стрим событий новых объявлений (contracts.md §2).
	StreamName = "listings.new"
	// GroupName — consumer group notifier; создание с MKSTREAM и позицией 0.
	GroupName = "notifier"
	// DeadStream — стрим для некорректных payload'ов.
	DeadStream = "listings.dead"

	workerCount      = 8                // пул воркеров, ТЗ 3.6 (дефолт NOTIFIER_WORKERS)
	lenWarnThreshold = int64(10000)     // мягкий порог WARN длины стрима (дефолт NOTIFIER_STREAM_LEN_THRESHOLD)
	minIdle          = 30 * time.Second // pending старше этого срока забирает XAUTOCLAIM
	claimEvery       = 15 * time.Second // период обхода pending
	readBlock        = 5 * time.Second  // блокирующее чтение XREADGROUP
	readCount        = int64(16)        // сообщений за одно чтение
	retryDelay       = 3 * time.Second  // пауза при ошибке Redis

	// ТЗ 3.6: «Порог контроля длины stream 10 000 событий; мягкий порог,
	// без удаления необработанных». Мягкость — в том, что записи никогда
	// не удаляются: превышение порога только логируется (WARN).
	streamLenWarnReset = 9_000       // гистерезис: WARN снова активен ниже этого
	lenCheckEvery      = time.Minute // период проверки XLEN (не каждый проход)
)

// Handler обрабатывает одно событие. Обработка фиксирует доставку
// (журнал/отложенные задания), после чего консьюмер подтверждает запись.
// Ошибка возвращается ТОЛЬКО при временной инфраструктурной сбое (Postgres,
// Redis) — ТЗ 4.3 «Временные ошибки БД/Redis повторяются»: консьюмер не
// выполняет XACK, запись остаётся в pending и повторяется через XAUTOCLAIM.
// Ошибки, обработанные внутри (отказ Telegram 429/сеть, статус unknown
// после отправки), ошибкой события не являются.
type Handler func(ctx context.Context, e model.Event) error

// Redis — операции go-redis, нужные консьюмеру и очистителю (reaper.go);
// UniversalClient их удовлетворяет.
type Redis interface {
	XGroupCreateMkStream(ctx context.Context, stream, group, start string) *redis.StatusCmd
	XReadGroup(ctx context.Context, a *redis.XReadGroupArgs) *redis.XStreamSliceCmd
	XAutoClaim(ctx context.Context, a *redis.XAutoClaimArgs) *redis.XAutoClaimCmd
	XAck(ctx context.Context, stream, group string, ids ...string) *redis.IntCmd
	XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd
	// Операции безопасной очистки стрима (reaper.go, ТЗ 4.3).
	XPending(ctx context.Context, stream, group string) *redis.XPendingCmd
	XInfoGroups(ctx context.Context, stream string) *redis.XInfoGroupsCmd
	XRange(ctx context.Context, stream, start, stop string) *redis.XMessageSliceCmd
	XDel(ctx context.Context, stream string, ids ...string) *redis.IntCmd
	XLen(ctx context.Context, stream string) *redis.IntCmd
}

// Consumer читает стрим группой notifier: цикл XREADGROUP раздаёт записи
// пулу воркеров, фоновый XAUTOCLAIM забирает зависшие pending-записи.
// Число воркеров и мягкий порог WARN длины стрима настраиваются из main
// (NOTIFIER_WORKERS, NOTIFIER_STREAM_LEN_THRESHOLD; ТЗ 3.6), дефолты —
// константы workerCount и lenWarnThreshold.
type Consumer struct {
	rdb     Redis
	name    string
	handle  Handler
	workers int
	lenWarn int64 // мягкий порог: длина стрима больше него — WARN, не фатально
}

// NewConsumer создаёт консьюмер: workers — размер пула воркеров, lenWarn —
// порог WARN длины стрима.
func NewConsumer(rdb Redis, consumerName string, workers int, lenWarn int64, handle Handler) *Consumer {
	return &Consumer{rdb: rdb, name: consumerName, handle: handle, workers: workers, lenWarn: lenWarn}
}

// DefaultWorkers и DefaultLenWarnThreshold — дефолты по ТЗ 3.6 для параметров,
// читаемых из окружения в main.
func DefaultWorkers() int            { return workerCount }
func DefaultLenWarnThreshold() int64 { return lenWarnThreshold }

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
	slog.Info("consumer group ready", "group", GroupName, "stream", StreamName, "consumer", c.name)

	jobs := make(chan redis.XMessage, readCount)

	var wg sync.WaitGroup
	for i := 0; i < c.workers; i++ {
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
	var (
		lastLenCheck time.Time // XLEN проверяется не каждый проход, а раз в lenCheckEvery
		lenWarned    bool      // гистерезис WARN: не спамить, пока стрим длинный
	)
	for {
		if time.Since(lastLenCheck) >= lenCheckEvery {
			lastLenCheck = time.Now()
			lenWarned = c.warnStreamLen(ctx, lenWarned)
		}
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
			slog.Warn("consumer: xreadgroup failed", "error", err)
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

// streamLenWarnState решает, пора ли логировать WARN по длине стрима, и
// возвращает новое состояние гистерезиса. WARN выдаётся, когда длина впервые
// достигает threshold (настроенный мягкий порог, c.lenWarn); пока длина не
// упадёт ниже streamLenWarnReset, повторных WARN не будет — при стабильно
// длинном стриме лог не заспамивается каждым проходом чтения (после сброса
// следующий выход за порог снова предупредит).
func streamLenWarnState(length int64, warned bool, threshold int64) (warn, newWarned bool) {
	if warned {
		return false, length >= streamLenWarnReset // ниже reset — гистерезис снят
	}
	if length >= threshold {
		return true, true
	}
	return false, false
}

// warnStreamLen проверяет XLEN стрима и логирует WARN при превышении мягкого
// порога (ТЗ 3.6: «Порог контроля длины stream 10 000 событий; мягкий порог,
// без удаления необработанных»). Ничего не удаляет и не обрезает (MAXLEN
// в XADD запрещён ТЗ 4.3) — только сигнал оператору. Ошибка Redis
// игнорируется: это телеметрия рядом с горячим циклом чтения, сбой XLEN
// не должен мешать обработке (состояние гистерезиса не меняется).
func (c *Consumer) warnStreamLen(ctx context.Context, warned bool) bool {
	n, err := c.rdb.XLen(ctx, StreamName).Result()
	if err != nil {
		return warned
	}
	warn, state := streamLenWarnState(n, warned, c.lenWarn)
	if warn {
		slog.Warn("stream length above soft threshold — необработанные записи не удаляются (ТЗ 3.6)",
			"stream", StreamName, "length", n, "threshold", c.lenWarn, "consumer", c.name)
	}
	return state
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
		// Мягкий порог длины стрима (ТЗ 3.6, порог настраивается): рост
		// длины — симптом отставания группы от публикации (события копятся
		// непрочитанными). Это WARN-телеметрия: доставку не прерывает и
		// обрезку не запускает — до безопасной границы стрим не режется.
		if l, err := c.rdb.XLen(context.WithoutCancel(ctx), StreamName).Result(); err != nil {
			slog.Warn("consumer: xlen probe failed", "error", err)
		} else if l > c.lenWarn {
			slog.Warn("stream length exceeds threshold — consumer may be lagging",
				"stream", StreamName, "length", l, "threshold", c.lenWarn)
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
					slog.Warn("consumer: xautoclaim failed", "error", err)
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

	// НФТ-11 — это p95 end-to-end < 5 с при профиле нагрузки «1 событие / 5 с»,
	// а не бюджет обработки: при последовательной доставке десяткам получателей
	// общий бюджет события кончался на середине списка — у хвоста отменялись
	// Allow/Postpone, ничего не записывалось, и безусловный XACK (issue #53)
	// терял уведомления систематически. Дедлайн пер-получательский
	// (notify.perRecipientBudget, 5 с на каждого chatID), а общий бюджет события
	// вернули как eventBudget 25 с (notify.go): он меньше minIdle 30 с, поэтому
	// живой воркер не перезабирается XAUTOCLAIM — при исчерпании бюджета остаток
	// получателей уходит в deferred-очередь (потери нет), продлевать владение
	// не требуется. От зависшей обработки защиту даёт тот же eventBudget +
	// XAUTOCLAIM; конкурентный повтор гасит дедуп по журналу (ФТ-3.5).
	// Ошибка хендлера — временный инфраструктурный сбой (Postgres/Redis),
	// ТЗ 4.3: подтверждать нельзя — запись остаётся в pending и будет
	// подхвачена XAUTOCLAIM (minIdle 30 с; eventBudget 25 с меньше порога,
	// поэтому живой воркер не перезабирается — см. notify.eventBudget).
	if err := c.handle(ctx, event); err != nil {
		metrics.ProcessErrors.Inc()
		slog.Error("consumer: handle failed — не подтверждено, запись остаётся в pending",
			"stream_msg_id", msg.ID, "event_id", event.EventID, "error", err)
		return
	}

	// XACK — только после успешной обработки; redis-операции не должны
	// зависеть от отмены контекста обработки (в т.ч. при остановке сервиса),
	// поэтому отдельный контекст.
	if err := c.rdb.XAck(context.WithoutCancel(ctx), StreamName, GroupName, msg.ID).Err(); err != nil {
		// Не подтверждено: запись останется в pending, XAUTOCLAIM вернёт её.
		slog.Warn("consumer: xack failed — запись вернётся по XAUTOCLAIM", "stream_msg_id", msg.ID, "error", err)
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
		slog.Error("consumer: dead-letter write failed — запись не подтверждена", "stream_msg_id", msg.ID, "reason", reason, "error", err)
		return
	}
	slog.Warn("consumer: bad payload moved to dead stream", "stream_msg_id", msg.ID, "dead_stream", DeadStream, "reason", reason)
	metrics.DeadLetter.Inc()
	if err := c.rdb.XAck(context.WithoutCancel(ctx), StreamName, GroupName, msg.ID).Err(); err != nil {
		slog.Warn("consumer: xack dead failed", "stream_msg_id", msg.ID, "error", err)
	}
}

// isBusyGroup распознаёт BUSYGROUP — группа уже создана (идемпотентный старт).
func isBusyGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}
