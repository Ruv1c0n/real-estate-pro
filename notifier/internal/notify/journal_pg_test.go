// Тесты реализации Journal на PostgreSQL. Запускаются только при заданном
// NOTIFIER_TEST_DSN: в CI (job notifier) Postgres нет — там все тесты
// скипаются, это норма, а не ошибка.
//
// Локально: docker compose up -d postgres, затем
// NOTIFIER_TEST_DSN='host=127.0.0.1 user=app password=app dbname=realestate' go test ./internal/notify/ -race
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"notifier/internal/model"
	"notifier/internal/store"
)

var (
	testPool *pgxpool.Pool
	hasDB    bool
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("NOTIFIER_TEST_DSN")
	if dsn == "" {
		// БД не сконфигурирована — тесты ниже скипнутся по hasDB.
		os.Exit(m.Run())
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pg tests: connect: %v\n", err)
		os.Exit(1)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "pg tests: ping: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	applyMigration(ctx, pool)

	testPool = pool
	hasDB = true
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// applyMigration применяет notifier/migrations/0001_init.sql одним Exec
// (внутри файла есть собственные BEGIN/COMMIT). Повторный запуск для тестов
// не фатален: «already exists» означает, что схема уже применена.
func applyMigration(ctx context.Context, pool *pgxpool.Pool) {
	sql, err := os.ReadFile("../../migrations/0001_init.sql")
	if err != nil {
		fmt.Fprintf(os.Stderr, "pg tests: read migration: %v\n", err)
		os.Exit(1)
	}
	if _, err := pool.Exec(ctx, string(sql)); err != nil &&
		!strings.Contains(err.Error(), "already exists") {
		fmt.Fprintf(os.Stderr, "pg tests: apply migration: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
}

// newTestJournal даёт PgJournal на чистой схеме: TRUNCATE notifier.users
// CASCADE каскадом чистит подписки и журнал (ФК ON DELETE CASCADE).
func newTestJournal(t *testing.T) *PgJournal {
	t.Helper()
	if !hasDB {
		t.Skip("NOTIFIER_TEST_DSN не задан — тесты PostgreSQL пропущены (в CI это норма)")
	}
	if _, err := testPool.Exec(context.Background(), "TRUNCATE notifier.users CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return NewPgJournal(testPool)
}

// mustAddUser вставляет пользователя: chat_id в notifications_log ссылается
// на notifier.users (FK), поэтому без него Record вернёт ошибку.
func mustAddUser(t *testing.T, chatID int64) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		"INSERT INTO notifier.users (chat_id) VALUES ($1) ON CONFLICT DO NOTHING", chatID); err != nil {
		t.Fatalf("insert user %d: %v", chatID, err)
	}
}

// logRowCount возвращает число записей журнала по паре (пользователь, объявление).
func logRowCount(t *testing.T, chatID, listingID int64) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		"SELECT count(*) FROM notifier.notifications_log WHERE chat_id = $1 AND listing_id = $2",
		chatID, listingID).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func TestPgJournalRecordTwiceSingleRow(t *testing.T) {
	j := newTestJournal(t)
	mustAddUser(t, 100)

	ctx := context.Background()
	e := testEvent(1)
	if err := j.Record(ctx, 100, e.ID, e.EventID, e, StatusSent, ModeTelegram); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// Повторная запись (перепубликация события, at-least-once) обновляет
	// строку, а не добавляет новую — UNIQUE (chat_id, listing_id).
	if err := j.Record(ctx, 100, e.ID, e.EventID, e, StatusUnknown, ModeTelegram); err != nil {
		t.Fatalf("повторный Record: %v", err)
	}

	if n := logRowCount(t, 100, e.ID); n != 1 {
		t.Fatalf("строк в журнале = %d, want 1", n)
	}
	var status string
	if err := testPool.QueryRow(ctx,
		"SELECT status FROM notifier.notifications_log WHERE chat_id = $1 AND listing_id = $2",
		100, e.ID).Scan(&status); err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != StatusUnknown {
		t.Errorf("status = %q, want %q (последняя запись обновляет статус)", status, StatusUnknown)
	}
}

func TestPgJournalSent(t *testing.T) {
	j := newTestJournal(t)
	mustAddUser(t, 100)

	ctx := context.Background()
	e := testEvent(1)
	if sent, err := j.Sent(ctx, e.ID, 100); err != nil || sent {
		t.Fatalf("Sent до Record = %v, %v; want false, nil", sent, err)
	}

	if err := j.Record(ctx, 100, e.ID, e.EventID, e, StatusSent, ModeLog); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if sent, err := j.Sent(ctx, e.ID, 100); err != nil || !sent {
		t.Fatalf("Sent после Record = %v, %v; want true, nil", sent, err)
	}
}

func TestPgJournalSentIgnoresDeferred(t *testing.T) {
	j := newTestJournal(t)
	mustAddUser(t, 100)

	ctx := context.Background()
	// Строка со статусом deferred (уведомление ещё в очереди, T5) доставкой
	// не считается — Sent должен вернуть false.
	_, err := testPool.Exec(ctx, `
		INSERT INTO notifier.notifications_log (chat_id, listing_id, event_id, status, mode, payload)
		VALUES ($1, $2, $3, 'deferred', $4, '{}')`,
		100, int64(1), testUUID, ModeLog)
	if err != nil {
		t.Fatalf("insert deferred: %v", err)
	}

	if sent, err := j.Sent(ctx, 1, 100); err != nil || sent {
		t.Fatalf("Sent для deferred = %v, %v; want false, nil", sent, err)
	}
}

// eventPayload — зеркало model.Event: journal хранит снимок события
// через encoding/json без тегов (поля — Go-имена структуры), round-trip
// проверяем по полям JSON в колонке payload.
type eventPayload struct {
	EventID   string    `json:"EventID"`
	ID        int64     `json:"ID"`
	Source    string    `json:"Source"`
	DealType  string    `json:"DealType"`
	Country   string    `json:"Country"`
	City      string    `json:"City"`
	Rooms     int       `json:"Rooms"`
	Price     float64   `json:"Price"`
	Currency  string    `json:"Currency"`
	PriceUSD  int64     `json:"PriceUSD"`
	Address   *string   `json:"Address"`
	URL       string    `json:"URL"`
	CreatedAt time.Time `json:"CreatedAt"`
}

func TestPgJournalPayloadRoundTrip(t *testing.T) {
	j := newTestJournal(t)
	mustAddUser(t, 100)

	ctx := context.Background()
	addr := "ул. Притыцкого 10"
	want := model.Event{
		EventID:   testUUID,
		ID:        1,
		Source:    "mock",
		DealType:  model.DealRent,
		Country:   "BY",
		City:      "Minsk",
		Rooms:     1,
		Price:     380.5,
		Currency:  "USD",
		PriceUSD:  380,
		Address:   &addr,
		URL:       "https://example.com/1",
		CreatedAt: time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC),
	}
	if err := j.Record(ctx, 100, want.ID, want.EventID, want, StatusSent, ModeTelegram); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var raw []byte
	if err := testPool.QueryRow(ctx,
		"SELECT payload FROM notifier.notifications_log WHERE chat_id = $1 AND listing_id = $2",
		100, want.ID).Scan(&raw); err != nil {
		t.Fatalf("select payload: %v", err)
	}
	var got eventPayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if got.EventID != want.EventID || got.ID != want.ID || got.Source != want.Source ||
		got.DealType != want.DealType || got.Country != want.Country || got.City != want.City ||
		got.Rooms != want.Rooms || got.Price != want.Price || got.Currency != want.Currency ||
		got.PriceUSD != want.PriceUSD || got.URL != want.URL || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("payload round-trip потерял поля:\n got  %+v\n want %+v", got, want)
	}
	if got.Address == nil || *got.Address != addr {
		t.Errorf("Address = %v, want %q", got.Address, addr)
	}
}

// TestPgDispatcherDoubleDelivery — критерий приёмки T4: двойная доставка
// того же события не даёт дубля ни в отправке, ни в журнале.
func TestPgDispatcherDoubleDelivery(t *testing.T) {
	j := newTestJournal(t)
	mustAddUser(t, 100)

	sender := &recordingSender{}
	subs := store.NewMemory()
	subs.Add(model.Subscription{ChatID: 100})
	d := NewDispatcher(subs, j, NewMemoryLimiter(10, time.Hour), ModeLog, sender.send)

	e := testEvent(1)
	d.Handle(context.Background(), e)
	d.Handle(context.Background(), e) // повторная доставка (XAdd того же события)

	if got := sender.sent(); len(got) != 1 {
		t.Fatalf("sent = %v, want 1", got)
	}
	if n := logRowCount(t, 100, e.ID); n != 1 {
		t.Fatalf("строк в журнале = %d, want 1", n)
	}
}
