package notify

import (
	"context"
	"errors"
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

func newTestDispatcher(limit int) (*Dispatcher, *recordingSender, *store.Memory, *MemoryLimiter, *MemoryQueue) {
	sender := &recordingSender{}
	subs := store.NewMemory()
	journal := NewMemoryJournal()
	limiter := NewMemoryLimiter(limit, time.Hour)
	queue := NewMemoryQueue()
	return NewDispatcher(subs, journal, limiter, queue, ModeLog, sender.send), sender, subs, limiter, queue
}

func TestHandleSendsOncePerChat(t *testing.T) {
	d, sender, subs, _, _ := newTestDispatcher(1000)
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
	d, sender, subs, _, _ := newTestDispatcher(1000)
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
	d, sender, subs, limiter, _ := newTestDispatcher(2)
	limiter.nowFunc = func() time.Time { return base }
	subs.Add(model.Subscription{ChatID: 10})

	d.Handle(context.Background(), testEvent(1))
	d.Handle(context.Background(), testEvent(2))
	d.Handle(context.Background(), testEvent(3)) // сверх лимита — в очередь отложенных

	if got := sender.sent(); len(got) != 2 {
		t.Fatalf("sent = %v, want 2 немедленных", got)
	}

	// Окно освобождается — флешер доставляет отложенное.
	limiter.nowFunc = func() time.Time { return base.Add(61 * time.Minute) }
	d.Flush(context.Background())

	if got := sender.sent(); len(got) != 3 || got[2] != "10:3" {
		t.Fatalf("sent = %v, want отложенное 10:3 после освобождения окна", got)
	}
}

func TestHandleDifferentChatsHaveOwnLimits(t *testing.T) {
	d, sender, subs, _, _ := newTestDispatcher(1)
	subs.Add(model.Subscription{ChatID: 10})
	subs.Add(model.Subscription{ChatID: 20})

	d.Handle(context.Background(), testEvent(1))
	d.Handle(context.Background(), testEvent(2))

	if got := sender.sent(); len(got) != 2 {
		t.Fatalf("sent = %v, want по одному уведомлению каждому чату", got)
	}
}

func TestMemoryLimiterSlidingWindow(t *testing.T) {
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	l := NewMemoryLimiter(2, time.Hour)
	l.nowFunc = func() time.Time { return base }
	ctx := context.Background()

	if ok, err := l.Allow(ctx, 1, ModeLog); err != nil || !ok {
		t.Fatalf("первое уведомление: %v, %v; want true", ok, err)
	}
	if ok, _ := l.Allow(ctx, 1, ModeLog); !ok {
		t.Fatal("второе уведомление должно пройти")
	}
	if ok, _ := l.Allow(ctx, 1, ModeLog); ok {
		t.Fatal("третье за час не должно пройти")
	}

	// Режимы доставки считаются раздельно (contracts.md §2).
	if ok, _ := l.Allow(ctx, 1, ModeTelegram); !ok {
		t.Fatal("окно telegram не должно затрагиваться окном log")
	}

	l.nowFunc = func() time.Time { return base.Add(61 * time.Minute) }
	if ok, _ := l.Allow(ctx, 1, ModeLog); !ok {
		t.Fatal("после истечения окна слот должен освободиться")
	}
}

func TestMemoryJournalTTL(t *testing.T) {
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	j := NewMemoryJournal()
	j.nowFunc = func() time.Time { return base }

	if err := j.Record(context.Background(), 10, 1, testUUID, testEvent(1), StatusSent, ModeLog); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if sent, err := j.Sent(context.Background(), 1, 10); err != nil || !sent {
		t.Fatalf("Sent = %v, %v; want true, nil (запись видна сразу)", sent, err)
	}
	if sent, _ := j.Sent(context.Background(), 2, 10); sent {
		t.Fatal("другое объявление не должно считаться отправленным")
	}

	// In-memory журнал чистится по TTL, чтобы не рос бесконечно; постоянный
	// дедуп даёт только таблица notifications_log (UNIQUE без TTL).
	j.nowFunc = func() time.Time { return base.Add(25 * time.Hour) }
	if sent, _ := j.Sent(context.Background(), 1, 10); sent {
		t.Fatal("после TTL запись удаляется")
	}
}

// mockJournal — Journal для проверки поведения Dispatcher при отказе БД.
type mockJournal struct {
	mu        sync.Mutex
	sentErr   error // ошибка, которую возвращает Sent
	recordErr error // ошибка, которую возвращает Record
	records   map[string]int
}

func newMockJournal() *mockJournal {
	return &mockJournal{records: make(map[string]int)}
}

func (m *mockJournal) Sent(_ context.Context, listingID, chatID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.records[fmt.Sprintf("%d:%d", chatID, listingID)] > 0, m.sentErr
}

func (m *mockJournal) Record(_ context.Context, chatID, listingID int64, _ string, _ model.Event, status, mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recordErr != nil {
		return m.recordErr
	}
	m.records[fmt.Sprintf("%d:%d", chatID, listingID)]++
	m.records[status+"/"+mode]++
	return nil
}

// mockLimiter — Limiter с управляемым ответом и ошибкой.
type mockLimiter struct {
	allowed bool
	err     error
}

func (m *mockLimiter) Allow(context.Context, int64, string) (bool, error) {
	return m.allowed, m.err
}

// mockQueue — DeferredQueue, записывающий вызовы.
type mockQueue struct {
	postponed   []DeferredJob
	marks       []string // "chatID:listingID=status"
	postponeErr error
	popErr      error
	markErr     error
	popResult   []DeferredJob
}

func (m *mockQueue) Postpone(_ context.Context, chatID int64, e model.Event, _ string) error {
	if m.postponeErr != nil {
		return m.postponeErr
	}
	m.postponed = append(m.postponed, DeferredJob{ChatID: chatID, Event: e})
	return nil
}

func (m *mockQueue) PopBatch(context.Context, int) ([]DeferredJob, error) {
	if m.popErr != nil {
		return nil, m.popErr
	}
	return m.popResult, nil
}

func (m *mockQueue) Mark(_ context.Context, job DeferredJob, _, status string) error {
	if m.markErr != nil {
		return m.markErr
	}
	m.marks = append(m.marks, fmt.Sprintf("%d:%d=%s", job.ChatID, job.Event.ID, status))
	return nil
}

func TestHandlePostponesWhenLimitReached(t *testing.T) {
	sender := &recordingSender{}
	subs := store.NewMemory()
	subs.Add(model.Subscription{ChatID: 10})
	queue := &mockQueue{}
	d := NewDispatcher(subs, newMockJournal(), &mockLimiter{allowed: false}, queue, ModeLog, sender.send)

	d.Handle(context.Background(), testEvent(1))

	if got := sender.sent(); len(got) != 0 {
		t.Fatalf("sent = %v, want 0 (лимит исчерпан)", got)
	}
	if len(queue.postponed) != 1 || queue.postponed[0].ChatID != 10 || queue.postponed[0].Event.ID != 1 {
		t.Fatalf("postponed = %+v, want одно задание 10:1", queue.postponed)
	}
}

func TestHandleLimiterErrorPostpones(t *testing.T) {
	// Ошибка лимитера трактуется как «слот занят»: задание откладывается,
	// уведомление не теряется; Dispatcher не паникует.
	sender := &recordingSender{}
	subs := store.NewMemory()
	subs.Add(model.Subscription{ChatID: 10})
	queue := &mockQueue{}
	d := NewDispatcher(subs, newMockJournal(), &mockLimiter{err: errors.New("db down")}, queue, ModeLog, sender.send)

	d.Handle(context.Background(), testEvent(1))

	if got := sender.sent(); len(got) != 0 {
		t.Fatalf("sent = %v, want 0", got)
	}
	if len(queue.postponed) != 1 {
		t.Fatalf("postponed = %+v, want одно задание в очереди", queue.postponed)
	}
}

func TestFlushDeliversAndMarksSent(t *testing.T) {
	e := testEvent(1)
	queue := &mockQueue{popResult: []DeferredJob{{ChatID: 10, Event: e}}}
	journal := newMockJournal()
	sender := &recordingSender{}
	subs := store.NewMemory()
	d := NewDispatcher(subs, journal, &mockLimiter{allowed: true}, queue, ModeTelegram, sender.send)

	d.Flush(context.Background())

	if got := sender.sent(); len(got) != 1 || got[0] != "10:1" {
		t.Fatalf("sent = %v, want 10:1", got)
	}
	if len(queue.marks) != 1 || queue.marks[0] != "10:1=sent" {
		t.Fatalf("marks = %v, want [10:1=sent]", queue.marks)
	}
	if n := journal.records["sent/telegram"]; n != 1 {
		t.Fatalf("записей журнала sent/telegram = %d, want 1", n)
	}
}

func TestFlushRetriesWhenLimitReached(t *testing.T) {
	e := testEvent(1)
	queue := &mockQueue{popResult: []DeferredJob{{ChatID: 10, Event: e}}}
	sender := &recordingSender{}
	subs := store.NewMemory()
	d := NewDispatcher(subs, newMockJournal(), &mockLimiter{allowed: false}, queue, ModeLog, sender.send)

	d.Flush(context.Background())

	if got := sender.sent(); len(got) != 0 {
		t.Fatalf("sent = %v, want 0 (окно всё ещё полно)", got)
	}
	if len(queue.marks) != 1 || queue.marks[0] != "10:1=retry" {
		t.Fatalf("marks = %v, want [10:1=retry]", queue.marks)
	}
}

func TestFlushPopErrorNoPanic(t *testing.T) {
	// Очередь недоступна: логируем и выходим, доставки и пометок нет.
	queue := &mockQueue{popErr: errors.New("db down")}
	sender := &recordingSender{}
	subs := store.NewMemory()
	d := NewDispatcher(subs, newMockJournal(), &mockLimiter{allowed: true}, queue, ModeLog, sender.send)

	d.Flush(context.Background())

	if got := sender.sent(); len(got) != 0 {
		t.Fatalf("sent = %v, want 0", got)
	}
	if len(queue.marks) != 0 {
		t.Fatalf("marks = %v, want пусто", queue.marks)
	}
}

func TestHandleSentErrorStillDelivers(t *testing.T) {
	// Журнал недоступен (Sent падает): Dispatcher не паникует, доставка
	// продолжается — at-least-once допускает повтор (contracts.md §2).
	sender := &recordingSender{}
	subs := store.NewMemory()
	journal := newMockJournal()
	journal.sentErr = errors.New("db down")
	d := NewDispatcher(subs, journal, &mockLimiter{allowed: true}, NewMemoryQueue(), ModeLog, sender.send)
	subs.Add(model.Subscription{ChatID: 10})

	d.Handle(context.Background(), testEvent(1))

	if got := sender.sent(); len(got) != 1 {
		t.Fatalf("sent = %v, want 1 (ошибка журнала не отменяет доставку)", got)
	}
}

func TestHandleRecordErrorNoPanic(t *testing.T) {
	// Record падает (БД недоступна) после успешной отправки: Dispatcher
	// логирует и продолжает, повтор — только при следующей публикации.
	sender := &recordingSender{}
	subs := store.NewMemory()
	journal := newMockJournal()
	journal.recordErr = errors.New("db down")
	d := NewDispatcher(subs, journal, &mockLimiter{allowed: true}, NewMemoryQueue(), ModeLog, sender.send)
	subs.Add(model.Subscription{ChatID: 10})

	d.Handle(context.Background(), testEvent(1))
	d.Flush(context.Background())

	if got := sender.sent(); len(got) != 1 {
		t.Fatalf("sent = %v, want 1 (ошибка Record не должна ломать доставку)", got)
	}
}

func TestHandleRecordsStatusAndMode(t *testing.T) {
	// Успешная отправка записывается со статусом sent, ошибка — unknown;
	// режим доставки берётся из Dispatcher.
	sender := &recordingSender{}
	subs := store.NewMemory()
	journal := newMockJournal()
	d := NewDispatcher(subs, journal, &mockLimiter{allowed: true}, NewMemoryQueue(), ModeTelegram, sender.send)
	subs.Add(model.Subscription{ChatID: 10})

	d.Handle(context.Background(), testEvent(1))

	if n := journal.records["sent/telegram"]; n != 1 {
		t.Fatalf("записей sent/telegram = %d, want 1", n)
	}

	sender.mu.Lock()
	sender.err = errors.New("telegram unavailable")
	sender.mu.Unlock()
	d.Handle(context.Background(), testEvent(2))

	if n := journal.records["unknown/telegram"]; n != 1 {
		t.Fatalf("записей unknown/telegram = %d, want 1", n)
	}
}

// blockingSender — отправитель, «зависающий» в отправке, чтобы смоделировать
// конкурентную доставку того же задания другим путём.
type blockingSender struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   []string
}

