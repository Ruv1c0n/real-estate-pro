// Тесты реализации Store на PostgreSQL. Запускаются только при заданном
// NOTIFIER_TEST_DSN: в CI (job notifier) Postgres нет — там все тесты
// скипаются, это норма, а не ошибка.
//
// Локально: docker compose up -d postgres, затем
// NOTIFIER_TEST_DSN='host=127.0.0.1 user=app password=app dbname=realestate' go test ./internal/store/ -race
package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"notifier/internal/model"
)

var (
	testPool *pgxpool.Pool
	testDSN  string // сохраняется для тестов, открывающих собственный пул
	hasDB    bool
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("NOTIFIER_TEST_DSN")
	if dsn == "" {
		// БД не сконфигурирована — тесты ниже скипнутся по hasDB.
		os.Exit(m.Run())
	}
	testDSN = dsn

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

// newTestStore даёт Postgres-хранилище на чистой схеме: TRUNCATE
// notifier.users CASCADE каскадом чистит подписки и журнал (ФК ON DELETE CASCADE).
func newTestStore(t *testing.T) *Postgres {
	t.Helper()
	if !hasDB {
		t.Skip("NOTIFIER_TEST_DSN не задан — тесты PostgreSQL пропущены (в CI это норма)")
	}
	if _, err := testPool.Exec(context.Background(), "TRUNCATE notifier.users CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return NewPostgres(testPool)
}

// mustAddUser вставляет пользователя: chat_id в subscriptions ссылается на
// notifier.users (FK), поэтому без него Add вернёт ошибку.
func mustAddUser(t *testing.T, chatID int64) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		"INSERT INTO notifier.users (chat_id) VALUES ($1) ON CONFLICT DO NOTHING", chatID); err != nil {
		t.Fatalf("insert user %d: %v", chatID, err)
	}
}

// mustAdd вставляет подписку через публичный Add. Подписки в тестах валидны,
// ошибка означает сбой БД или кода — фаталим с явной проверкой.
func mustAdd(t *testing.T, p *Postgres, sub model.Subscription) model.Subscription {
	t.Helper()
	got, err := p.Add(context.Background(), sub)
	if err != nil {
		t.Fatalf("Add(%+v): %v", sub, err)
	}
	return got
}

// mustByChat возвращает подписки чата; ошибка выборки — фатал.
func mustByChat(t *testing.T, p *Postgres, chatID int64) []model.Subscription {
	t.Helper()
	subs, err := p.ByChat(context.Background(), chatID)
	if err != nil {
		t.Fatalf("ByChat(%d): %v", chatID, err)
	}
	return subs
}

// mustCount возвращает число подписок чата; ошибка выборки — фатал.
func mustCount(t *testing.T, p *Postgres, chatID int64) int {
	t.Helper()
	n, err := p.CountForChat(context.Background(), chatID)
	if err != nil {
		t.Fatalf("CountForChat(%d): %v", chatID, err)
	}
	return n
}

// assertSameSub сравнивает подписки по всем полям кроме ID (ID присваивает БД).
func assertSameSub(t *testing.T, want, got model.Subscription) {
	t.Helper()
	if got.ChatID != want.ChatID {
		t.Errorf("ChatID = %d, want %d", got.ChatID, want.ChatID)
	}
	if got.DealType != want.DealType {
		t.Errorf("DealType = %q, want %q", got.DealType, want.DealType)
	}
	if got.Country != want.Country {
		t.Errorf("Country = %q, want %q", got.Country, want.Country)
	}
	if got.City != want.City {
		t.Errorf("City = %q, want %q", got.City, want.City)
	}
	assertSameInt(t, "Rooms", want.Rooms, got.Rooms)
	assertSameInt64(t, "MinPriceUSD", want.MinPriceUSD, got.MinPriceUSD)
	assertSameInt64(t, "MaxPriceUSD", want.MaxPriceUSD, got.MaxPriceUSD)
}

func assertSameInt(t *testing.T, name string, want, got *int) {
	t.Helper()
	if (want == nil) != (got == nil) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	if want != nil && *got != *want {
		t.Errorf("%s = %d, want %d", name, *got, *want)
	}
}

