// Тесты раннера миграций. Pg-тесты запускаются только при заданном
// NOTIFIER_TEST_DSN: в CI (job notifier) Postgres нет — там тесты
// скипаются, это норма, а не ошибка.
//
// Локально: docker compose up -d postgres, затем
// NOTIFIER_TEST_DSN='host=127.0.0.1 user=app password=app dbname=realestate' go test ./internal/migrate/ -race
package migrate

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	testPool *pgxpool.Pool
	hasDB    bool
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("NOTIFIER_TEST_DSN")
	if dsn == "" {
		// БД не сконфигурирована — Pg-тесты ниже скипнутся по hasDB.
		os.Exit(m.Run())
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		// С заданным DSN подключение обязано работать — иначе локально
		// сломан стенд, и это надо чинить, а не молча скипать.
		panic(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		panic(err)
	}

	testPool = pool
	hasDB = true
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// TestVersionOfAndSort — unit-тесты без БД: разбор версий и сортировка имён.
func TestVersionOfAndSort(t *testing.T) {
	cases := map[string]string{
		"0001_init.sql":        "0001",
		"0002_add_index.sql":   "0002",
		"0010_late_change.sql": "0010",
	}
	for name, want := range cases {
		if got := versionOf(name); got != want {
			t.Errorf("versionOf(%q) = %q, want %q", name, got, want)
		}
	}

	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("embedded migrations: no .sql files found")
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("migrationNames не отсортирован: %q > %q", names[i-1], names[i])
		}
	}
	if versionOf(names[0]) != "0001" {
		t.Errorf("первая миграция = %q, хочу 0001", names[0])
	}
}

// TestApplyOnEmptySchema прогоняет раннер с нуля: после сброса схемы Apply
// применяет 0001 (создаёт users / subscriptions / notifications_log и
// schema_migrations), повторный запуск — no-op, Applied совпадает с файлами.
func TestApplyOnEmptySchema(t *testing.T) {
	if !hasDB {
		t.Skip("NOTIFIER_TEST_DSN не задан — Pg-тесты скипаются (в CI Postgres нет)")
	}
	ctx := context.Background()

	// Пустая схема: раннер обязан работать с нуля, как при docker compose down -v.
	if _, err := testPool.Exec(ctx, `DROP SCHEMA IF EXISTS notifier CASCADE`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}

	if err := Apply(ctx, testPool); err != nil {
		t.Fatalf("первый Apply: %v", err)
	}
	for _, table := range []string{"users", "subscriptions", "notifications_log", "schema_migrations"} {
		var exists bool
		err := testPool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='notifier' AND table_name=$1)`,
			table).Scan(&exists)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("после Apply нет таблицы notifier.%s", table)
		}
	}

	// Повторный запуск не применяет 0001 повторно и не падает.
	if err := Apply(ctx, testPool); err != nil {
		t.Fatalf("повторный Apply: %v", err)
	}

	// Список применённых версий совпадает с файлами миграций.
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	want := make([]string, len(names))
	for i, n := range names {
		want[i] = versionOf(n)
	}
	got, err := Applied(ctx, testPool)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("Applied = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Applied[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
