// Package notify решает, кому и когда доставлять уведомления по событию:
// дедупликация по журналу, лимит «30 в час» на пользователя и отложенная
// доставка при превышении лимита (ФТ-3.4/3.5, contracts.md §2).
package notify

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"notifier/internal/model"
	"notifier/internal/store"
	"notifier/internal/telegram"
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

// StatusRetry — не статус журнала, а инструкция Mark для очереди отложенных:
// задание остаётся в очереди и будет повторено следующим проходом флешера.
const StatusRetry = "retry"

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
// пользователя (ТЗ 3.6: 30 в час), отдельно по режиму доставки
// (contracts.md §2: log учитывается отдельно от Telegram).
type Limiter interface {
	// Allow проверяет окно, но ничего не пишет: окно считается по фактически
	// отправленным уведомлениям (их фиксирует Record в deliver).
	Allow(ctx context.Context, chatID int64, mode string) (bool, error)
}

// DeferredJob — уведомление, не вместившееся в часовой лимит.
type DeferredJob struct {
	ChatID int64
	Event  model.Event
}

// DeferredQueue — очередь отложенных уведомлений (contracts.md §2: лишние
// задания откладываются; на этапе 2 — в PostgreSQL). Хранится в той же
// таблице notifications_log: строка со статусом 'deferred'.
type DeferredQueue interface {
	// Postpone кладёт задание в очередь.
	Postpone(ctx context.Context, chatID int64, e model.Event, mode string) error
	// PopBatch возвращает до limit самых старых заданий из очереди.
	PopBatch(ctx context.Context, limit int) ([]DeferredJob, error)
	// Mark фиксирует исход: sent/unknown — доставка завершена,
	// retry — задание остаётся в очереди.
	Mark(ctx context.Context, job DeferredJob, mode, status string) error
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

// limiterKey — ключ окна in-memory лимитера: пользователь и режим доставки.
type limiterKey struct {
	chatID int64
	mode   string
}

// MemoryLimiter — реализация Limiter в памяти для режима без БД и тестов.
type MemoryLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	sent    map[limiterKey][]time.Time
	nowFunc func() time.Time
}

func NewMemoryLimiter(limit int, window time.Duration) *MemoryLimiter {
	return &MemoryLimiter{limit: limit, window: window, sent: make(map[limiterKey][]time.Time), nowFunc: time.Now}
}

// Allow проверяет и сразу занимает слот окна. true — отправлять можно.
// В отличие от Pg-версии слот занимается до отправки: in-memory режим
// демонстрационный, гонки между экземплярами там не бывает.
func (l *MemoryLimiter) Allow(_ context.Context, chatID int64, mode string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.nowFunc()
	key := limiterKey{chatID, mode}
	fresh := l.sent[key][:0]
	for _, at := range l.sent[key] {
		if now.Sub(at) < l.window {
			fresh = append(fresh, at)
		}
	}
	if len(fresh) >= l.limit {
		l.sent[key] = fresh
		return false, nil
	}
	l.sent[key] = append(fresh, now)
	return true, nil
}

// perRecipientBudget — дедлайн на доставку ОДНОМУ получателю. У получателя
// 2-3 последовательные операции (журнал, лимитер, Telegram HTTP, очередь
// отложенных); единый бюджет на всё событие при десятках получателей
// кончался на середине списка, поэтому дедлайн теперь свой у каждого.
const perRecipientBudget = 5 * time.Second

// Dispatcher сопоставляет событие с подписками и доставляет уведомления.
type Dispatcher struct {
	subs    store.Store
	journal Journal
	limiter Limiter
	queue   DeferredQueue
	send    Sender
	mode    string // telegram / log, для notifications_log.mode
	// perRecipient — дедлайн на одного получателя (см. deliverRecipient);
	// поле незэкспортировано, тесты того же пакета ставят его напрямую.
	perRecipient time.Duration
}

