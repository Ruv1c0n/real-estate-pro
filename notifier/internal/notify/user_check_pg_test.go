// Тесты проверки существования пользователя перед отправкой (ТЗ 4.2) на
// PostgreSQL. Запускаются только при заданном NOTIFIER_TEST_DSN (см.
// journal_pg_test.go): в CI Postgres нет — тест скипается, это норма.
package notify

import (
	"context"
	"testing"

	"notifier/internal/model"
	"notifier/internal/store"
)

// TestPgHandleDeliversForExistingUser — сквозной путь Handle с настоящим
// Pg-хранилищем: пользователь есть (создан Add'ом подписки) — проверка
// UserExists проходит, доставка выполняется.
func TestPgHandleDeliversForExistingUser(t *testing.T) {
	resetTestDB(t)
	ctx := context.Background()

	sender := &recordingSender{}
	subs := store.NewPostgres(testPool)
	if _, err := subs.Add(ctx, model.Subscription{ChatID: 100, City: "Minsk"}); err != nil {
		t.Fatalf("Add subscription: %v", err)
	}
	d := NewDispatcher(subs, NewPgJournal(testPool), NewPgLimiter(testPool, 10), NewPgQueue(testPool), ModeLog, sender.send)

	if err := d.Handle(ctx, testEvent(1)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := sender.sent(); len(got) != 1 || got[0] != "100:1" {
		t.Fatalf("sent = %v, want [100:1]", got)
	}
}

// TestPgFlushDropsDeferredJobOfDeletedUser — ТЗ 4.2 на flush-пути: задание
// лежало в очереди, пользователь удалён. Строку пользователя реально удалить
// нельзя (FK chat_id → users на deferred-строке; каскад /stop снял бы и её),
// поэтому окно гонки «пользователь удалён после Postpone, строка ещё жива»
// имитируется обёрткой userGateStore с exists=false — Mark(drop) обязан
// удалить deferred-строку из notifications_log (паритет с каскадом /stop).
func TestPgFlushDropsDeferredJobOfDeletedUser(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)
	ctx := context.Background()

	e := testEvent(1)
	queue := NewPgQueue(testPool)
	if err := queue.Postpone(ctx, 100, e, ModeLog); err != nil {
		t.Fatalf("Postpone: %v", err)
	}

	sender := &recordingSender{}
	subs := store.NewPostgres(testPool)
	d := NewDispatcher(&userGateStore{Store: subs, exists: false}, NewPgJournal(testPool),
		NewPgLimiter(testPool, 10), queue, ModeLog, sender.send)

	d.Flush(ctx)

	if got := sender.sent(); len(got) != 0 {
		t.Fatalf("sent = %v, want 0 (пользователь удалён)", got)
	}
	if n := logRowCount(t, 100, e.ID); n != 0 {
		t.Fatalf("deferred-строка после drop осталась в журнале: %d, want 0", n)
	}
}

// TestPgFlushDeliversDeferredJobForExistingUser — обратный случай: задание
// в очереди, пользователь на месте — проверка UserExists проходит, доставка
// выполняется, строка журнала закрывается статусом sent.
func TestPgFlushDeliversDeferredJobForExistingUser(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)
	ctx := context.Background()

	e := testEvent(1)
	queue := NewPgQueue(testPool)
	if err := queue.Postpone(ctx, 100, e, ModeLog); err != nil {
		t.Fatalf("Postpone: %v", err)
	}

	sender := &recordingSender{}
	subs := store.NewPostgres(testPool)
	d := NewDispatcher(subs, NewPgJournal(testPool), NewPgLimiter(testPool, 10), queue, ModeLog, sender.send)

	d.Flush(ctx)

	if got := sender.sent(); len(got) != 1 || got[0] != "100:1" {
		t.Fatalf("sent = %v, want [100:1]", got)
	}
	var status string
	if err := testPool.QueryRow(ctx,
		"SELECT status FROM notifier.notifications_log WHERE chat_id = 100 AND listing_id = $1", e.ID).
		Scan(&status); err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != StatusSent {
		t.Fatalf("status = %q, want %q", status, StatusSent)
	}
}
