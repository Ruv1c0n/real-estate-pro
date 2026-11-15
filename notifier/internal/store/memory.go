// Package store хранит подписки notifier. Реализации: Memory — в памяти
// процесса (режим без БД и тесты), Postgres — в PostgreSQL схемы notifier
// (см. postgres.go). Все методы принимают контекст и возвращают ошибку:
// вызывающие обязаны различать «нет данных» и «хранилище недоступно».
package store

import (
	"context"
	"sync"

	"notifier/internal/model"
)

// Store — хранилище подписок notifier. ctx и error обязательны: вызывающие
// обязаны различать «нет данных» и «хранилище недоступно».
type Store interface {
	// Add сохраняет подписку и возвращает её с присвоенным ID (ID выдаёт БД).
	Add(ctx context.Context, s model.Subscription) (model.Subscription, error)
	// ByChat возвращает подписки одного чата.
	ByChat(ctx context.Context, chatID int64) ([]model.Subscription, error)
	// Delete удаляет подписку, только если она принадлежит этому чату;
	// false — у чата нет подписки с таким ID.
	Delete(ctx context.Context, chatID, id int64) (bool, error)
	// DeleteAllForChat удаляет все подписки чата и возвращает их число.
	// Бот его не вызывает: /stop идёт через DeleteUser (после #48), метод
	// нужен реализации DeleteUser в Memory.
	DeleteAllForChat(ctx context.Context, chatID int64) (int, error)
	// CountForChat — число подписок чата (лимит 10 на пользователя, ТЗ 3.6).
	CountForChat(ctx context.Context, chatID int64) (int, error)
	// All возвращает копию всех подписок (безопасно для чтения из горутин).
	All(ctx context.Context) ([]model.Subscription, error)
	// EnsureUser создаёт запись пользователя (/start, ФТ-3.1), если её ещё нет.
	// Повторный вызов — без ошибки. Add вызывает EnsureUser сам, поэтому
	// /subscribe работает и без предварительного /start.
	EnsureUser(ctx context.Context, chatID int64) error
	// DeleteUser удаляет пользователя (/stop, ФТ-3.1) вместе с его данными —
	// в Postgres каскадом чистятся подписки и журнал — и возвращает число
	// удалённых подписок для ответа бота.
	DeleteUser(ctx context.Context, chatID int64) (int, error)
}

// Memory — реализация Store в памяти процесса.
type Memory struct {
	mu     sync.RWMutex
	nextID int64
	subs   []model.Subscription
	users  map[int64]struct{}
}

var _ Store = (*Memory)(nil)

func NewMemory() *Memory {
	return &Memory{nextID: 1, users: make(map[int64]struct{})}
}

func (m *Memory) Add(ctx context.Context, s model.Subscription) (model.Subscription, error) {
	// В Postgres подписка ссылается на пользователя (FK): /subscribe без
	// предварительного /start тоже должен работать, поэтому Add создаёт
	// запись пользователя сам — Memory ведёт себя так же.
	if err := m.EnsureUser(ctx, s.ChatID); err != nil {
		return model.Subscription{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	s.ID = m.nextID
	m.nextID++
	m.subs = append(m.subs, s)
	return s, nil
}

func (m *Memory) ByChat(ctx context.Context, chatID int64) ([]model.Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []model.Subscription
	for _, s := range m.subs {
		if s.ChatID == chatID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *Memory) Delete(ctx context.Context, chatID, id int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.subs {
		if s.ID == id && s.ChatID == chatID {
			m.subs = append(m.subs[:i], m.subs[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (m *Memory) DeleteAllForChat(ctx context.Context, chatID int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.subs[:0]
	n := 0
	for _, s := range m.subs {
		if s.ChatID == chatID {
			n++
			continue
		}
		kept = append(kept, s)
	}
	m.subs = kept
	return n, nil
}

func (m *Memory) CountForChat(ctx context.Context, chatID int64) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, s := range m.subs {
		if s.ChatID == chatID {
			n++
		}
	}
	return n, nil
}

func (m *Memory) All(ctx context.Context) ([]model.Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Subscription, len(m.subs))
	copy(out, m.subs)
	return out, nil
}

// EnsureUser запоминает пользователя; повторный вызов — no-op.
func (m *Memory) EnsureUser(ctx context.Context, chatID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[chatID] = struct{}{}
	return nil
}

// DeleteUser удаляет пользователя вместе с подписками (в Postgres это делает
// каскад БД) и возвращает их число для ответа бота.
func (m *Memory) DeleteUser(ctx context.Context, chatID int64) (int, error) {
	n, err := m.DeleteAllForChat(ctx, chatID)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	delete(m.users, chatID)
	m.mu.Unlock()
	return n, nil
}
