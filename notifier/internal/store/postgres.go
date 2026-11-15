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
	"log"

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

// Интерфейс Store контекст не принимает и ошибок не возвращает — менять его
// нельзя (issue #31, этап 2). Контекст и ошибка живут в парных методах ниже;
// интерфейсные обёртки вызывают их с context.Background() и логируют ошибку
// вместо паники.

func (p *Postgres) Add(s model.Subscription) model.Subscription {
	added, err := p.add(context.Background(), s)
	if err != nil {
		log.Printf("store: добавить подписку (chat %d): %v", s.ChatID, err)
		return s // ID остаётся нулевым: строка не вставлена
	}
	return added
}

func (p *Postgres) add(ctx context.Context, s model.Subscription) (model.Subscription, error) {
	// FK subscriptions.chat_id → users.chat_id: пользователь должен существовать.
	// Создаём запись сами (ON CONFLICT DO NOTHING — идемпотентно), чтобы
	// /subscribe работал и без предварительного /start.
	if _, err := p.pool.Exec(ctx,
		`INSERT INTO notifier.users (chat_id) VALUES ($1) ON CONFLICT (chat_id) DO NOTHING`,
		s.ChatID); err != nil {
		return model.Subscription{}, fmt.Errorf("ensure user: %w", err)
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
		return model.Subscription{}, fmt.Errorf("insert subscription: %w", err)
	}
	return s, nil
}

func (p *Postgres) ByChat(chatID int64) []model.Subscription {
	subs, err := p.byChat(context.Background(), chatID)
	if err != nil {
		log.Printf("store: подписки чата %d: %v", chatID, err)
		return nil
	}
	return subs
}

func (p *Postgres) byChat(ctx context.Context, chatID int64) ([]model.Subscription, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, chat_id, deal_type, country, city, rooms, min_price_usd, max_price_usd
		FROM notifier.subscriptions
		WHERE chat_id = $1
		ORDER BY id`, chatID)
	if err != nil {
		return nil, fmt.Errorf("select subscriptions: %w", err)
	}
	return collectSubscriptions(rows)
}

func (p *Postgres) Delete(chatID, id int64) bool {
	deleted, err := p.delete(context.Background(), chatID, id)
	if err != nil {
		log.Printf("store: удалить подписку %d (chat %d): %v", id, chatID, err)
		return false
	}
	return deleted
}

func (p *Postgres) delete(ctx context.Context, chatID, id int64) (bool, error) {
	ct, err := p.pool.Exec(ctx,
		`DELETE FROM notifier.subscriptions WHERE id = $1 AND chat_id = $2`, id, chatID)
	if err != nil {
		return false, fmt.Errorf("delete subscription: %w", err)
	}
	return ct.RowsAffected() == 1, nil
}

func (p *Postgres) DeleteAllForChat(chatID int64) int {
	n, err := p.deleteAllForChat(context.Background(), chatID)
	if err != nil {
		log.Printf("store: удалить подписки чата %d: %v", chatID, err)
		return 0
	}
	return n
}

func (p *Postgres) deleteAllForChat(ctx context.Context, chatID int64) (int, error) {
	ct, err := p.pool.Exec(ctx,
		`DELETE FROM notifier.subscriptions WHERE chat_id = $1`, chatID)
	if err != nil {
		return 0, fmt.Errorf("delete chat subscriptions: %w", err)
	}
	return int(ct.RowsAffected()), nil
}

func (p *Postgres) CountForChat(chatID int64) int {
	n, err := p.countForChat(context.Background(), chatID)
	if err != nil {
		log.Printf("store: число подписок чата %d: %v", chatID, err)
		return 0
	}
	return n
}

func (p *Postgres) countForChat(ctx context.Context, chatID int64) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifier.subscriptions WHERE chat_id = $1`, chatID).
		Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count subscriptions: %w", err)
	}
	return n, nil
}

func (p *Postgres) All() []model.Subscription {
	subs, err := p.all(context.Background())
	if err != nil {
		log.Printf("store: все подписки: %v", err)
		return nil
	}
	return subs
}

func (p *Postgres) all(ctx context.Context) ([]model.Subscription, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, chat_id, deal_type, country, city, rooms, min_price_usd, max_price_usd
		FROM notifier.subscriptions
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("select all subscriptions: %w", err)
	}
	return collectSubscriptions(rows)
}

// EnsureUser создаёт запись пользователя (/start, ФТ-3.1); повторный вызов —
// no-op, без ошибки.
func (p *Postgres) EnsureUser(chatID int64) error {
	_, err := p.pool.Exec(context.Background(),
		`INSERT INTO notifier.users (chat_id) VALUES ($1) ON CONFLICT (chat_id) DO NOTHING`,
		chatID)
	if err != nil {
		return fmt.Errorf("ensure user: %w", err)
	}
	return nil
}

// DeleteUser удаляет пользователя (/stop, ФТ-3.1): БД каскадом чистит его
// подписки и журнал (ON DELETE CASCADE). Число подписок считаем ДО удаления —
// каскад их сотрёт, а боту нужен ответ «удалено подписок — N».
func (p *Postgres) DeleteUser(chatID int64) (int, error) {
	ctx := context.Background()
	n, err := p.countForChat(ctx, chatID)
	if err != nil {
		return 0, fmt.Errorf("count subscriptions before delete user: %w", err)
	}
	if _, err := p.pool.Exec(ctx,
		`DELETE FROM notifier.users WHERE chat_id = $1`, chatID); err != nil {
		return 0, fmt.Errorf("delete user: %w", err)
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