func NewDispatcher(subs store.Store, journal Journal, limiter Limiter, queue DeferredQueue, mode string, send Sender) *Dispatcher {
	return &Dispatcher{subs: subs, journal: journal, limiter: limiter, queue: queue, mode: mode, send: send, perRecipient: perRecipientBudget}
}

// deferredBatchSize — сколько отложенных заданий флешер забирает за один проход.
const deferredBatchSize = 100

// Handle обрабатывает одно событие из стрима: находит получателей
// (уникальные чаты, ФТ-3.4), пропускает повторную доставку по журналу,
// отправляет сразу или откладывает при исчерпании лимита.
func (d *Dispatcher) Handle(ctx context.Context, e model.Event) {
	recipients, err := d.subs.All(ctx)
	if err != nil {
		// Хранилище подписок недоступно: ошибку нельзя трактовать как
		// «получателей нет» — это была бы молчаливая потеря событий. Доставку
		// не начинаем вовсе, чтобы никто не получил частичную рассылку.
		// До условного XACK (issue #53) событие при этом теряется из стрима —
		// осознанный шаг к R10: надёжность даёт подтверждение после полной
		// обработки, а не до неё.
		log.Printf("dispatch: list subscriptions: %v", err)
		return
	}
	for _, chatID := range model.Recipients(e, recipients) {
		d.deliverRecipient(ctx, chatID, e)
	}
}

// deliverRecipient доставляет событие одному получателю в рамках его
// собственного дедлайна. Доставка последовательная, и раньше общий бюджет
// события кончался на середине списка: у хвоста отменялись Allow/Postpone,
// ничего не записывалось, и событие терялось после XACK. Свой дедлайн у
// каждого получателя гарантирует, что хвост получает полный бюджет
// независимо от размера списка.
func (d *Dispatcher) deliverRecipient(ctx context.Context, chatID int64, e model.Event) {
	rctx, cancel := context.WithTimeout(ctx, d.perRecipient)
	defer cancel()

	sent, err := d.journal.Sent(rctx, e.ID, chatID)
	if err != nil {
		// Журнал недоступен — не паникуем и не прерываем доставку
		// остальных получателей: at-least-once допускает повтор.
		log.Printf("journal check listing %d chat %d: %v", e.ID, chatID, err)
	}
	if sent {
		return // событие доставлено повторно — не дублируем (ФТ-3.5)
	}
	ok, err := d.limiter.Allow(rctx, chatID, d.mode)
	if err != nil {
		// Ошибка лимитера трактуется как «слот занят»: задание
		// откладывается и будет повторено флешером.
		log.Printf("limiter check listing %d chat %d: %v", e.ID, chatID, err)
		ok = false
	}
	if ok {
		d.deliver(rctx, chatID, e)
		return
	}
	if err := d.postpone(rctx, chatID, e); err != nil {
		log.Printf("postpone listing %d chat %d: %v", e.ID, chatID, err)
		return
	}
	log.Printf("hourly limit reached: listing %d to chat %d deferred", e.ID, chatID)
}

// postpone возвращает уведомление в очередь отложенных. Запись идёт на
// контексте без отмены (context.WithoutCancel): истёкший пер-получательский
// дедлайн или отмена родителя (например, shutdown) не должны мешать — иначе
// отложенная строка не запишется, не останется ни записи в журнале, ни
// задания в очереди, и уведомление будет потеряно молча после XACK.
// Отложенная строка — единственная гарантия at-least-once через флешер,
// поэтому она важнее дедлайна. Риск «зависшей» записи без дедлайна осознан
// и зеркалит XACK в consumer.process (тот же context.WithoutCancel).
func (d *Dispatcher) postpone(ctx context.Context, chatID int64, e model.Event) error {
	return d.queue.Postpone(context.WithoutCancel(ctx), chatID, e, d.mode)
}

