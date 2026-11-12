// PgLimiter — окно «не более limit уведомлений за скользящий час»
// по таблице notifier.notifications_log (ТЗ 3.6, contracts.md §2).
package notify

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PgLimiter struct {
	pool  *pgxpool.Pool
	limit int
}

func NewPgLimiter(pool *pgxpool.Pool, limit int) *PgLimiter {
	return &PgLimiter{pool: pool, limit: limit}
}

// Allow сообщает, есть ли свободный слот окна, и ничего не пишет: окно
// считается по фактически отправленным уведомлениям — их фиксирует Record
// в deliver, проставляя sent_at. Гонка Allow→Record у параллельных доставок
// одному чату (слот освобождается только после записи) признана приемлемой:
// учебный проект, всплеск может кратковременно превысить лимит на единицы.
//
// Запрос бьёт в индекс notifications_log_rate_idx (chat_id, mode, sent_at)
// WHERE sent_at IS NOT NULL; режимы telegram/log считаются раздельно.
func (l *PgLimiter) Allow(ctx context.Context, chatID int64, mode string) (bool, error) {
	var n int
	err := l.pool.QueryRow(ctx, `
		SELECT count(*) FROM notifier.notifications_log
		WHERE chat_id = $1 AND mode = $2
		  AND sent_at > now() - interval '1 hour'
		  AND status IN ('sent', 'unknown')`, chatID, mode).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("limiter allow: %w", err)
	}
	return n < l.limit, nil
}
