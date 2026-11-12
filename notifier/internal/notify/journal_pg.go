// Реализация Journal на PostgreSQL: таблица notifier.notifications_log
// (ФТ-3.5, схема notifier/migrations/0001_init.sql). Дедуп постоянный —
// UNIQUE (chat_id, listing_id), TTL как в in-memory журнале не нужен.
package notify

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"notifier/internal/model"
)

// PgJournal — журнал уведомлений в notifier.notifications_log.
type PgJournal struct {
	pool *pgxpool.Pool
}

func NewPgJournal(pool *pgxpool.Pool) *PgJournal {
	return &PgJournal{pool: pool}
}

// Sent сообщает, доставлялось ли уже объявление пользователю. Строка со
// статусом 'deferred' доставкой не считается: уведомление ещё в очереди.
func (j *PgJournal) Sent(ctx context.Context, listingID, chatID int64) (bool, error) {
	var sent bool
	err := j.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM notifier.notifications_log
			WHERE chat_id = $1 AND listing_id = $2 AND status IN ('sent', 'unknown')
		)`, chatID, listingID).Scan(&sent)
	if err != nil {
		return false, fmt.Errorf("journal sent check: %w", err)
	}
	return sent, nil
}

// Record фиксирует доставку со статусом ('sent' при успехе отправки,
// 'unknown' при ошибке). Повторная запись той же пары (пользователь,
// объявление) обновляет статус и sent_at — вторую строку UNIQUE не пускает,
// поэтому двойная доставка события дубля не создаёт.
func (j *PgJournal) Record(ctx context.Context, chatID, listingID int64, eventID string, event model.Event, status, mode string) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("journal payload: %w", err)
	}
	// sent_at обязателен сразу при вставке: CHECK notifications_log_sent_at_chk
	// требует его для статусов sent/unknown.
	if _, err := j.pool.Exec(ctx, `
		INSERT INTO notifier.notifications_log
			(chat_id, listing_id, event_id, status, mode, payload, sent_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (chat_id, listing_id)
		DO UPDATE SET status = EXCLUDED.status, sent_at = now()`,
		chatID, listingID, eventID, status, mode, payload); err != nil {
		return fmt.Errorf("journal record: %w", err)
	}
	return nil
}