func assertSameInt64(t *testing.T, name string, want, got *int64) {
	t.Helper()
	if (want == nil) != (got == nil) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	if want != nil && *got != *want {
		t.Errorf("%s = %d, want %d", name, *got, *want)
	}
}

func TestPostgresAddReturnsDBID(t *testing.T) {
	p := newTestStore(t)
	mustAddUser(t, 100)

	got := mustAdd(t, p, model.Subscription{ChatID: 100, City: "Minsk"})
	if got.ID <= 0 {
		t.Fatalf("Add() вернул ID %d, положительного из БД", got.ID)
	}
	if n := mustCount(t, p, 100); n != 1 {
		t.Errorf("CountForChat(100) = %d, want 1", n)
	}
}

func TestPostgresByChat(t *testing.T) {
	p := newTestStore(t)
	mustAddUser(t, 100)
	mustAddUser(t, 200)

	mustAdd(t, p, model.Subscription{ChatID: 100, City: "Minsk"})
	mustAdd(t, p, model.Subscription{ChatID: 200, City: "Brest"})
	mustAdd(t, p, model.Subscription{ChatID: 100, DealType: model.DealRent})
	mustAdd(t, p, model.Subscription{ChatID: 100, Country: "PL"})

	subs := mustByChat(t, p, 100)
	if len(subs) != 3 {
		t.Fatalf("len(ByChat(100)) = %d, want 3", len(subs))
	}
	for i := 1; i < len(subs); i++ {
		if subs[i].ID <= subs[i-1].ID {
			t.Errorf("ByChat не отсортирован по id: %d после %d", subs[i].ID, subs[i-1].ID)
		}
	}
	if subs[0].City != "Minsk" || subs[1].DealType != model.DealRent || subs[2].Country != "PL" {
		t.Errorf("неожиданное содержимое ByChat(100): %+v", subs)
	}
	if got := mustByChat(t, p, 200); len(got) != 1 || got[0].City != "Brest" {
		t.Errorf("ByChat(200) = %+v, want одна подписка Brest", got)
	}
	if got := mustByChat(t, p, 999); len(got) != 0 {
		t.Errorf("ByChat(999) = %+v, want пусто", got)
	}
}

func TestPostgresDelete(t *testing.T) {
	p := newTestStore(t)
	mustAddUser(t, 100)
	mustAddUser(t, 200)

	a := mustAdd(t, p, model.Subscription{ChatID: 100, City: "Minsk"})
	b := mustAdd(t, p, model.Subscription{ChatID: 200, City: "Brest"})

	if ok, err := p.Delete(context.Background(), 100, a.ID); err != nil || !ok {
		t.Errorf("Delete(100, a.ID) = %v, %v; want true, nil", ok, err)
	}
	if ok, err := p.Delete(context.Background(), 100, a.ID); err != nil || ok {
		t.Errorf("повторный Delete(100, a.ID) = %v, %v; want false, nil", ok, err)
	}
	if ok, err := p.Delete(context.Background(), 100, b.ID); err != nil || ok {
		t.Errorf("Delete(100, b.ID) = %v, %v — чужая подписка не должна удаляться", ok, err)
	}
	if n := mustCount(t, p, 200); n != 1 {
		t.Errorf("CountForChat(200) = %d, want 1 (подписка b должна остаться)", n)
	}
}

func TestPostgresDeleteAllForChat(t *testing.T) {
	p := newTestStore(t)
	mustAddUser(t, 100)
	mustAddUser(t, 200)

	for _, city := range []string{"Minsk", "Brest", "Grodno"} {
		mustAdd(t, p, model.Subscription{ChatID: 100, City: city})
	}
	mustAdd(t, p, model.Subscription{ChatID: 200, City: "Vitebsk"})

	if n, err := p.DeleteAllForChat(context.Background(), 100); err != nil || n != 3 {
		t.Errorf("DeleteAllForChat(100) = %d, %v; want 3, nil", n, err)
	}
	if n := mustCount(t, p, 100); n != 0 {
		t.Errorf("CountForChat(100) = %d, want 0", n)
	}
	if n := mustCount(t, p, 200); n != 1 {
		t.Errorf("CountForChat(200) = %d, want 1", n)
	}
}

