package notify

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"notifier/internal/model"
	"notifier/internal/store"
)

const testUUID = "550e8400-e29b-41d4-a716-446655440000"

func testEvent(id int64) model.Event {
	return model.Event{
		EventID:   fmt.Sprintf("550e8400-e29b-41d4-a716-44665544%04x", id),
		ID:        id,
		Source:    "mock",
		DealType:  model.DealRent,
		Country:   "BY",
		City:      "Minsk",
		Rooms:     1,
		Price:     380,
		Currency:  "USD",
		PriceUSD:  380,
		URL:       fmt.Sprintf("https://example.com/%d", id),
		CreatedAt: time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC),
	}
}

// recordingSender — отправитель, который записывает вызовы (потокобезопасно).
type recordingSender struct {
	mu    sync.Mutex
	calls []string // "chatID:listingID"
	err   error
}

func (s *recordingSender) send(_ context.Context, chatID int64, e model.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, fmt.Sprintf("%d:%d", chatID, e.ID))
	return s.err
}

func (s *recordingSender) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func newTestDispatcher(limit int) (*Dispatcher, *recordingSender, *store.Memory, *Limiter) {
	sender := &recordingSender{}
	subs := store.NewMemory()
	journal := NewJournal(time.Hour)
	limiter := NewLimiter(limit, time.Hour)
	return NewDispatcher(subs, journal, limiter, sender.send), sender, subs, limiter
}

func TestHandleSendsOncePerChat(t *testing.T) {
	d, sender, subs, _ := newTestDispatcher(1000)
	subs.Add(model.Subscription{ChatID: 10, City: "Minsk"})
	subs.Add(model.Subscription{ChatID: 10, Rooms: model.IntPtr(1)}) // тот же чат
	subs.Add(model.Subscription{ChatID: 20, City: "Brest"})          // не совпал

	d.Handle(context.Background(), testEvent(1))

	got := sender.sent()
	if len(got) != 1 || got[0] != "10:1" {
		t.Fatalf("sent = %v, want single delivery [10:1]", got)
	}
}

func TestHandleRedeliveryDeduplicated(t *testing.T) {
	d, sender, subs, _ := newTestDispatcher(1000)
	subs.Add(model.Subscription{ChatID: 10})

	e := testEvent(1)
	d.Handle(context.Background(), e)
	d.Handle(context.Background(), e) // повторная доставка того же события

	if got := sender.sent(); len(got) != 1 {
		t.Fatalf("sent = %v, want 1 (повторная доставка не дублируется)", got)
	}
}

func TestHandleDeferWhenLimitReached(t *testing.T) {
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	d, sender, subs, limiter := newTestDispatcher(2)
	limiter.nowFunc = func() time.Time { return base }
	subs.Add(model.Subscription{ChatID: 10})

	d.Handle(context.Background(), testEvent(1))
	d.Handle(context.Background(), testEvent(2))
	d.Handle(context.Background(), testEvent(3)) // сверх лимита — в отложенные

	if got := sender.sent(); len(got) != 2 {
		t.Fatalf("sent = %v, want 2 немедленных", got)
	}

	// Окно освобождается — отложенное уходит.
	limiter.nowFunc = func() time.Time { return base.Add(61 * time.Minute) }
	d.Flush(context.Background())

	if got := sender.sent(); len(got) != 3 || got[2] != "10:3" {
		t.Fatalf("sent = %v, want отложенное 10:3 после освобождения окна", got)
	}
}

func TestHandleDifferentChatsHaveOwnLimits(t *testing.T) {
	d, sender, subs, _ := newTestDispatcher(1)
	subs.Add(model.Subscription{ChatID: 10})
	subs.Add(model.Subscription{ChatID: 20})

	d.Handle(context.Background(), testEvent(1))
	d.Handle(context.Background(), testEvent(2))

	if got := sender.sent(); len(got) != 2 {
		t.Fatalf("sent = %v, want по одному уведомлению каждому чату", got)
	}
}

func TestLimiterSlidingWindow(t *testing.T) {
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	l := NewLimiter(2, time.Hour)
	l.nowFunc = func() time.Time { return base }

	if !l.Allow(1) || !l.Allow(1) {
		t.Fatal("первые два уведомления должны пройти")
	}
	if l.Allow(1) {
		t.Fatal("третье за час не должно пройти")
	}

	l.nowFunc = func() time.Time { return base.Add(61 * time.Minute) }
	if !l.Allow(1) {
		t.Fatal("после истечения окна слот должен освободиться")
	}
	if !l.Allow(1) {
		t.Fatal("в новом окне второй слот ещё свободен")
	}
	if l.Allow(1) {
		t.Fatal("в новом окне снова максимум два")
	}
}

func TestJournalTTL(t *testing.T) {
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	j := NewJournal(time.Hour)
	j.nowFunc = func() time.Time { return base }

	j.Record(1, 10)
	if !j.Sent(1, 10) {
		t.Fatal("запись должна быть видна сразу")
	}
	if j.Sent(2, 10) {
		t.Fatal("другое объявление не должно считаться отправленным")
	}

	j.nowFunc = func() time.Time { return base.Add(2 * time.Hour) }
	if j.Sent(1, 10) {
		t.Fatal("после TTL запись удаляется")
	}
}

func TestFlushDropsAlreadySent(t *testing.T) {
	d, sender, subs, _ := newTestDispatcher(1)
	subs.Add(model.Subscription{ChatID: 10})

	d.Handle(context.Background(), testEvent(1))
	d.Handle(context.Background(), testEvent(2)) // отложено

	// Событие 2 успело уйти другим путём — в журнале.
	d.journal.Record(2, 10)
	d.Flush(context.Background())

	for _, c := range sender.sent() {
		if c == "10:2" {
			t.Fatal("отложенное задание, уже отправленное другим путём, должно быть снято")
		}
	}
}
