// postgres.go — реализация Store на PostgreSQL (схема notifier).
// Схема задана миграцией notifier/migrations/0001_init.sql (не редактировать).
//
// Маппинг «NULL = любое значение»:
//   - deal_type, country, city: пустая строка модели ↔ NULL в БД;
//   - rooms, min_price_usd, max_price_usd: nil-указатель модели ↔ NULL в БД.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"notifier/internal/model"
)

// Postgres хранит подписки в таблице notifier.subscriptions.
type Postgres struct {
	pool *pgxpool.Pool
}

var _ Store = (*Postgres)(nil)

// NewPostgres принимает открытый пул соединений (internal/db.Connect).
func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

// Add сохраняет подписку и возвращает её с реальным ID из БД. Ошибка означает,
// что строка не вставлена — вызывающий обязан отличить это от успеха.
func (p *Postgres) Add(ctx context.Context, s model.Subscription) (model.Subscription, error) {
	// FK subscriptions.chat_id → users.chat_id: пользователь должен существовать.
	// Создаём запись сами (ON CONFLICT DO NOTHING — идемпотентно), чтобы
	// /subscribe работал и без предварительного /start — тот же SQL, что и
	// в EnsureUser (единая точка вставки, без дублирования).
	if err := p.ensureUser(ctx, s.ChatID); err != nil {
		return model.Subscription{}, fmt.Errorf("store: add subscription (chat %d): %w", s.ChatID, err)
	}

	// NULLIF превращает пустую строку модели в NULL («любое значение»);
	// nil-указатели pgx записывает как NULL без преобразований.
	row := p.pool.QueryRow(ctx, `
		INSERT INTO notifier.subscriptions
			(chat_id, deal_type, country, city, rooms, min_price_usd, max_price_usd)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7)
		RETURNING id`,
		s.ChatID, s.DealType, s.Country, s.City, s.Rooms, s.MinPriceUSD, s.MaxPriceUSD)
	if err := row.Scan(&s.ID); err != nil {
		return model.Subscription{}, fmt.Errorf("store: add subscription (chat %d): insert: %w", s.ChatID, err)
	}
	return s, nil
}

func (p *Postgres) ByChat(ctx context.Context, chatID int64) ([]model.Subscription, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, chat_id, deal_type, country, city, rooms, min_price_usd, max_price_usd
		FROM notifier.subscriptions
		WHERE chat_id = $1
		ORDER BY id`, chatID)
	if err != nil {
		return nil, fmt.Errorf("store: list subscriptions (chat %d): %w", chatID, err)
	}
	subs, err := collectSubscriptions(rows)
	if err != nil {
		return nil, fmt.Errorf("store: list subscriptions (chat %d): %w", chatID, err)
	}
	return subs, nil
}

// Delete удаляет подписку чата; false — у чата нет подписки с таким ID.
func (p *Postgres) Delete(ctx context.Context, chatID, id int64) (bool, error) {
	ct, err := p.pool.Exec(ctx,
		`DELETE FROM notifier.subscriptions WHERE id = $1 AND chat_id = $2`, id, chatID)
	if err != nil {
		return false, fmt.Errorf("store: delete subscription %d (chat %d): %w", id, chatID, err)
	}
	return ct.RowsAffected() == 1, nil
}

func (p *Postgres) DeleteAllForChat(ctx context.Context, chatID int64) (int, error) {
	ct, err := p.pool.Exec(ctx,
		`DELETE FROM notifier.subscriptions WHERE chat_id = $1`, chatID)
	if err != nil {
		return 0, fmt.Errorf("store: delete subscriptions (chat %d): %w", chatID, err)
	}
	return int(ct.RowsAffected()), nil
}

func (p *Postgres) CountForChat(ctx context.Context, chatID int64) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifier.subscriptions WHERE chat_id = $1`, chatID).
		Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count subscriptions (chat %d): %w", chatID, err)
	}
	return n, nil
}

func (p *Postgres) All(ctx context.Context) ([]model.Subscription, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, chat_id, deal_type, country, city, rooms, min_price_usd, max_price_usd
		FROM notifier.subscriptions
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list all subscriptions: %w", err)
	}
	subs, err := collectSubscriptions(rows)
	if err != nil {
		return nil, fmt.Errorf("store: list all subscriptions: %w", err)
	}
	return subs, nil
}

// EnsureUser создаёт запись пользователя (/start, ФТ-3.1); повторный вызов —
// no-op, без ошибки.
func (p *Postgres) EnsureUser(ctx context.Context, chatID int64) error {
	if err := p.ensureUser(ctx, chatID); err != nil {
		return fmt.Errorf("store: ensure user (chat %d): %w", chatID, err)
	}
	return nil
}

// ensureUser — общая вставка пользователя для Add и EnsureUser. Без
// префикса источника: его добавляют вызывающие, чтобы сообщение показывало
// операцию, в которой случился сбой.
func (p *Postgres) ensureUser(ctx context.Context, chatID int64) error {
	if _, err := p.pool.Exec(ctx,
		`INSERT INTO notifier.users (chat_id) VALUES ($1) ON CONFLICT (chat_id) DO NOTHING`,
		chatID); err != nil {
		return fmt.Errorf("ensure user (chat %d): %w", chatID, err)
	}
	return nil
}

// DeleteUser удаляет пользователя (/stop, ФТ-3.1): БД каскадом чистит его
// подписки и журнал (ON DELETE CASCADE). Число подписок считаем ДО удаления —
// каскад их сотрёт, а боту нужен ответ «удалено подписок — N».
func (p *Postgres) DeleteUser(ctx context.Context, chatID int64) (int, error) {
	n, err := p.CountForChat(ctx, chatID)
	if err != nil {
		// Ошибка уже обёрнута с префиксом «store:» в CountForChat —
		// пробрасываем как есть, чтобы не дублировать префикс.
		return 0, err
	}
	if _, err := p.pool.Exec(ctx,
		`DELETE FROM notifier.users WHERE chat_id = $1`, chatID); err != nil {
		return 0, fmt.Errorf("store: delete user (chat %d): %w", chatID, err)
	}
	return n, nil
}

// collectSubscriptions дочитывает выборку подписок в порядке ORDER BY.
func collectSubscriptions(rows pgx.Rows) ([]model.Subscription, error) {
	defer rows.Close()

	var subs []model.Subscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}
	return subs, nil
}

// scanSubscription читает одну строку выборки; NULL читается в указатель
// и превращается в пустую строку или nil-указатель модели.
func scanSubscription(row pgx.Row) (model.Subscription, error) {
	var (
		s                  model.Subscription
		dealType           *string
		country            *string
		city               *string
		rooms              *int
		minPrice, maxPrice *int64
	)
	if err := row.Scan(&s.ID, &s.ChatID, &dealType, &country, &city, &rooms, &minPrice, &maxPrice); err != nil {
		return model.Subscription{}, err
	}
	s.DealType = derefStr(dealType)
	s.Country = derefStr(country)
	s.City = derefStr(city)
	s.Rooms = rooms
	s.MinPriceUSD = minPrice
	s.MaxPriceUSD = maxPrice
	return s, nil
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