func newBlockingSender() *blockingSender {
	return &blockingSender{entered: make(chan struct{}), release: make(chan struct{})}
}

func (s *blockingSender) send(_ context.Context, chatID int64, e model.Event) error {
	s.mu.Lock()
	s.calls = append(s.calls, fmt.Sprintf("%d:%d", chatID, e.ID))
	s.mu.Unlock()
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return nil
}

func (s *blockingSender) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func queueLen(q *MemoryQueue) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.jobs)
}

func TestClaimRelease(t *testing.T) {
	d, _, _, _, _ := newTestDispatcher(10)
	key := journalKey{listingID: 1, chatID: 10}

	if !d.claim(key) {
		t.Fatal("первый клейм должен пройти")
	}
	if d.claim(key) {
		t.Fatal("повторный клейм той же пары должен быть отклонён")
	}
	d.release(key)
	if !d.claim(key) {
		t.Fatal("после release клейм должен пройти снова")
	}
	d.release(key)
}

func TestHandleConcurrentDeliveryNotDuplicated(t *testing.T) {
	// Гонка двух путей (consumer ∥ flusher): оба успевают прочитать
	// journal.Sent == false до записи результата. Клейм «в полёте»
	// должен оставить ровно одну отправку (ревью #46).
	sender := newBlockingSender()
	subs := store.NewMemory()
	d := NewDispatcher(subs, NewMemoryJournal(), NewMemoryLimiter(10, time.Hour), NewMemoryQueue(), ModeLog, sender.send)
	subs.Add(model.Subscription{ChatID: 10})

	e := testEvent(1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.Handle(context.Background(), e)
	}()
	<-sender.entered                  // первый путь завис в отправке, клейм удерживается
	d.Handle(context.Background(), e) // второй путь — пропускает по клейму
	close(sender.release)
	wg.Wait()

	if got := sender.sent(); len(got) != 1 {
		t.Fatalf("sent = %v, want 1 (параллельная доставка не дублируется)", got)
	}
}

