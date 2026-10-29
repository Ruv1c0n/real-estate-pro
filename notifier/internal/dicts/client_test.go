package dicts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newServer поднимает Core API с переключателем исправности: пока fail=0,
// отдаёт справочники и считает запросы.
func newServer(t *testing.T) (*Client, *httptest.Server, *atomic.Int32) {
	t.Helper()
	var fail atomic.Bool
	var countriesCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/countries":
			countriesCalls.Add(1)
			w.Write([]byte(`[{"code":"BY","name":"Беларусь"},{"code":"RU","name":"Россия"}]`))
		case "/api/v1/cities":
			switch r.URL.Query().Get("country") {
			case "BY":
				w.Write([]byte(`[{"country":"BY","name":"Minsk","aliases":["Минск","minsk"]},
					{"country":"BY","name":"Brest","aliases":[]}]`))
			case "RU":
				w.Write([]byte(`[{"country":"RU","name":"Moscow","aliases":["Москва"]}]`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	// ttl=0 — кэш всегда «просрочен», но данные остаются как запас.
	c := New(srv.URL)
	c.ttl = 0
	return c, srv, &countriesCalls
}

func TestValidateCanonicalizesCountryAndCity(t *testing.T) {
	c, _, _ := newServer(t)

	country, city, err := c.Validate(context.Background(), "by", "Минск")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if country != "BY" || city != "Minsk" {
		t.Fatalf("got (%q, %q), want (BY, Minsk)", country, city)
	}
}

func TestValidateCityWithoutCountry(t *testing.T) {
	c, _, _ := newServer(t)

	country, city, err := c.Validate(context.Background(), "", "moscow")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if country != "RU" || city != "Moscow" {
		t.Fatalf("got (%q, %q), want (RU, Moscow)", country, city)
	}
}

func TestValidateRejectsUnknown(t *testing.T) {
	c, _, _ := newServer(t)
	ctx := context.Background()

	if _, _, err := c.Validate(ctx, "XX", ""); err == nil {
		t.Error("неизвестная страна должна отклоняться")
	}
	if _, _, err := c.Validate(ctx, "BY", "Gomel"); err == nil {
		t.Error("неизвестный город должен отклоняться")
	}
	if _, _, err := c.Validate(ctx, "", "Nowhere"); err == nil {
		t.Error("город вне справочника должен отклоняться")
	}
}

func TestValidateCountryOnly(t *testing.T) {
	c, _, _ := newServer(t)
	country, city, err := c.Validate(context.Background(), "ru", "")
	if err != nil || country != "RU" || city != "" {
		t.Fatalf("got (%q, %q, %v), want (RU, \"\", nil)", country, city, err)
	}
}

func TestCacheServesRepeatedCalls(t *testing.T) {
	c, _, calls := newServer(t)
	c.ttl = time.Minute // свежий кэш

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, _, err := c.Validate(ctx, "BY", "Minsk"); err != nil {
			t.Fatalf("Validate #%d: %v", i, err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("countries fetched %d раз, want 1 (кэш)", n)
	}
}

func TestStaleCacheWhenCoreAPIDown(t *testing.T) {
	c, srv, _ := newServer(t)
	ctx := context.Background()

	if _, _, err := c.Validate(ctx, "BY", "Minsk"); err != nil {
		t.Fatalf("первая Validate: %v", err)
	}
	srv.Close() // Core API упал

	country, city, err := c.Validate(ctx, "BY", "Minsk")
	if err != nil {
		t.Fatalf("при недоступности Core API должен работать кэш: %v", err)
	}
	if country != "BY" || city != "Minsk" {
		t.Fatalf("got (%q, %q)", country, city)
	}
}

func TestUnavailableWithoutCache(t *testing.T) {
	c := New("http://127.0.0.1:1") // никуда не ведёт
	c.http.Timeout = 200 * time.Millisecond

	_, _, err := c.Validate(context.Background(), "BY", "Minsk")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
}

func TestHealthy(t *testing.T) {
	c, srv, _ := newServer(t)
	ctx := context.Background()
	if !c.Healthy(ctx) {
		t.Fatal("Healthy должен быть true при живом Core API")
	}
	srv.Close()
	if !c.Healthy(ctx) {
		t.Fatal("Healthy должен быть true по кэшу после падения Core API")
	}
	c2 := New("http://127.0.0.1:1")
	c2.http.Timeout = 200 * time.Millisecond
	if c2.Healthy(ctx) {
		t.Fatal("Healthy должен быть false без Core API и кэша")
	}
}