// logRateLimited логирует отказ 429 с рекомендованной паузой из ответа
// Telegram (RateLimitError.RetryAfter); обёртки может не быть — дозорная
// ошибка ErrRateLimited распознаётся и без неё.
func logRateLimited(err error, listingID, chatID int64) {
	var rle *telegram.RateLimitError
	if errors.As(err, &rle) {
		log.Printf("dispatch: rate limited (retry_after %s): listing %d chat %d", rle.RetryAfter, listingID, chatID)
		return
	}
	log.Printf("dispatch: rate limited: listing %d chat %d", listingID, chatID)
}

func (d *Dispatcher) deliver(ctx context.Context, chatID int64, e model.Event) {
	status := StatusSent
	if err := d.send(ctx, chatID, e); err != nil {
		// 429 — явный отказ Telegram с рекомендованной паузой (ТЗ 4.3
		// «Для 429 учитывается Retry-After»), а не неопределённый исход:
		// терминальный unknown (contracts.md §2) здесь потерял бы уведомление.
		// Возвращаем задание в очередь отложенных — повторит флешер; дубль
		// при повторном Postpone съедает UNIQUE (chat_id, listing_id)
		// Pg-очереди (ON CONFLICT DO NOTHING).
		if errors.Is(err, telegram.ErrRateLimited) {
			logRateLimited(err, e.ID, chatID)
			if perr := d.postpone(ctx, chatID, e); perr != nil {
				// Postpone тоже упал — лог и выходим (как в deliverRecipient).
				log.Printf("postpone listing %d chat %d: %v", e.ID, chatID, perr)
			}
			return
		}
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

// Flush делает один проход по очереди отложенных: забирает батч самых
// старых заданий, доставляет, что влезло в лимит, остальное возвращает
// в очередь (retry).
func (d *Dispatcher) Flush(ctx context.Context) {
	jobs, err := d.queue.PopBatch(ctx, deferredBatchSize)
	if err != nil {
		log.Printf("pop deferred batch: %v", err)
		return
	}
	for _, job := range jobs {
		ok, err := d.limiter.Allow(ctx, job.ChatID, d.mode)
		if err != nil {
			// Окно не подтверждено — задание остаётся в очереди.
			log.Printf("limiter check listing %d chat %d: %v", job.Event.ID, job.ChatID, err)
			ok = false
		}
		if !ok {
			if err := d.queue.Mark(ctx, job, d.mode, StatusRetry); err != nil {
				log.Printf("mark retry listing %d chat %d: %v", job.Event.ID, job.ChatID, err)
			}
			continue
		}
		status := StatusSent
		if err := d.send(ctx, job.ChatID, job.Event); err != nil {
			// 429 — явный отказ (ТЗ 4.3), а не неопределённый исход: задание
			// возвращается в очередь (retry), повтор — следующим проходом
			// флешера. Точную паузу Retry-After очередь пока не отслеживает.
			if errors.Is(err, telegram.ErrRateLimited) {
				logRateLimited(err, job.Event.ID, job.ChatID)
				if merr := d.queue.Mark(ctx, job, d.mode, StatusRetry); merr != nil {
					log.Printf("mark retry listing %d chat %d: %v", job.Event.ID, job.ChatID, merr)
				}
				continue
			}
			// contracts.md §2: ошибка отправки — статус unknown, без повтора.
			log.Printf("send listing %d to chat %d: %v", job.Event.ID, job.ChatID, err)
			status = StatusUnknown
		}
		if err := d.queue.Mark(ctx, job, d.mode, status); err != nil {
			log.Printf("mark listing %d chat %d: %v", job.Event.ID, job.ChatID, err)
		}
		// Журнал: для Pg-очереди строка задания и есть запись журнала,
		// Record подтверждает её (upsert по той же паре); для memory-очереди
		// это единственная фиксация доставки.
		if err := d.journal.Record(ctx, job.ChatID, job.Event.ID, job.Event.EventID, job.Event, status, d.mode); err != nil {
			log.Printf("journal record listing %d chat %d: %v", job.Event.ID, job.ChatID, err)
		}
	}
}
