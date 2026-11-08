// PgQueue — очередь отложенных уведомлений в notifier.notifications_log:
// строка со статусом 'deferred' одновременно является записью журнала для
// своей пары (chat_id, listing_id) — UNIQUE дедуплицирует и журнал, и очередь
// (ТЗ 3.6, contracts.md §2).
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"notifier/internal/model"
)

// eventSnapshot — снимок события в контрактной форме (contracts.md §2,
// snake_case). PopBatch разбирает payload через model.ParseEvent, который
// ждёт именно такой формат, поэтому хранить его надо не «как.Marshal(Event)»
// (Go-имена полей), а в контрактном виде.
type eventSnapshot struct {
	EventID   string    `json:"event_id"`
	ID        int64     `json:"id"`
	Source    string    `json:"source"`
	DealType  string    `json:"deal_type"`
	Country   string    `json:"country"`
	City      string    `json:"city"`
	Rooms     int       `json:"rooms"`
	Price     float64   `json:"price"`
	Currency  string    `json:"currency"`
	PriceUSD  int64     `json:"price_usd"`
	Address   *string   `json:"address"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

func snapshotOf(e model.Event) eventSnapshot {
	return eventSnapshot{
		EventID:   e.EventID,
		ID:        e.ID,
		Source:    e.Source,
		DealType:  e.DealType,
		Country:   e.Country,
		City:      e.City,
		Rooms:     e.Rooms,
		Price:     e.Price,
		Currency:  e.Currency,
		PriceUSD:  e.PriceUSD,
		Address:   e.Address,
		URL:       e.URL,
		CreatedAt: e.CreatedAt,
	}
}

// PgQueue — очередь отложенных заданий. Транзакция PopBatch держит блокировки
// FOR UPDATE до Mark: параллельный PopBatch (SKIP LOCKED) уже занятые строки
// не видит и берёт следующие — батчи не пересекаются. Внутри сервиса флешер
// один, поэтому транзакция одна на структуру и защищена мьютексом.
type PgQueue struct {
	pool *pgxpool.Pool

	mu sync.Mutex
	tx pgx.Tx // транзакция последнего PopBatch, коммитится в Mark
}

func NewPgQueue(pool *pgxpool.Pool) *PgQueue {
	return &PgQueue{pool: pool}
}

// Postpone кладёт событие в очередь строкой 'deferred'. ON CONFLICT DO
// NOTHING: пара (chat_id, listing_id) уникальна, повторная публикация того же
// события не создаёт второе задание, а существующая строка (deferred или уже
// доставленная) остаётся как есть.
func (q *PgQueue) Postpone(ctx context.Context, chatID int64, e model.Event, mode string) error {
	payload, err := json.Marshal(snapshotOf(e))
	if err != nil {
		return fmt.Errorf("queue payload: %w", err)
	}
	if _, err := q.pool.Exec(ctx, `
		INSERT INTO notifier.notifications_log
			(chat_id, listing_id, event_id, status, mode, payload)
		VALUES ($1, $2, $3, 'deferred', $4, $5)
		ON CONFLICT (chat_id, listing_id) DO NOTHING`,
		chatID, e.ID, e.EventID, mode, payload); err != nil {
		return fmt.Errorf("queue postpone: %w", err)
	}
	return nil
}

// PopBatch возвращает до limit самых старых отложенных заданий
// (индекс notifications_log_deferred_idx). Строки блокируются за этим
// воркером до Mark.
func (q *PgQueue) PopBatch(ctx context.Context, limit int) ([]DeferredJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.tx == nil {
		tx, err := q.pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("queue begin: %w", err)
		}
		q.tx = tx
	}
	rows, err := q.tx.Query(ctx, `
		SELECT chat_id, payload FROM notifier.notifications_log
		WHERE status = 'deferred'
		ORDER BY created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		q.rollbackTx(ctx)
		return nil, fmt.Errorf("queue pop: %w", err)
	}
	defer rows.Close()

	var jobs []DeferredJob
	for rows.Next() {
		var chatID int64
		var payload []byte
		if err := rows.Scan(&chatID, &payload); err != nil {
			q.rollbackTx(ctx)
			return nil, fmt.Errorf("queue pop scan: %w", err)
		}
		e, err := model.ParseEvent(payload)
		if err != nil {
			q.rollbackTx(ctx)
			return nil, fmt.Errorf("queue pop payload: %w", err)
		}
		jobs = append(jobs, DeferredJob{ChatID: chatID, Event: e})
	}
	if err := rows.Err(); err != nil {
		q.rollbackTx(ctx)
		return nil, fmt.Errorf("queue pop rows: %w", err)
	}
	return jobs, nil
}

// Mark фиксирует исход задания: sent/unknown — строка получает статус и
// sent_at, retry — остаётся deferred (вернётся следующим проходом флешера).
// Любой Mark завершает транзакцию PopBatch: блокировки сняты, следующий
// проход откроет её заново. Внутри сервиса флешер один, поэтому досрочное
// снятие блокировок с неотмеченных заданий батча безопасно.
func (q *PgQueue) Mark(ctx context.Context, job DeferredJob, _ string, status string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.tx == nil {
		// Mark без активного PopBatch — короткая автономная транзакция.
		tx, err := q.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("queue begin: %w", err)
		}
		q.tx = tx
	}
	switch status {
	case StatusRetry:
		// UPDATE не нужен: строка уже deferred.
	case StatusSent, StatusUnknown:
		if _, err := q.tx.Exec(ctx, `
			UPDATE notifier.notifications_log
			SET status = $3, sent_at = now()
			WHERE chat_id = $1 AND listing_id = $2 AND status = 'deferred'`,
			job.ChatID, job.Event.ID, status); err != nil {
			q.rollbackTx(ctx)
			return fmt.Errorf("queue mark: %w", err)
		}
	default:
		q.rollbackTx(ctx)
		return fmt.Errorf("queue mark: неизвестный статус %q", status)
	}
	if err := q.tx.Commit(ctx); err != nil {
		q.tx = nil
		return fmt.Errorf("queue commit: %w", err)
	}
	q.tx = nil
	return nil
}

// rollbackTx откатывает незавершённую транзакцию PopBatch: строки остаются
// deferred и будут забраны следующим проходом.
func (q *PgQueue) rollbackTx(ctx context.Context) {
	if q.tx != nil {
		_ = q.tx.Rollback(ctx)
		q.tx = nil
	}
}
