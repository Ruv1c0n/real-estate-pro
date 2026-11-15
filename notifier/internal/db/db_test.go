package db

import (
	"strings"
	"testing"
)

// Тесты dsnFromEnv не требуют БД — матрица конфигурации проверяется в CI.
func TestDsnFromEnvMatrix(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantDSN string // ожидаемая подстрока DSN; "" и err==nil — память без БД
		wantErr bool
	}{
		{
			name: "не сконфигурировано — memory-режим",
			env:  map[string]string{},
		},
		{
			name:    "полная конфигурация",
			env:     map[string]string{"POSTGRES_HOST": "db", "POSTGRES_PORT": "5433", "POSTGRES_USER": "app", "POSTGRES_PASSWORD": "app", "POSTGRES_DB": "realestate"},
			wantDSN: "host=db port=5433 user=app password=app dbname=realestate",
		},
		{
			name:    "дефолты host/port/db при явно заданных user/password",
			env:     map[string]string{"POSTGRES_USER": "u", "POSTGRES_PASSWORD": "p"},
			wantDSN: "host=127.0.0.1 port=5432 user=u password=p dbname=realestate",
		},
		{
			name:    "задана только POSTGRES_DB — ошибка, не дефолты app/app",
			env:     map[string]string{"POSTGRES_DB": "realestate"},
			wantErr: true,
		},
		{
			name:    "задан только POSTGRES_HOST — ошибка",
			env:     map[string]string{"POSTGRES_HOST": "db"},
			wantErr: true,
		},
		{
			name:    "user без password — ошибка",
			env:     map[string]string{"POSTGRES_USER": "u", "POSTGRES_HOST": "db"},
			wantErr: true,
		},
		{
			name:    "password без user — ошибка",
			env:     map[string]string{"POSTGRES_PASSWORD": "p", "POSTGRES_DB": "realestate"},
			wantErr: true,
		},
		{
			name:    "DATABASE_URL используется как есть",
			env:     map[string]string{"DATABASE_URL": "postgres://x:y@z/q", "POSTGRES_DB": "realestate"},
			wantDSN: "postgres://x:y@z/q",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			dsn, err := dsnFromEnv()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("dsnFromEnv() = %q, ожидалась ошибка", dsn)
				}
				return
			}
			if err != nil {
				t.Fatalf("dsnFromEnv(): неожиданная ошибка: %v", err)
			}
			if dsn == "" && tt.wantDSN != "" {
				t.Fatalf("dsnFromEnv() = пустая строка, ожидался DSN с %q", tt.wantDSN)
			}
			if tt.wantDSN != "" && !strings.Contains(dsn, tt.wantDSN) {
				t.Fatalf("dsnFromEnv() = %q, ожидалось вхождение %q", dsn, tt.wantDSN)
			}
		})
	}
}