func TestFlushRequeuesClaimedJob(t *testing.T) {
	// Задание, занятое параллельной доставкой, возвращается в очередь
	// пометкой retry, а не отбрасывается (ТЗ 3.6: отложенные не теряются).
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	d, sender, subs, limiter, queue := newTestDispatcher(1)
	limiter.nowFunc = func() time.Time { return base }
	subs.Add(model.Subscription{ChatID: 10})

	d.Handle(context.Background(), testEvent(1))
	d.Handle(context.Background(), testEvent(2)) // отложено

	key := journalKey{listingID: 2, chatID: 10}
	if !d.claim(key) {
		t.Fatal("клейм должен пройти до Flush")
	}

	limiter.nowFunc = func() time.Time { return base.Add(61 * time.Minute) }
	d.Flush(context.Background())

	if got := sender.sent(); len(got) != 1 {
		t.Fatalf("sent = %v, want 1 (занятое клеймом задание не отправляется)", got)
	}
	if n := queueLen(queue); n != 1 {
		t.Fatalf("заданий в очереди после Flush = %d, want 1 (задание вернулось по retry)", n)
	}

	d.release(key)
	d.Flush(context.Background())

	if got := sender.sent(); len(got) != 2 || got[1] != "10:2" {
		t.Fatalf("sent = %v, want [10:1 10:2] (после release задание доставлено)", got)
	}
	if n := queueLen(queue); n != 0 {
		t.Fatalf("заданий в очереди после доставки = %d, want 0", n)
	}
}

