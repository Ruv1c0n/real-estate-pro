// Package db отвечает за подключение notifier к PostgreSQL (схема notifier).
package db

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Дефолты для частей DSN, если соответствующая переменная не задана.
const (
	defaultHost     = "127.0.0.1"
	defaultPort     = "5432"
	defaultUser     = "app"
	defaultPassword = "app"
	defaultDB       = "realestate"
)

// Connect открывает пул соединений с Postgres.
//
// Если ни одной переменной POSTGRES_* не задано или пинг не прошёл при старте,
// возвращает (nil, nil) и не падает: сервис продолжает работать на in-memory
// хранилище — так живут CI и локальные тесты без Docker.
func Connect(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := dsnFromEnv()
	if dsn == "" {
		log.Println("DB disabled: running in memory mode")
		return nil, nil
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 5

	// Пинг с таймаутом: недоступная база не должна блокировать старт.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(pingCtx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		log.Printf("DB disabled: running in memory mode (ping: %v)", err)
		return nil, nil
	}

	log.Println("DB connected")
	return pool, nil
}

// dsnFromEnv собирает DSN из окружения. Задан DATABASE_URL (legacy-вариант
// с main) — используется как есть, без разбора частей. Ни одной переменной
// POSTGRES_* нет — БД не сконфигурирована, возвращается пустая строка.
func dsnFromEnv() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}

	host := os.Getenv("POSTGRES_HOST")
	port := os.Getenv("POSTGRES_PORT")
	user := os.Getenv("POSTGRES_USER")
	password := os.Getenv("POSTGRES_PASSWORD")
	name := os.Getenv("POSTGRES_DB")

	if host == "" && port == "" && user == "" && password == "" && name == "" {
		return ""
	}

	if host == "" {
		host = defaultHost
	}
	if port == "" {
		port = defaultPort
	}
	if user == "" {
		user = defaultUser
	}
	if password == "" {
		password = defaultPassword
	}
	if name == "" {
		name = defaultDB
	}

	// Keyword-value формат, а не URL: знак @ и другие спецсимволы пароля
	// его не ломают (тот же класс бага, что словили в Python на PR #39).
	return "host=" + quoteValue(host) +
		" port=" + quoteValue(port) +
		" user=" + quoteValue(user) +
		" password=" + quoteValue(password) +
		" dbname=" + quoteValue(name) +
		" sslmode=disable"
}

// quoteValue экранирует значение по правилам pgx-connstring: значения с
// пробелами, кавычками и обратным слэшем берутся в одинарные кавычки,
// внутри них экранируются `\` и кавычки.
func quoteValue(v string) string {
	if !strings.ContainsAny(v, " \t\n'\"\\") {
		return v
	}
	q := strings.ReplaceAll(v, `\`, `\\`)
	q = strings.ReplaceAll(q, `'`, `\'`)
	q = strings.ReplaceAll(q, `"`, `\"`)
	return "'" + q + "'"
}
