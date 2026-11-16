// Тесты безопасной очистки стрима (ТЗ 4.3): удаляются только подтверждённые
// записи за безопасной границей; pending и непрочитанные не трогаются.
package stream

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// compareStreamIDs сравнивает два ID потока как пары (мс, seq): лексикографика
// строк ломается на разной длине миллисекундной части ("999-1" > "1000-0").
func compareStreamIDs(a, b string) int {
	parse := func(id string) (int64, int64) {
		ms, seq, _ := strings.Cut(id, "-")
		m, _ := strconv.ParseInt(ms, 10, 64)
		s, _ := strconv.ParseInt(seq, 10, 64)
		return m, s
	}
	am, as := parse(a)
	bm, bs := parse(b)
	if am != bm {
		if am < bm {
			return -1
		}
		return 1
	}
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	}
	return 0
}

// remainingIDs возвращает текущее содержимое стрима двойника.
func (f *fakeRedis) remainingIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.streamIDs...)
}

func (f *fakeRedis) deletedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

// TestReaperDeletesOnlyConfirmedBeforeMinPending — записи строго раньше
// минимального pending ID доставлены и подтверждены (их нет в PEL), их
// можно удалять; сама pending-запись и хвост стрима остаются.
func TestReaperDeletesOnlyConfirmedBeforeMinPending(t *testing.T) {
	rdb := &fakeRedis{
		streamIDs:     []string{"10-1", "11-2", "12-1", "13-5"},
		pendingIDs:    []string{"12-1"}, // 12-1 «в работе», 13-5 — непрочитанный хвост
		lastDelivered: "13-5",
	}
	r := NewReaper(rdb, cleanupInterval)

	n, boundary, err := r.CleanOnce(context.Background())
	if err != nil {
		t.Fatalf("CleanOnce: %v", err)
	}
	if boundary != "12-1" {
		t.Errorf("boundary = %q, want 12-1 (min pending ID)", boundary)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	// 10-1 и 11-2 доставлены и подтверждены (их нет в PEL) — удаляются.
	if got := strings.Join(rdb.deletedIDs(), ","); got != "10-1,11-2" {
		t.Errorf("deleted ids = %q, want 10-1,11-2", got)
	}
	// Pending-запись и непрочитанный хвост остаются в стриме.
	if got := strings.Join(rdb.remainingIDs(), ","); got != "12-1,13-5" {
		t.Errorf("remaining = %q, want 12-1,13-5", got)
	}
	// XDEL не трогает PEL: pending-запись остаётся видимой в XPENDING
	// (сводка не зависит от удаления подтверждённых записей).
	if p, err := rdb.XPending(context.Background(), StreamName, GroupName).Result(); err != nil || p.Count != 1 {
		t.Errorf("pending после XDEL = %+v (%v), want Count=1", p, err)
	}
}

// TestReaperSkipsAllWhenPendingAtHead — pending-запись с минимальным ID
// означает, что подтверждённых записей перед ней нет: удалять нечего.
func TestReaperSkipsAllWhenPendingAtHead(t *testing.T) {
	rdb := &fakeRedis{
		streamIDs:     []string{"10-1", "10-2"},
		pendingIDs:    []string{"10-1", "10-2"},
		lastDelivered: "10-2",
	}
	r := NewReaper(rdb, cleanupInterval)

	n, _, err := r.CleanOnce(context.Background())
	if err != nil {
		t.Fatalf("CleanOnce: %v", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0", n)
	}
	if got := rdb.remainingIDs(); len(got) != 2 {
		t.Errorf("remaining = %v, want обе записи", got)
	}
}

// TestReaperEmptyPendingUsesLastDelivered — pending пуст: всё доставленное
// подтверждено, границей служит last-delivered-id группы; записи после него
// (если бы появились) не трогаются.
func TestReaperEmptyPendingUsesLastDelivered(t *testing.T) {
	rdb := &fakeRedis{
		streamIDs:     []string{"10-1", "11-1"},
		lastDelivered: "11-1",
	}
	r := NewReaper(rdb, cleanupInterval)

	n, boundary, err := r.CleanOnce(context.Background())
	if err != nil {
		t.Fatalf("CleanOnce: %v", err)
	}
	if boundary != "11-1" {
		t.Errorf("boundary = %q, want 11-1", boundary)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
	// Граница exclusive: последняя доставленная запись остаётся (консервативно).
	if got := rdb.remainingIDs(); len(got) != 1 || got[0] != "11-1" {
		t.Errorf("remaining = %v, want [11-1]", got)
	}
}

// TestReaperNothingToCleanWhenGroupFresh — нулевой last-delivered (группа
// создана, но ничего не доставляла) и пустой стрим — проход без удаления.
func TestReaperNothingToCleanWhenGroupFresh(t *testing.T) {
	rdb := &fakeRedis{lastDelivered: "0-0"}
	r := NewReaper(rdb, cleanupInterval)

	n, boundary, err := r.CleanOnce(context.Background())
	if err != nil {
		t.Fatalf("CleanOnce: %v", err)
	}
	if n != 0 || boundary != "" {
		t.Errorf("deleted = %d, boundary = %q, want 0/\"\"", n, boundary)
	}
}
