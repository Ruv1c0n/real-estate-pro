// MemoryQueue — реализация DeferredQueue в памяти для режима без БД и тестов.
package notify

import (
	"context"
	"strconv"
	"sync"

	"notifier/internal/model"
)

type MemoryQueue struct {
	mu   sync.Mutex
	jobs []DeferredJob
	// inflight — пары, забранные PopBatch и ещё не отмеченные Mark. Дедуп
	// Postpone обязан видеть их: окно PopBatch→Mark невидимо для скана
	// очереди, и повторная публикация события «в полёте» дала бы вторую
	// копию (ТЗ 4.3: «Повтор после сбоя может создать несколько записей
	// stream с одним event_id»). Ключ «chatID:listingID».
	inflight map[string]bool
}

func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{inflight: make(map[string]bool)}
}

// jobKey — ключ дедупа пары (чат, объявление).
func jobKey(chatID, listingID int64) string {
	return strconv.FormatInt(chatID, 10) + ":" + strconv.FormatInt(listingID, 10)
}

// Postpone добавляет задание в конец очереди. Повторная публикация события
// (ТЗ 4.3, at-least-once) может прийти, пока задание лежит в очереди ИЛИ
// находится «в полёте» между PopBatch и Mark: журнал отметит доставку только
// после отправки, так что проверка журнала в Handle дубль не отсечёт. Дедуп
// по паре (chatID, listing) и по очереди, и по inflight — паритет с UNIQUE
// (chat_id, listing_id) db-режима (PgQueue, ON CONFLICT DO NOTHING): без него
// повтор даёт двойную доставку. Очередь короткоживущая, линейный O(n) скан
// приемлем.
func (q *MemoryQueue) Postpone(_ context.Context, chatID int64, e model.Event, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := jobKey(chatID, e.ID)
	if q.inflight[key] {
		return nil // дубль: задание «в полёте», второе не создаём
	}
	for _, job := range q.jobs {
		if job.ChatID == chatID && job.Event.ID == e.ID {
			return nil // дубль: задание уже в очереди, второе не создаём
		}
	}
	q.jobs = append(q.jobs, DeferredJob{ChatID: chatID, Event: e})
	return nil
}

// PopBatch забирает самые старые задания: они удаляются из очереди и
// помечаются «в полёте» (видны дедупу Postpone), Mark снимает пометку.
func (q *MemoryQueue) PopBatch(_ context.Context, limit int) ([]DeferredJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if limit > len(q.jobs) {
		limit = len(q.jobs)
	}
	jobs := append([]DeferredJob(nil), q.jobs[:limit]...)
	q.jobs = q.jobs[limit:]
	for _, job := range jobs {
		q.inflight[jobKey(job.ChatID, job.Event.ID)] = true
	}
	return jobs, nil
}

// Mark фиксирует исход: sent/unknown — задание обработано, снимаем пометку
// «в полёте» и убираем его из очереди, если оно там ещё лежит (так бывает,
// когда Flush закрывает задание как «уже отправленное» без забора);
// retry — снимаем пометку и возвращаем задание в начало очереди (дальше его
// снова видит дедуп Postpone).
func (q *MemoryQueue) Mark(_ context.Context, job DeferredJob, _, status string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.inflight, jobKey(job.ChatID, job.Event.ID))
	switch status {
	case StatusRetry:
		q.jobs = append([]DeferredJob{job}, q.jobs...)
	case StatusSent, StatusUnknown:
		// Паритет с db-режимом: строка перестаёт быть deferred — в memory
		// аналогично убираем из очереди.
		for i, j := range q.jobs {
			if j.ChatID == job.ChatID && j.Event.ID == job.Event.ID {
				q.jobs = append(q.jobs[:i], q.jobs[i+1:]...)
				break
			}
		}
	}
	return nil
}
