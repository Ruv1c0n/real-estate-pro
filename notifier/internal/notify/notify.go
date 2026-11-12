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

// Режим доставки, попадает в notifications_log.mode (учитываются отдельно,
// contracts.md §2). Выбирается в main.go по наличию TELEGRAM_BOT_TOKEN.
const (
	ModeTelegram = "telegram"
	ModeLog      = "log"
)

// Статусы записи журнала из схемы 0001 (notifications_log_status_chk).
// 'failed' не используется: ошибки отправки фиксируются как 'unknown'
// без автоматического повтора (contracts.md §2).
const (
	StatusSent    = "sent"
	StatusUnknown = "unknown"
)

// Journal — журнал отправленных уведомлений: одному пользователю одно
// объявление доставляется не более одного раза (ФТ-3.5). Дедуп постоянный,
// без TTL: в PostgreSQL это UNIQUE (chat_id, listing_id).
type Journal interface {
	// Sent сообщает, доставлялось ли уже это объявление этому пользователю.
	Sent(ctx context.Context, listingID, chatID int64) (bool, error)
	// Record фиксирует доставку со статусом ('sent'/'unknown'), снимком
	// события и режимом отправки.
	Record(ctx context.Context, chatID, listingID int64, eventID string, event model.Event, status, mode string) error
}

// Limiter — скользящее окно «не более N уведомлений за период» на
// пользователя (ТЗ 3.6: 30 в час). На этапе 2 окно считается по журналу
// в PostgreSQL; сейчас — в памяти процесса.
type Limiter interface {
	// Allow проверяет и сразу занимает слот окна. true — отправлять можно.
	Allow(chatID int64) bool
}

// journalKey — ключ in-memory журнала: пара (объявление, пользователь).
type journalKey struct {
	listingID int64
	chatID    int64
}

// memoryJournalTTL — срок жизни записи in-memory журнала. Костыль режима
// без БД, чтобы карта не росла бесконечно: постоянный дедуп даёт только
// таблица notifications_log (UNIQUE без TTL).
const memoryJournalTTL = 24 * time.Hour

// MemoryJournal — реализация Journal в памяти для режима без БД и тестов.
type MemoryJournal struct {
	mu      sync.Mutex
	sent    map[journalKey]time.Time
	nowFunc func() time.Time
}

func NewMemoryJournal() *MemoryJournal {
	return &MemoryJournal{sent: make(map[journalKey]time.Time), nowFunc: time.Now}
}

// Sent сообщает, отправлялось ли уже это объявление этому пользователю.
func (j *MemoryJournal) Sent(_ context.Context, listingID, chatID int64) (bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cleanup()
	_, ok := j.sent[journalKey{listingID, chatID}]
	return ok, nil
}

// Record отмечает объявление как отправленное пользователю. Статус, снимок
// события и режим in-memory журнал не хранит — для этого есть таблица
// notifications_log.
func (j *MemoryJournal) Record(_ context.Context, chatID, listingID int64, _ string, _ model.Event, _, _ string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cleanup()
	j.sent[journalKey{listingID, chatID}] = j.nowFunc()
	return nil
}

// cleanup удаляет записи старше TTL, чтобы журнал не рос бесконечно.
func (j *MemoryJournal) cleanup() {
	now := j.nowFunc()
	for k, at := range j.sent {
		if now.Sub(at) > memoryJournalTTL {
			delete(j.sent, k)
		}
	}
}

// MemoryLimiter — реализация Limiter в памяти для режима без БД и тестов.
type MemoryLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	sent    map[int64][]time.Time
	nowFunc func() time.Time
}

func NewMemoryLimiter(limit int, window time.Duration) *MemoryLimiter {
	return &MemoryLimiter{limit: limit, window: window, sent: make(map[int64][]time.Time), nowFunc: time.Now}
}

// Allow проверяет и сразу занимает слот окна. true — отправлять можно.
func (l *MemoryLimiter) Allow(chatID int64) bool {
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
	journal Journal
	limiter Limiter
	send    Sender
	mode    string // telegram / log, для notifications_log.mode

	deferMu  sync.Mutex
	deferred []deferredJob
}

func NewDispatcher(subs store.Store, journal Journal, limiter Limiter, mode string, send Sender) *Dispatcher {
	return &Dispatcher{subs: subs, journal: journal, limiter: limiter, mode: mode, send: send}
}

// Handle обрабатывает одно событие из стрима: находит получателей
// (уникальные чаты, ФТ-3.4), пропускает повторную доставку по журналу,
// отправляет сразу или откладывает при исчерпании лимита.
func (d *Dispatcher) Handle(ctx context.Context, e model.Event) {
	for _, chatID := range model.Recipients(e, d.subs.All()) {
		sent, err := d.journal.Sent(ctx, e.ID, chatID)
		if err != nil {
			// Журнал недоступен — не паникуем и не прерываем доставку
			// остальных получателей: at-least-once допускает повтор.
			log.Printf("journal check listing %d chat %d: %v", e.ID, chatID, err)
		}
		if sent {
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
	status := StatusSent
	if err := d.send(ctx, chatID, e); err != nil {
		// Ошибка отправки фиксируется в журнале как unknown: автоматический
		// повтор не выполняется (contracts.md §2 — статус unknown без повтора).
		log.Printf("send listing %d to chat %d: %v", e.ID, chatID, err)
		status = StatusUnknown
	}
	if err := d.journal.Record(ctx, chatID, e.ID, e.EventID, e, status, d.mode); err != nil {
		// Недоступная БД не должна валить сервис: доставка уже прошла,
		// повтор будет только при следующей публикации события.
		log.Printf("journal record listing %d chat %d: %v", e.ID, chatID, err)
	}
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
		sent, err := d.journal.Sent(ctx, job.event.ID, job.chatID)
		if err != nil {
			log.Printf("journal check listing %d chat %d: %v", job.event.ID, job.chatID, err)
		}
		if sent {
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
