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

// Postpone добавляет задание в конец очереди. Дедуп, как в Pg-версии
// (ON CONFLICT DO NOTHING), не нужен: повторная публикация события
// отсекается проверкой журнала в Handle до вызова Postpone.
func (q *MemoryQueue) Postpone(_ context.Context, chatID int64, e model.Event, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
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