func TestPostgresCountForChat(t *testing.T) {
	p := newTestStore(t)
	mustAddUser(t, 100)

	if n := mustCount(t, p, 100); n != 0 {
		t.Fatalf("CountForChat(100) = %d, want 0", n)
	}
	mustAdd(t, p, model.Subscription{ChatID: 100, City: "Minsk"})
	mustAdd(t, p, model.Subscription{ChatID: 100, City: "Brest"})
	if n := mustCount(t, p, 100); n != 2 {
		t.Errorf("CountForChat(100) = %d, want 2", n)
	}
}

func TestPostgresAll(t *testing.T) {
	p := newTestStore(t)
	mustAddUser(t, 100)
	mustAddUser(t, 200)

	a := mustAdd(t, p, model.Subscription{ChatID: 100, City: "Minsk"})
	b := mustAdd(t, p, model.Subscription{ChatID: 200, City: "Brest"})

	all, err := p.All(context.Background())
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(All()) = %d, want 2", len(all))
	}
	if all[0].ID != a.ID || all[1].ID != b.ID {
		t.Errorf("All() не отсортирован по id: [%d %d], want [%d %d]", all[0].ID, all[1].ID, a.ID, b.ID)
	}
}

func TestPostgresClosedPoolReturnsError(t *testing.T) {
	// Хранилище недоступно: публичный метод обязан вернуть ошибку, а не
	// нулевое значение — вызывающий обязан различать «нет данных» и
	// «хранилище недоступно» (H2).
	if !hasDB {
		t.Skip("NOTIFIER_TEST_DSN не задан — тесты PostgreSQL пропущены (в CI это норма)")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	p := NewPostgres(pool)
	pool.Close()

	subs, err := p.All(ctx)
	if err == nil {
		t.Fatal("All на закрытом пуле: err = nil, want ошибка вместо пустого списка")
	}
	if len(subs) != 0 {
		t.Errorf("All на закрытом пуле вернул %d подписок, want 0", len(subs))
	}
	if !strings.Contains(err.Error(), "store:") {
		t.Errorf("ошибка All без префикса источника: %q", err.Error())
	}
	if n, err := p.CountForChat(ctx, 1); err == nil {
		t.Errorf("CountForChat на закрытом пуле: err = nil (n = %d), want ошибка", n)
	}
}

func TestPostgresRoundTripFull(t *testing.T) {
	p := newTestStore(t)
	mustAddUser(t, 100)

	// Все поля заданы: пустых значений нет, NULL в БД появляться не должно.
	want := model.Subscription{
		ChatID:      100,
		DealType:    model.DealRent,
		Country:     "BY",
		City:        "Minsk",
		Rooms:       model.IntPtr(1),
		MinPriceUSD: model.Int64Ptr(100),
		MaxPriceUSD: model.Int64Ptr(500),
	}
	got := mustAdd(t, p, want)
	if got.ID <= 0 {
		t.Fatalf("Add() вернул ID %d", got.ID)
	}

	subs := mustByChat(t, p, 100)
	if len(subs) != 1 {
		t.Fatalf("len(ByChat(100)) = %d, want 1", len(subs))
	}
	assertSameSub(t, want, subs[0])
}

func TestPostgresRoundTripNulls(t *testing.T) {
	p := newTestStore(t)
	mustAddUser(t, 100)

	// Пустая строка и nil-указатель в модели — NULL в БД («любое значение»).
	// Полностью пустую подписку БД не примет (subscriptions_not_empty_chk),
	// поэтому «пустые» поля проверяем на подписке с одним заданным полем.
	want := model.Subscription{ChatID: 100, City: "Minsk"}
	got := mustAdd(t, p, want)
	if got.ID <= 0 {
		t.Fatalf("Add() вернул ID %d", got.ID)
	}

	subs := mustByChat(t, p, 100)
	if len(subs) != 1 {
		t.Fatalf("len(ByChat(100)) = %d, want 1", len(subs))
	}
	assertSameSub(t, want, subs[0])
}

func TestPostgresConstraintViolationReturnsError(t *testing.T) {
	p := newTestStore(t)
	mustAddUser(t, 100)

	cases := []struct {
		name string
		sub  model.Subscription
	}{
		{"rooms отрицательное (rooms_chk)", model.Subscription{ChatID: 100, City: "Minsk", Rooms: model.IntPtr(-1)}},
		{"min больше max (price_range_chk)", model.Subscription{ChatID: 100, MinPriceUSD: model.Int64Ptr(500), MaxPriceUSD: model.Int64Ptr(100)}},
		{"country не alpha-2 (country_chk)", model.Subscription{ChatID: 100, Country: "blr"}},
		{"все поля пустые (not_empty_chk)", model.Subscription{ChatID: 100}},
		{"нет пользователя (FK)", model.Subscription{ChatID: 777, City: "Minsk"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Публичный Add возвращает ошибку констрейнта, а не подписку
			// с нулевым ID: вызывающий обязан видеть сбой вставки.
			got, err := p.Add(context.Background(), tc.sub)
			if err == nil {
				t.Error("ожидали ошибку констрейнта, получили nil")
			}
			if got.ID != 0 {
				t.Errorf("Add при ошибке вернул ID %d, want 0", got.ID)
			}
		})
	}
}

func TestPostgresEnsureUserIdempotent(t *testing.T) {
	p := newTestStore(t)

	if err := p.EnsureUser(context.Background(), 300); err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if err := p.EnsureUser(context.Background(), 300); err != nil { // повторный — no-op
		t.Fatalf("повторный EnsureUser: %v", err)
	}

	var n int
	if err := testPool.QueryRow(context.Background(),
		"SELECT count(*) FROM notifier.users WHERE chat_id = 300").Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Errorf("строк users для chat 300: %d, want 1", n)
	}
}

