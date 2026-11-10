// MemoryQueue — реализация DeferredQueue в памяти для режима без БД и тестов.
package notify

import (
	"context"
	"sync"

	"notifier/internal/model"
)

type MemoryQueue struct {
	mu   sync.Mutex
	jobs []DeferredJob
}

func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{}
}

// Postpone добавляет задание в конец очереди. Повторная публикация события
// (ТЗ 4.3, at-least-once) может прийти, пока задание ещё лежит в очереди:
// журнал отметит доставку только после Flush, так что проверка журнала
// в Handle дубль не отсечёт. Дедуп по паре (chatID, listing) — паритет
// с UNIQUE (chat_id, listing_id) db-режима (PgQueue, ON CONFLICT DO NOTHING):
// без него повтор даёт двойную доставку; очередь короткоживущая, линейный
// O(n) скан приемлем.
func (q *MemoryQueue) Postpone(_ context.Context, chatID int64, e model.Event, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, job := range q.jobs {
		if job.ChatID == chatID && job.Event.ID == e.ID {
			return nil // дубль: задание уже в очереди, второе не создаём
		}
	}
	q.jobs = append(q.jobs, DeferredJob{ChatID: chatID, Event: e})
	return nil
}

// PopBatch забирает самые старые задания: они удаляются из очереди сразу,
// Mark(retry) возвращает их в начало при следующем проходе.
func (q *MemoryQueue) PopBatch(_ context.Context, limit int) ([]DeferredJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if limit > len(q.jobs) {
		limit = len(q.jobs)
	}
	jobs := append([]DeferredJob(nil), q.jobs[:limit]...)
	q.jobs = q.jobs[limit:]
	return jobs, nil
}

// Mark фиксирует исход: sent/unknown — задание уже забрано из очереди,
// делать нечего; retry — возвращаем задание в начало очереди.
func (q *MemoryQueue) Mark(_ context.Context, job DeferredJob, _, status string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if status == StatusRetry {
		q.jobs = append([]DeferredJob{job}, q.jobs...)
	}
	return nil
}
