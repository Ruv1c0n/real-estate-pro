// Тесты сборки DSN из окружения (карточка R7). Табличный тест dsnFromEnv:
// пустое окружение, дефолты частей при частичной конфигурации, passthrough
// DATABASE_URL, экранирование спецсимволов quoteValue, sslmode из
// POSTGRES_SSLMODE и дефолт disable.
package db

import (
	"strings"
	"testing"
)

// envKeys — все переменные окружения, влияющие на dsnFromEnv; тест гасит их,
// чтобы не зависеть от окружения (CI, локальный .env).
var envKeys = []string{
	"DATABASE_URL",
	"POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_USER",
	"POSTGRES_PASSWORD", "POSTGRES_DB", "POSTGRES_SSLMODE",
}

func TestDsnFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    string // точное значение DSN (пустая строка)
		wantSub string // обязательная подстрока DSN
	}{
		{
			name: "ничего не задано — БД не сконфигурирована",
			env:  nil,
			want: "",
		},
		{
			name:    "POSTGRES_SSLMODE=require попадает в DSN",
			env:     map[string]string{"POSTGRES_SSLMODE": "require"},
			wantSub: "sslmode=require",
		},
		{
			name:    "sslmode не задан — дефолт disable",
			env:     map[string]string{"POSTGRES_HOST": "db"},
			wantSub: "sslmode=disable",
		},
		{
			name: "только POSTGRES_DB задан — остальные части из дефолтов",
			env:  map[string]string{"POSTGRES_DB": "realestatetest"},
			want: "host=127.0.0.1 port=5432 user=app password=app dbname=realestatetest sslmode=disable",
		},
		{
			name: "все POSTGRES_* заданы — значения без дефолтов и без кавычек",
			env: map[string]string{
				"POSTGRES_HOST":     "db",
				"POSTGRES_PORT":     "6543",
				"POSTGRES_USER":     "notifier",
				"POSTGRES_PASSWORD": "secret",
				"POSTGRES_DB":       "notifier",
				"POSTGRES_SSLMODE":  "require",
			},
			want: "host=db port=6543 user=notifier password=secret dbname=notifier sslmode=require",
		},
		{
			name: "DATABASE_URL задан — passthrough как есть, части не разбираются",
			env:  map[string]string{"DATABASE_URL": "postgres://u:p@db:5432/x?sslmode=require"},
			want: "postgres://u:p@db:5432/x?sslmode=require",
		},
		{
			name: "DATABASE_URL пуст, POSTGRES_* нет — БД не сконфигурирована",
			env:  map[string]string{"DATABASE_URL": ""},
			want: "",
		},
		{
			name: "пароль с пробелом, кавычкой, обратным слэшем и @ — экранирование quoteValue",
			env:  map[string]string{"POSTGRES_PASSWORD": `pa ss"w'rd\@`},
			want: `host=127.0.0.1 port=5432 user=app password='pa ss\"w\'rd\\@' dbname=realestate sslmode=disable`,
		},
		{
			name: "пароль только с @ — без кавычек: keyword-value формат его не ломает",
			env:  map[string]string{"POSTGRES_PASSWORD": `p@ss`},
			want: `host=127.0.0.1 port=5432 user=app password=p@ss dbname=realestate sslmode=disable`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range envKeys {
				t.Setenv(k, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			dsn := dsnFromEnv()
			if tt.wantSub != "" {
				if !strings.Contains(dsn, tt.wantSub) {
					t.Fatalf("dsn %q не содержит %q", dsn, tt.wantSub)
				}
				return
			}
			if dsn != tt.want {
				t.Fatalf("dsnFromEnv() = %q, want %q", dsn, tt.want)
			}
		})
	}
}
