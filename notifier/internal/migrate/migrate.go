// Package migrate — раннер миграций notifier: применяет SQL-файлы из
// notifier/migrations к базе при старте сервиса.
//
// Схему БД меняют только миграции Ильи (файлы в notifier/migrations) —
// раннер лишь ПРИМЕНЯЕТ их, сам ничего не создаёт, кроме служебной таблицы
// schema_migrations для учёта применённых версий. Применение при деплое —
// зона Даниила (issue #31, этап 2).
package migrate

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"notifier/migrations"
)

// advisoryLockKey — константный ключ session-level advisory-лока раннера.
// Значение — hashtext('notifier_migrations') (LE-платформы: x86/ARM), можно
// перепроверить запросом SELECT hashtext('notifier_migrations').
// Ключ именно константный, а не вычисляемый на месте: все экземпляры
// notifier (compose, ручной запуск, второй контейнер при параллельном
// деплое) должны блокировать один и тот же ключ, иначе лок не сработает.
const advisoryLockKey int64 = -543989193

// Apply накатывает ещё не применённые миграции на базу за пулом.
//
// Файл выполняется КАК ЕСТЬ одним Exec, БЕЗ собственной транзакции раннера:
// в файлах миграций уже есть свои BEGIN/COMMIT, вложенный BEGIN сломал бы
// применение (pgx в simple-protocol выполняет такой файл атомарно).
//
// От параллельного запуска Apply защищён advisory-локом (см. advisoryLockKey).
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	// Advisory-лок берётся на ВЫДЕЛЕННОМ соединении (pool.Acquire), которое
	// держится весь Apply: файлы миграций ведут собственные транзакции, поэтому
	// обернуть выполнение во внешнюю транзакцию раннера нельзя (вложенный
	// BEGIN упал бы), а transaction-level pg_advisory_xact_lock отпал бы
	// вместе с транзакцией файла. Session-level pg_advisory_lock на
	// выделенном соединении держится независимо от чужих транзакций и
	// снимается явно на том же соединении до возврата его в пул — иначе
	// соединение ушло бы в пул с удерживаемым локом. Захват блокирующий:
	// второй раннер дождётся очереди и после снятия лока увидит версии уже
	// применёнными. Пул обязан иметь MaxConns >= 2 (в db.Connect — 5):
	// одно соединение занято локом, файлы идут по остальным.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrations: acquire connection for advisory lock: %w", err)
	}
	// Release — последним: defer'ы срабатывают LIFO, поэтому
	// зарегистрированный ниже unlock выполнится ДО возврата соединения в пул.
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		return fmt.Errorf("migrations: acquire advisory lock: %w", err)
	}
	// Лок снимаем на том же соединении и вне отмены родительского ctx
	// (Apply может завершиться по отмене ctx — лок всё равно обязан быть
	// снят; если соединение сломалось, Release уничтожит его вместе с локом).
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx),
			`SELECT pg_advisory_unlock($1)`, advisoryLockKey); err != nil {
			log.Printf("migrations: release advisory lock: %v", err)
		}
	}()

	// Схему создаёт 0001, но до применения 0001 раннеру нужна служебная
	// таблица — на чистом деплое (docker compose down -v) схемы ещё нет,
	// поэтому гарантируем её сами. Это код раннера, файл 0001 не трогаем.
	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS notifier`); err != nil {
		return fmt.Errorf("migrations: create schema: %w", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS notifier.schema_migrations (
	    version    TEXT        PRIMARY KEY,
	    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("migrations: create schema_migrations: %w", err)
	}

	// Служебная таблица могла только что создаться вместе со схемой —
	// ищем только в ней, пути search_path не используем.
	applied := make(map[string]bool)
	rows, err := pool.Query(ctx, `SELECT version FROM notifier.schema_migrations`)
	if err != nil {
		return fmt.Errorf("migrations: list applied: %w", err)
	}
	// Close идемпотентен — defer сразу после успешного Query закрывает строки
	// на любом пути выхода, ручные Close по веткам не нужны.
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("migrations: scan applied: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migrations: list applied: %w", err)
	}

	names, err := migrationNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		version := versionOf(name)
		if applied[version] {
			log.Printf("migration %s already applied", version)
			continue
		}
		sql, err := migrations.FS().ReadFile(name)
		if err != nil {
			return fmt.Errorf("migrations: read %s: %w", name, err)
		}
		// Один Exec, файл как есть — транзакцию ведёт сам файл.
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migrations: apply %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO notifier.schema_migrations (version) VALUES ($1)`, version); err != nil {
			return fmt.Errorf("migrations: record %s: %w", version, err)
		}
		log.Printf("migration %s applied", version)
	}
	return nil
}

// applied возвращает отсортированный список версий, отмеченных в
// schema_migrations, — для тестов раннера (тот же пакет, экспорт не нужен).
func applied(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT version FROM notifier.schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// migrationNames читает имена *.sql из встраиваемого каталога в порядке
// сортировки по имени (лексикографической — по ней и применяется раннер).
func migrationNames() ([]string, error) {
	entries, err := migrations.FS().ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("migrations: read dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	// ReadDir уже возвращает отсортированный список, сортировка — страховка.
	sort.Strings(names)
	return names, nil
}

// versionOf вырезает версию из имени файла: 0001_init.sql -> 0001.
func versionOf(name string) string {
	name = strings.TrimSuffix(name, ".sql")
	if i := strings.IndexByte(name, '_'); i >= 0 {
		name = name[:i]
	}
	return name
}
