// Тесты PgQueue на PostgreSQL (см. journal_pg_test.go про NOTIFIER_TEST_DSN).
package notify

import (
	"context"
	"testing"
	"time"

	"notifier/internal/model"
	"notifier/internal/store"
)

// mustPostpone кладёт событие в очередь, падая при ошибке.
func mustPostpone(t *testing.T, q *PgQueue, chatID, listingID int64) {
	t.Helper()
	if err := q.Postpone(context.Background(), chatID, testEvent(listingID), ModeLog); err != nil {
		t.Fatalf("Postpone %d/%d: %v", chatID, listingID, err)
	}
}

func TestPgQueuePostponeCreatesDeferred(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)
	q := NewPgQueue(testPool)

	mustPostpone(t, q, 100, 1)
	mustPostpone(t, q, 100, 1) // повторная публикация — ON CONFLICT DO NOTHING

	var status string
	if err := testPool.QueryRow(context.Background(),
		"SELECT status FROM notifier.notifications_log WHERE chat_id = 100 AND listing_id = 1").Scan(&status); err != nil {
		t.Fatalf("select: %v", err)
	}
	if status != "deferred" {
		t.Errorf("status = %q, want deferred", status)
	}
	if n := logRowCount(t, 100, 1); n != 1 {
		t.Errorf("строк = %d, want 1 (повторное Postpone не дублирует)", n)
	}
}

func TestPgQueuePopBatchOldestFirst(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)
	q := NewPgQueue(testPool)
	ctx := context.Background()

	for _, id := range []int64{1, 2, 3} {
		mustPostpone(t, q, 100, id)
	}
	// created_at ставится БД; для детерминизма задаём порядок явно.
	if _, err := testPool.Exec(ctx,
		`UPDATE notifier.notifications_log SET created_at = now() - (interval '1 minute' * listing_id)`); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	jobs, err := q.PopBatch(ctx, 2)
	if err != nil {
		t.Fatalf("PopBatch: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("len(jobs) = %d, want 2", len(jobs))
	}
	// Самые старые первыми: 3 (минус 3 минуты), затем 2.
	if jobs[0].Event.ID != 3 || jobs[1].Event.ID != 2 {
		t.Errorf("порядок = [%d %d], want [3 2]", jobs[0].Event.ID, jobs[1].Event.ID)
	}
	if jobs[0].ChatID != 100 {
		t.Errorf("ChatID = %d, want 100", jobs[0].ChatID)
	}
	// payload разбирается через model.ParseEvent — событие без потерь.
	if jobs[0].Event.City != "Minsk" || jobs[0].Event.PriceUSD != 380 {
		t.Errorf("Event из payload неполный: %+v", jobs[0].Event)
	}
	// Транзакция PopBatch не закрыта Mark-ом — откатываем, чтобы TRUNCATE
	// следующего теста не ждал блокировку.
	q.rollbackTx(ctx)
}

func TestPgQueuePopBatchSkipLocked(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)

	for _, id := range []int64{1, 2, 3, 4, 5, 6} {
		mustPostpone(t, NewPgQueue(testPool), 100, id)
	}

	// Два независимых воркера: второй не должен видеть строки, заблокированные
	// первым (FOR UPDATE SKIP LOCKED) — батчи не пересекаются.
	q1 := NewPgQueue(testPool)
	q2 := NewPgQueue(testPool)
	ctx := context.Background()

	j1, err := q1.PopBatch(ctx, 3)
	if err != nil {
		t.Fatalf("PopBatch q1: %v", err)
	}
	j2, err := q2.PopBatch(ctx, 3)
	if err != nil {
		t.Fatalf("PopBatch q2: %v", err)
	}

	if len(j1) != 3 || len(j2) != 3 {
		t.Fatalf("len = %d + %d, want 3 + 3", len(j1), len(j2))
	}
	seen := make(map[int64]bool)
	for _, job := range append(append([]DeferredJob(nil), j1...), j2...) {
		if seen[job.Event.ID] {
			t.Errorf("задание %d попало в оба батча", job.Event.ID)
		}
		seen[job.Event.ID] = true
	}

	// Транзакции воркеров не завершены — откатываем, строки остаются deferred.
	q1.rollbackTx(ctx)
	q2.rollbackTx(ctx)
}

