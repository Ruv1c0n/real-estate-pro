// Package notify решает, кому и когда доставлять уведомления по событию:
// дедупликация по журналу, лимит «30 в час» на пользователя и отложенная
// доставка при превышении лимита (ФТ-3.4/3.5, contracts.md §2).
package notify

import (
	"context"
	"log"
	"sync"
	"time"

	"notifier/internal/model"
	"notifier/internal/store"
)

// Sender доставляет уведомление одному пользователю. В режиме бота это
// Telegram, в log-режиме (ФТ-3.6, без TELEGRAM_BOT_TOKEN) — запись в stdout.
type Sender func(ctx context.Context, chatID int64, e model.Event) error

// journalKey — ключ журнала: пара (объявление, пользователь), ФТ-3.5.
type journalKey struct {
	listingID int64
	chatID    int64
}

// Journal — журнал отправленных уведомлений: одному пользователю одно
// объявление отправляется не более одного раза. Реализация в памяти;
// на этапе 2 (миграции схемы notifier) заменяется таблицей
// notifications_log с уникальной парой (user_id, listing_id).
type Journal struct {
	mu      sync.Mutex
	sent    map[journalKey]time.Time
	ttl     time.Duration
	nowFunc func() time.Time
}

func NewJournal(ttl time.Duration) *Journal {
	return &Journal{sent: make(map[journalKey]time.Time), ttl: ttl, nowFunc: time.Now}
}

// Sent сообщает, отправлялось ли уже это объявление этому пользователю.
func (j *Journal) Sent(listingID, chatID int64) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cleanup()
	_, ok := j.sent[journalKey{listingID, chatID}]
	return ok
}

// Record отмечает объявление как отправленное пользователю.
func (j *Journal) Record(listingID, chatID int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cleanup()
	j.sent[journalKey{listingID, chatID}] = j.nowFunc()
}

// cleanup удаляет записи старше ttl, чтобы журнал не рос бесконечно.
func (j *Journal) cleanup() {
	now := j.nowFunc()
	for k, at := range j.sent {
		if now.Sub(at) > j.ttl {
			delete(j.sent, k)
		}
	}
}

// Limiter — скользящее окно «не более N уведомлений за период» на
// пользователя (ТЗ 3.6: 30 в час). На этапе 2 окно считается по журналу
// в PostgreSQL; сейчас — в памяти процесса.
type Limiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	sent    map[int64][]time.Time
	nowFunc func() time.Time
}

func NewLimiter(limit int, window time.Duration) *Limiter {
	return &Limiter{limit: limit, window: window, sent: make(map[int64][]time.Time), nowFunc: time.Now}
}

// Allow проверяет и сразу занимает слот окна. true — отправлять можно.
func (l *Limiter) Allow(chatID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.nowFunc()
	fresh := l.sent[chatID][:0]
	for _, at := range l.sent[chatID] {
		if now.Sub(at) < l.window {
			fresh = append(fresh, at)
		}
	}
	if len(fresh) >= l.limit {
		l.sent[chatID] = fresh
		return false
	}
	l.sent[chatID] = append(fresh, now)
	return true
}

// deferredJob — уведомление, не вместившееся в часовой лимит (contracts.md §2:
// лишние задания откладываются; на этапе 2 — в PostgreSQL).
type deferredJob struct {
	chatID int64
	event  model.Event
}

// Dispatcher сопоставляет событие с подписками и доставляет уведомления.
type Dispatcher struct {
	subs    store.Store
	journal *Journal
	limiter *Limiter
	send    Sender

	deferMu  sync.Mutex
	deferred []deferredJob
}

func NewDispatcher(subs store.Store, journal *Journal, limiter *Limiter, send Sender) *Dispatcher {
	return &Dispatcher{subs: subs, journal: journal, limiter: limiter, send: send}
}

// Handle обрабатывает одно событие из стрима: находит получателей
// (уникальные чаты, ФТ-3.4), пропускает повторную доставку по журналу,
// отправляет сразу или откладывает при исчерпании лимита.
func (d *Dispatcher) Handle(ctx context.Context, e model.Event) {
	for _, chatID := range model.Recipients(e, d.subs.All()) {
		if d.journal.Sent(e.ID, chatID) {
			continue // событие доставлено повторно — не дублируем (ФТ-3.5)
		}
		if d.limiter.Allow(chatID) {
			d.deliver(ctx, chatID, e)
		} else {
			d.postpone(chatID, e)
		}
	}
}

func (d *Dispatcher) deliver(ctx context.Context, chatID int64, e model.Event) {
	if err := d.send(ctx, chatID, e); err != nil {
		// Ошибка отправки фиксируется в журнале: автоматический повтор
		// не выполняется (contracts.md §2 — статус unknown без повтора).
		log.Printf("send listing %d to chat %d: %v", e.ID, chatID, err)
	}
	d.journal.Record(e.ID, chatID)
}

func (d *Dispatcher) postpone(chatID int64, e model.Event) {
	d.deferMu.Lock()
	d.deferred = append(d.deferred, deferredJob{chatID: chatID, event: e})
	d.deferMu.Unlock()
	log.Printf("hourly limit reached: listing %d to chat %d deferred", e.ID, chatID)
}

// RunFlusher периодически отправляет отложенные уведомления,
// когда часовое окно освобождается.
func (d *Dispatcher) RunFlusher(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.Flush(ctx)
		}
	}
}

// Flush делает один проход по отложенным заданиям.
func (d *Dispatcher) Flush(ctx context.Context) {
	d.deferMu.Lock()
	jobs := d.deferred
	d.deferred = nil
	d.deferMu.Unlock()

	var requeue []deferredJob
	for _, job := range jobs {
		if d.journal.Sent(job.event.ID, job.chatID) {
			continue // уже доставлено другим путём
		}
		if d.limiter.Allow(job.chatID) {
			d.deliver(ctx, job.chatID, job.event)
		} else {
			requeue = append(requeue, job)
		}
	}
	if len(requeue) > 0 {
		d.deferMu.Lock()
		d.deferred = append(requeue, d.deferred...)
		d.deferMu.Unlock()
	}
}
