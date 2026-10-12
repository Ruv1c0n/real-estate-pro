// Package store хранит подписки. Сейчас — в памяти; позже заменим на Postgres
// с тем же набором методов.
package store

import (
	"sync"

	"notifier/internal/model"
)

type sentKey struct{ chatID, listingID int64 }

type Memory struct {
	mu     sync.RWMutex
	nextID int64
	subs   []model.Subscription
	sent   map[sentKey]struct{} // журнал отправленных уведомлений (аналог notifications_log)
}

func NewMemory() *Memory {
	return &Memory{nextID: 1, sent: map[sentKey]struct{}{}}
}

// Add сохраняет подписку и возвращает её с присвоенным ID.
func (m *Memory) Add(s model.Subscription) model.Subscription {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.ID = m.nextID
	m.nextID++
	m.subs = append(m.subs, s)
	return s
}

// ByChat возвращает подписки одного чата.
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

// Delete удаляет подписку, только если она принадлежит этому чату.
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

// All возвращает копию всех подписок (безопасно для чтения из горутин).
func (m *Memory) All() []model.Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Subscription, len(m.subs))
	copy(out, m.subs)
	return out
}

// MarkSent атомарно записывает в журнал пару (чат, объявление).
// Возвращает false, если уведомление уже отправлялось (ФТ-3.5):
// это защищает от дублей при повторной доставке события из стрима.
func (m *Memory) MarkSent(chatID, listingID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := sentKey{chatID, listingID}
	if _, ok := m.sent[k]; ok {
		return false
	}
	m.sent[k] = struct{}{}
	return true
}