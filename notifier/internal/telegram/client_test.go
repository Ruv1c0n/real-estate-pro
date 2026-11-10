package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient — клиент поверх httptest-сервера, отвечающего handler'ом.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{token: "test-token", base: srv.URL, http: srv.Client()}
}

func TestRateLimitedCarriesRetryAfter(t *testing.T) {
	// 429 с parameters.retry_after: типизированная ошибка с паузой из ответа.
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 5","parameters":{"retry_after":5}}`))
	})

	err := c.SendMessage(context.Background(), 10, "привет")

	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("err = %v, want *RateLimitError", err)
	}
	if rle.RetryAfter != 5*time.Second {
		t.Fatalf("RetryAfter = %s, want 5s", rle.RetryAfter)
	}
}

func TestRateLimitedWithoutParametersUsesDefault(t *testing.T) {
	// Поле parameters.retry_after отсутствует — берётся defaultRetryAfter.
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests"}`))
	})

	err := c.SendMessage(context.Background(), 10, "привет")

	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("err = %v, want *RateLimitError", err)
	}
	if rle.RetryAfter != defaultRetryAfter {
		t.Fatalf("RetryAfter = %s, want %s (дефолт)", rle.RetryAfter, defaultRetryAfter)
	}
}

func TestOtherAPIErrorsNotRateLimited(t *testing.T) {
	// Не-429 ответ — обычная ошибка: в доставке остаётся терминальный
	// unknown без повтора (contracts.md §2).
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
	})

	err := c.SendMessage(context.Background(), 10, "привет")

	if err == nil {
		t.Fatal("want error")
	}
	if errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want НЕ ErrRateLimited (400 — не rate limit)", err)
	}
}

func TestSendMessageOK(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	})

	if err := c.SendMessage(context.Background(), 10, "привет"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
}
