// Package store хранит подписки. Сейчас — в памяти; после SQL-миграций
// схемы notifier (issue #31, этап 2) подменится реализацией на PostgreSQL
// с тем же набором методов.
package store

import (
	"sync"

	"notifier/internal/model"
)

// Store — хранилище подписок notifier.
type Store interface {
	// Add сохраняет подписку и возвращает её с присвоенным ID.
	Add(s model.Subscription) model.Subscription
	// ByChat возвращает подписки одного чата.
	ByChat(chatID int64) []model.Subscription
	// Delete удаляет подписку, только если она принадлежит этому чату.
	Delete(chatID, id int64) bool
	// DeleteAllForChat удаляет все подписки чата и возвращает их число (/stop, ФТ-3.1).
	DeleteAllForChat(chatID int64) int
	// CountForChat — число подписок чата (лимит 10 на пользователя, ТЗ 3.6).
	CountForChat(chatID int64) int
	// All возвращает копию всех подписок (безопасно для чтения из горутин).
	All() []model.Subscription
	// EnsureUser создаёт запись пользователя (/start, ФТ-3.1), если её ещё нет.
	// Повторный вызов — без ошибки. Add вызывает EnsureUser сам, поэтому
	// /subscribe работает и без предварительного /start.
	EnsureUser(chatID int64) error
	// DeleteUser удаляет пользователя (/stop, ФТ-3.1) вместе с его данными —
	// в Postgres каскадом чистятся подписки и журнал — и возвращает число
	// удалённых подписок для ответа бота.
	DeleteUser(chatID int64) (int, error)
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

func (m *Memory) Add(s model.Subscription) model.Subscription {
	// В Postgres подписка ссылается на пользователя (FK): /subscribe без
	// предварительного /start тоже должен работать, поэтому Add создаёт
	// запись пользователя сам — Memory ведёт себя так же.
	_ = m.EnsureUser(s.ChatID)

	m.mu.Lock()
	defer m.mu.Unlock()
	s.ID = m.nextID
	m.nextID++
	m.subs = append(m.subs, s)
	return s
}

func (m *Memory) ByChat(chatID int64) []model.Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []model.Subscription
	for _, s := range m.subs {
		if s.ChatID == chatID {
			out = append(out, s)
		}
	}
	return out
}

func (m *Memory) Delete(chatID, id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.subs {
		if s.ID == id && s.ChatID == chatID {
			m.subs = append(m.subs[:i], m.subs[i+1:]...)
			return true
		}
	}
	return false
}

func (m *Memory) DeleteAllForChat(chatID int64) int {
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
	return n
}

func (m *Memory) CountForChat(chatID int64) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, s := range m.subs {
		if s.ChatID == chatID {
			n++
		}
	}
	return n
}

func (m *Memory) All() []model.Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Subscription, len(m.subs))
	copy(out, m.subs)
	return out
}

// EnsureUser запоминает пользователя; повторный вызов — no-op.
func (m *Memory) EnsureUser(chatID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[chatID] = struct{}{}
	return nil
}

// DeleteUser удаляет пользователя вместе с подписками (в Postgres это делает
// каскад БД) и возвращает их число для ответа бота.
func (m *Memory) DeleteUser(chatID int64) (int, error) {
	n := m.DeleteAllForChat(chatID)
	m.mu.Lock()
	delete(m.users, chatID)
	m.mu.Unlock()
	return n, nil
}
