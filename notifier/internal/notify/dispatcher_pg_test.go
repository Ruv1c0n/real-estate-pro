// Тесты Dispatcher на PostgreSQL (см. journal_pg_test.go про NOTIFIER_TEST_DSN):
// условный XACK (issue #53, ТЗ 4.3) — при временном сбое БД Handle возвращает
// ошибку, событие остаётся в pending, а после восстановления БД повторная
// обработка доставляет уведомление ровно один раз (дедуп журнала, ФТ-3.5).
package notify

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"notifier/internal/model"
	"notifier/internal/store"
)

// TestPgDispatcherRedeliveredOnceAfterDBFailure — сценарий issue #53:
//  1. нормальная обработка: доставка + запись журнала;
//  2. БД «падает» (закрытый пул): Handle возвращает ошибку — консьюмер
//     не выполняет XACK, событие остаётся в pending;
//  3. «БД восстановилась»: повторная обработка того же события —
//     дедуп журнала не даёт второй доставки, в журнале одна строка.
func TestPgDispatcherRedeliveredOnceAfterDBFailure(t *testing.T) {
	resetTestDB(t)
	mustAddUser(t, 100)

	ctx := context.Background()
	sender := &recordingSender{}
	subs := store.NewMemory()
	subs.Add(ctx, model.Subscription{ChatID: 100})
	d := NewDispatcher(subs, NewPgJournal(testPool), NewPgLimiter(testPool, 10), NewPgQueue(testPool), ModeLog, sender.send)

	e := testEvent(1)
	if err := d.Handle(ctx, e); err != nil {
		t.Fatalf("Handle при доступной БД: %v", err)
	}
	if got := sender.sent(); len(got) != 1 || got[0] != "100:1" {
		t.Fatalf("sent = %v, want [100:1]", got)
	}

	// БД «падает»: Dispatcher на закрытом пуле — все операции с БД возвращают
	// ошибку (закрытие пула имитирует временный сбой соединения).
	downPool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("create broken pool: %v", err)
	}
	downPool.Close()
	dDown := NewDispatcher(subs, NewPgJournal(downPool), NewPgLimiter(downPool, 10), NewPgQueue(downPool), ModeLog, sender.send)
	if err := dDown.Handle(ctx, e); err == nil {
		t.Fatal("Handle при недоступной БД должен вернуть ошибку (событие остаётся в pending, без XACK)")
	}

	// «БД восстановилась»: повторная обработка (XAUTOCLAIM вернул pending-запись)
	// не доставляет второй раз — Sent находит запись журнала (ФТ-3.5).
	if err := d.Handle(ctx, e); err != nil {
		t.Fatalf("повторный Handle после восстановления БД: %v", err)
	}
	if got := sender.sent(); len(got) != 1 {
		t.Fatalf("sent после повтора = %v, want 1 (повторная доставка не дублируется)", got)
	}
	if n := logRowCount(t, 100, e.ID); n != 1 {
		t.Fatalf("строк в журнале = %d, want 1", n)
	}
}
