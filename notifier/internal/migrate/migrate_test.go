// Тесты раннера миграций. Pg-тесты запускаются только при заданном
// NOTIFIER_TEST_DSN: в CI (job notifier) Postgres нет — там тесты
// скипаются, это норма, а не ошибка.
//
// Локально: docker compose up -d postgres, затем
// NOTIFIER_TEST_DSN='host=127.0.0.1 user=app password=app dbname=realestate' go test ./internal/migrate/ -race
package migrate

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

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
// schema_migrations), повторный запуск — no-op, applied совпадает с файлами.
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
	got, err := applied(ctx, testPool)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("applied = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("applied[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestApplyParallel — гонка двух раннеров, каждый со своим пулом к одной БД
// (имитация параллельного деплоя: второй контейнер или ручной запуск рядом
// с compose). Оба Apply обязаны завершиться успешно: один применяет
// миграции, второй ждёт на advisory-локе и после его снятия находит версии
// уже записанными. Дублей версий в schema_migrations быть не должно
// (второй раннер не должен повторно применить файл).
func TestApplyParallel(t *testing.T) {
	if !hasDB {
		t.Skip("NOTIFIER_TEST_DSN не задан — Pg-тесты скипаются (в CI Postgres нет)")
	}
	// Страховка от зависания: если лок не сработает и раннеры примутся
	// драться за файлы, тест обязан упасть с ошибкой ctx, а не висеть.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dsn := os.Getenv("NOTIFIER_TEST_DSN")

	// Чистая схема: оба раннера стартуют с нуля, как при docker compose down -v.
	if _, err := testPool.Exec(ctx, `DROP SCHEMA IF EXISTS notifier CASCADE`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}

	const runners = 2
	errs := make([]error, runners)
	var wg sync.WaitGroup
	for i := 0; i < runners; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Отдельный пул на раннера — как отдельный процесс контейнера;
			// у каждого свой lock-коннект (pool.Acquire внутри Apply).
			pool, err := pgxpool.New(ctx, dsn)
			if err != nil {
				errs[i] = fmt.Errorf("pool %d: %w", i, err)
				return
			}
			defer pool.Close()
			errs[i] = Apply(ctx, pool)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("runner %d: Apply: %v", i, err)
		}
	}

	// Версии в schema_migrations — ровно по одной на каждый файл миграций.
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[versionOf(n)] = true
	}
	got, err := applied(ctx, testPool)
	if err != nil {
		t.Fatal(err)
	}
	gotSet := make(map[string]bool, len(got))
	for _, v := range got {
		gotSet[v] = true
	}
	for v := range want {
		if !gotSet[v] {
			t.Errorf("версия %q не записана в schema_migrations", v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("в schema_migrations %d версий, хочу %d (без дублей): %v", len(got), len(want), got)
	}
}
