// Тесты сборки DSN из окружения. Полный набор dsn-кейсов — карточка R7;
// здесь минимальный табличный тест dsnFromEnv: пустое окружение, sslmode
// из POSTGRES_SSLMODE и дефолт disable.
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