func TestPgQueueMarkRetryKeepsJob(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)
	q := NewPgQueue(testPool)
	ctx := context.Background()

	mustPostpone(t, q, 100, 1)

	jobs, err := q.PopBatch(ctx, 5)
	if err != nil {
		t.Fatalf("PopBatch: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	if err := q.Mark(ctx, jobs[0], ModeLog, StatusRetry); err != nil {
		t.Fatalf("Mark(retry): %v", err)
	}

	// retry — задание остаётся в очереди (статус не менялся).
	jobs, err = q.PopBatch(ctx, 5)
	if err != nil {
		t.Fatalf("повторный PopBatch: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Event.ID != 1 {
		t.Fatalf("после retry PopBatch = %+v, want то же задание", jobs)
	}
	q.rollbackTx(ctx)
}

func TestPgQueueMarkSetsStatusAndSentAt(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)

	cases := []struct {
		status string
		want   string
	}{
		{StatusSent, "sent"},
		{StatusUnknown, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			resetTestDB(t)
			mustAddUser(t, 100)
			q := NewPgQueue(testPool)
			ctx := context.Background()

			mustPostpone(t, q, 100, 1)
			jobs, err := q.PopBatch(ctx, 5)
			if err != nil {
				t.Fatalf("PopBatch: %v", err)
			}
			if err := q.Mark(ctx, jobs[0], ModeLog, tc.status); err != nil {
				t.Fatalf("Mark: %v", err)
			}

			var status string
			var sentAt *time.Time
			if err := testPool.QueryRow(ctx,
				"SELECT status, sent_at FROM notifier.notifications_log WHERE chat_id = 100 AND listing_id = 1").
				Scan(&status, &sentAt); err != nil {
				t.Fatalf("select: %v", err)
			}
			if status != tc.want {
				t.Errorf("status = %q, want %q", status, tc.want)
			}
			if sentAt == nil {
				t.Error("sent_at = NULL, want заполнен")
			}
		})
	}
}

func TestPgQueuePopBatchSkipsDelivered(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)
	q := NewPgQueue(testPool)
	j := NewPgJournal(testPool)
	ctx := context.Background()

	// Задание отложено, но событие уже доставлено другим путём: строка стала
	// sent (Record делает upsert той же пары) — PopBatch её не возвращает.
	mustPostpone(t, q, 100, 1)
	e := testEvent(1)
	if err := j.Record(ctx, 100, 1, e.EventID, e, StatusSent, ModeLog); err != nil {
		t.Fatalf("Record: %v", err)
	}

	jobs, err := q.PopBatch(ctx, 5)
	if err != nil {
		t.Fatalf("PopBatch: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("PopBatch = %+v, want пусто (уже доставлено)", jobs)
	}
	// Транзакция открыта даже при пустом батче — откат.
	q.rollbackTx(ctx)
}

// TestPgThirtyPerHourScenario — критерий приёмки T5: сценарий ТЗ 3.6 целиком
// на Pg-реализациях. 30 доставок проходят, 31-е откладывается, после
// освобождения окна флешер доставляет его со статусом sent.
func TestPgThirtyPerHourScenario(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)

	j := NewPgJournal(testPool)
	l := NewPgLimiter(testPool, 30)
	q := NewPgQueue(testPool)
	sender := &recordingSender{}
	subs := store.NewMemory()
	subs.Add(model.Subscription{ChatID: 100})
	d := NewDispatcher(subs, j, l, q, ModeLog, sender.send)
	ctx := context.Background()

	for id := int64(1); id <= 30; id++ {
		d.Handle(ctx, testEvent(id))
	}
	if got := sender.sent(); len(got) != 30 {
		t.Fatalf("sent = %d, want 30", len(got))
	}

	// 31-е уведомление: окно исчерпано — задание отложено, не отправлено.
	d.Handle(ctx, testEvent(31))
	if got := sender.sent(); len(got) != 30 {
		t.Fatalf("31-е ушло сразу: sent = %d, want 30", len(got))
	}
	var status string
	if err := testPool.QueryRow(ctx,
		"SELECT status FROM notifier.notifications_log WHERE chat_id = 100 AND listing_id = 31").Scan(&status); err != nil {
		t.Fatalf("select deferred row: %v", err)
	}
	if status != "deferred" {
		t.Fatalf("31-е: status = %q, want deferred", status)
	}

	// Час прошёл — окно освободилось, флешер доставляет отложенное.
	if _, err := testPool.Exec(ctx,
		"UPDATE notifier.notifications_log SET sent_at = now() - interval '2 hours' WHERE chat_id = 100 AND listing_id < 31"); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	d.Flush(ctx)

	if got := sender.sent(); len(got) != 31 || got[30] != "100:31" {
		t.Fatalf("sent = %d, последнее %q; want 31, 100:31", len(got), got[len(got)-1])
	}
	if err := testPool.QueryRow(ctx,
		"SELECT status FROM notifier.notifications_log WHERE chat_id = 100 AND listing_id = 31").Scan(&status); err != nil {
		t.Fatalf("select after flush: %v", err)
	}
	if status != "sent" {
		t.Errorf("после флеша status = %q, want sent", status)
	}
}
