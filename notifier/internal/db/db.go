// Package db отвечает за подключение notifier к PostgreSQL (схема notifier).
package db

import (
	"context"
	"fmt"
	"log/slog"
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
	defaultSSLMode  = "disable"
)

// pingAttempts — число попыток пинга при старте (задержки 1s, 2s между ними).
const pingAttempts = 3

// Connect открывает пул соединений с Postgres.
//
// (nil, nil) возвращается только когда БД не сконфигурирована (ни одной
// переменной POSTGRES_*): сервис продолжает работать на in-memory хранилище —
// так живут CI и локальные тесты без Docker. Сконфигурированная, но
// недоступная БД — ошибка старта (fail-fast, main.go делает fatal()):
// тихий уход в memory-режим маскировал бы проблему окружения весь аптайм.
// Частичная конфигурация (задана часть POSTGRES_* без user/password) — тоже
// ошибка: иначе misconfig молча превращается в подключение app/app@127.0.0.1.
// Переподключение в процессе работы обеспечивает сам pgxpool.
func Connect(ctx context.Context) (*pgxpool.Pool, error) {
	dsn, err := dsnFromEnv()
	if err != nil {
		return nil, err
	}
	if dsn == "" {
		slog.Info("db: disabled — running in memory mode")
		return nil, nil
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse dsn: %w", err)
	}
	cfg.MaxConns = 5

	// Ограничения соединений: ConnectTimeout не даёт установке соединения
	// висеть бесконечно; statement_timeout — сессионный лимит на запрос,
	// зависший запрос не держит соединение пула вечно.
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5s"

	// Пинг с ограниченными ретраями: до 3 попыток с задержкой 1s, 2s между
	// ними — Postgres при одновременном старте docker compose может ещё
	// подниматься. Общий бюджет 10s ограничивает блокировку старта сверху:
	// при исчерпании попыток или бюджета — ошибка (fail-fast), а не ожидание.
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(pingCtx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}

	var pingErr error
	for attempt := 1; ; attempt++ {
		if pingErr = pool.Ping(pingCtx); pingErr == nil {
			break
		}
		// Попытки или бюджет (отмена ctx) исчерпаны — отдаём ошибку наверх.
		if attempt == pingAttempts || pingCtx.Err() != nil {
			break
		}
		// Пауза между попытками (1s, 2s); досрочный выход — при отмене
		// контекста или исчерпании бюджета.
		select {
		case <-pingCtx.Done():
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	if pingErr != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", pingErr)
	}

	slog.Info("db: connected")
	return pool, nil
}

// dsnFromEnv собирает DSN из окружения. Задан DATABASE_URL (legacy-вариант
// с main) — используется как есть, без разбора частей. Ни одной переменной
// POSTGRES_* нет — БД не сконфигурирована, возвращается пустая строка.
// Задана хоть одна часть POSTGRES_* — user и password обязательны явно:
// недостающие части подставлялись бы дефолтами, и опечатка в имени
// переменной (например, задан только POSTGRES_DB) дала бы «рабочее»
// подключение app/app@127.0.0.1 вместо ошибки конфигурации.
func dsnFromEnv() (string, error) {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url, nil
	}

	host := os.Getenv("POSTGRES_HOST")
	port := os.Getenv("POSTGRES_PORT")
	user := os.Getenv("POSTGRES_USER")
	password := os.Getenv("POSTGRES_PASSWORD")
	name := os.Getenv("POSTGRES_DB")
	sslmode := os.Getenv("POSTGRES_SSLMODE")

	if host == "" && port == "" && user == "" && password == "" && name == "" && sslmode == "" {
		return "", nil
	}

	if user == "" || password == "" {
		return "", fmt.Errorf("db: POSTGRES_USER и POSTGRES_PASSWORD должны быть заданы явно, когда задана любая из переменных POSTGRES_*")
	}

	if host == "" {
		host = defaultHost
	}
	if port == "" {
		port = defaultPort
	}
	if name == "" {
		name = defaultDB
	}
	if sslmode == "" {
		sslmode = defaultSSLMode
	}

	// Keyword-value формат, а не URL: знак @ и другие спецсимволы пароля
	// его не ломают (тот же класс бага, что словили в Python на PR #39).
	return "host=" + quoteValue(host) +
		" port=" + quoteValue(port) +
		" user=" + quoteValue(user) +
		" password=" + quoteValue(password) +
		" dbname=" + quoteValue(name) +
		" sslmode=" + quoteValue(sslmode), nil
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
