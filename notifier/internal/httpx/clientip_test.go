package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPFromRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.7:5678"
	if ip := ClientIP(req); ip != "198.51.100.7" {
		t.Fatalf("ClientIP = %q, want 198.51.100.7", ip)
	}

	// RemoteAddr без порта (некоторые транспорта) — не паникуем.
	req.RemoteAddr = "198.51.100.9"
	if ip := ClientIP(req); ip != "198.51.100.9" {
		t.Fatalf("ClientIP без порта = %q, want как есть", ip)
	}

	// IPv6 RemoteAddr: скобки и порт снимаются, адрес остаётся целиком —
	// дальше его режет /64-бакетирование лимитера.
	req.RemoteAddr = "[2001:db8::1]:443"
	if ip := ClientIP(req); ip != "2001:db8::1" {
		t.Fatalf("ClientIP IPv6 = %q, want 2001:db8::1", ip)
	}
}