func TestPostgresDeleteUserCascade(t *testing.T) {
	p := newTestStore(t)
	if err := p.EnsureUser(context.Background(), 100); err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	mustAdd(t, p, model.Subscription{ChatID: 100, City: "Minsk"})
	mustAdd(t, p, model.Subscription{ChatID: 100, City: "Brest"})
	// Строка журнала: каскад по FK должен убрать и её (/stop, ФТ-3.1).
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO notifier.notifications_log (chat_id, listing_id, event_id, status, mode, payload)
		VALUES (100, 1, gen_random_uuid(), 'deferred', 'log', '{}')`); err != nil {
		t.Fatalf("insert log row: %v", err)
	}

	n, err := p.DeleteUser(context.Background(), 100)
	if err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if n != 2 {
		t.Errorf("DeleteUser вернул %d подписок, want 2", n)
	}
	assertTablesEmpty(t, "notifier.users", "notifier.subscriptions", "notifier.notifications_log")
}

func TestPostgresAddCreatesUser(t *testing.T) {
	p := newTestStore(t)
	// Пользователя для чата 300 нет: Add должен создать его сам —
	// /subscribe без предварительного /start работает.
	got := mustAdd(t, p, model.Subscription{ChatID: 300, City: "Minsk"})
	if got.ID <= 0 {
		t.Fatalf("Add() без /start вернул ID %d", got.ID)
	}

	var n int
	if err := testPool.QueryRow(context.Background(),
		"SELECT count(*) FROM notifier.users WHERE chat_id = 300").Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Errorf("строк users для chat 300: %d, want 1", n)
	}
}

// assertTablesEmpty проверяет, что во всех перечисленных таблицах пусто
// (после /stop не должно оставаться данных ни в одной из трёх).
func assertTablesEmpty(t *testing.T, tables ...string) {
	t.Helper()
	for _, table := range tables {
		var n int
		if err := testPool.QueryRow(context.Background(),
			"SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("в %s осталось строк: %d, want 0", table, n)
		}
	}
}