// serialLimiter — Limiter, фиксирующий параллельные вызовы Allow: если
// второй Allow начался, пока первый не завершился, сериализация доставок
// одного чата нарушена (ревью #49).
type serialLimiter struct {
	mu       sync.Mutex
	active   int
	overflow bool
}

func (m *serialLimiter) Allow(_ context.Context, _ int64, _ string) (bool, error) {
	m.mu.Lock()
	m.active++
	if m.active > 1 {
		m.overflow = true
	}
	m.mu.Unlock()
	time.Sleep(20 * time.Millisecond) // окно для пересечения без блокировки
	m.mu.Lock()
	m.active--
	m.mu.Unlock()
	return true, nil
}

func TestChatLockSerializesAllow(t *testing.T) {
	// Сериализация доставок одного чата: параллельные Handle разных событий
	// одного чата не должны одновременно быть внутри Allow → send → Record,
	// иначе лимитер дважды увидит один свободный слот.
	sender := &recordingSender{}
	subs := store.NewMemory()
	lim := &serialLimiter{}
	d := NewDispatcher(subs, NewMemoryJournal(), lim, NewMemoryQueue(), ModeLog, sender.send)
	subs.Add(model.Subscription{ChatID: 10})

	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, e := range []model.Event{testEvent(1), testEvent(2)} {
		wg.Add(1)
		go func(e model.Event) {
			defer wg.Done()
			<-start
			d.Handle(context.Background(), e)
		}(e)
	}
	close(start)
	wg.Wait()

	if lim.overflow {
		t.Fatal("Allow вызывался параллельно для одного чата — чат-лок не сериализует")
	}
	if got := sender.sent(); len(got) != 2 {
		t.Fatalf("sent = %v, want 2 (оба события доставлены)", got)
	}
}
