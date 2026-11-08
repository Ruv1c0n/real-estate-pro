// Тесты PgLimiter на PostgreSQL (см. journal_pg_test.go про NOTIFIER_TEST_DSN).
package notify

import (
	"context"
	"testing"
)

// mustRecordSent фиксирует доставку через PgJournal — окно лимитера считается
// по фактически отправленным, поэтому заполняем его настоящими Record.
func mustRecordSent(t *testing.T, j *PgJournal, chatID, listingID int64, mode string) {
	t.Helper()
	e := testEvent(listingID)
	if err := j.Record(context.Background(), chatID, listingID, e.EventID, e, StatusSent, mode); err != nil {
		t.Fatalf("Record %d/%d: %v", chatID, listingID, err)
	}
}

func TestPgLimiterCountsRecentSent(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)
	j := NewPgJournal(testPool)
	l := NewPgLimiter(testPool, 2)
	ctx := context.Background()

	if ok, err := l.Allow(ctx, 100, ModeLog); err != nil || !ok {
		t.Fatalf("пустое окно: %v, %v; want true", ok, err)
	}
	mustRecordSent(t, j, 100, 1, ModeLog)
	mustRecordSent(t, j, 100, 2, ModeLog)
	if ok, err := l.Allow(ctx, 100, ModeLog); err != nil || ok {
		t.Fatalf("после двух записей: %v, %v; want false", ok, err)
	}

	// Скользящее окно: запись двухчасовой давности слот освобождает.
	if _, err := testPool.Exec(ctx,
		"UPDATE notifier.notifications_log SET sent_at = now() - interval '2 hours' WHERE listing_id = 1"); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if ok, err := l.Allow(ctx, 100, ModeLog); err != nil || !ok {
		t.Fatalf("после выхода записи из окна: %v, %v; want true", ok, err)
	}
}

func TestPgLimiterIgnoresDeferred(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)
	j := NewPgJournal(testPool)
	l := NewPgLimiter(testPool, 2)
	ctx := context.Background()

	mustRecordSent(t, j, 100, 1, ModeLog)
	// Отложенное (и не отправленное) слот не занимает: sent_at IS NULL,
	// второе уведомление в окно помещается.
	q := NewPgQueue(testPool)
	if err := q.Postpone(ctx, 100, testEvent(2), ModeLog); err != nil {
		t.Fatalf("Postpone: %v", err)
	}
	if ok, err := l.Allow(ctx, 100, ModeLog); err != nil || !ok {
		t.Fatalf("deferred не должен занимать слот: %v, %v; want true", ok, err)
	}
}

func TestPgLimiterModeSeparate(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)
	j := NewPgJournal(testPool)
	l := NewPgLimiter(testPool, 1)

	mustRecordSent(t, j, 100, 1, ModeTelegram)
	if ok, _ := l.Allow(context.Background(), 100, ModeTelegram); ok {
		t.Fatal("окно telegram исчерпано")
	}
	if ok, err := l.Allow(context.Background(), 100, ModeLog); err != nil || !ok {
		t.Fatalf("log учитывается отдельно от telegram (contracts.md §2): %v, %v", ok, err)
	}
}

func TestPgLimiterDoesNotWrite(t *testing.T) {
	// Allow ничего не пишет: окно считается по Record из deliver.
	resetTestDB(t)
	mustAddUser(t, 100)
	l := NewPgLimiter(testPool, 10)
	ctx := context.Background()

	var before, after int
	if err := testPool.QueryRow(ctx,
		"SELECT count(*) FROM notifier.notifications_log WHERE chat_id = 100").Scan(&before); err != nil {
		t.Fatalf("count before: %v", err)
	}
	for i := 0; i < 15; i++ {
		if _, err := l.Allow(ctx, 100, ModeLog); err != nil {
			t.Fatalf("Allow: %v", err)
		}
	}
	if err := testPool.QueryRow(ctx,
		"SELECT count(*) FROM notifier.notifications_log WHERE chat_id = 100").Scan(&after); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if before != 0 || after != 0 {
		t.Fatalf("Allow записал строки: было %d, стало %d", before, after)
	}
}
