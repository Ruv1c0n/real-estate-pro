package bot

import (
	"strings"
	"testing"

	"notifier/internal/model"
)

func TestParseSubscription(t *testing.T) {
	s, err := ParseSubscription(strings.Fields("city=Minsk rooms=1 max=400 deal=rent country=by min=$100"))
	if err != nil {
		t.Fatal(err)
	}
	if s.City != "Minsk" || s.Country != "BY" || s.DealType != model.DealRent {
		t.Errorf("unexpected text fields: %+v", s)
	}
	if s.Rooms == nil || *s.Rooms != 1 {
		t.Errorf("rooms: %v", s.Rooms)
	}
	if s.MaxPriceUSD == nil || *s.MaxPriceUSD != 400 || s.MinPriceUSD == nil || *s.MinPriceUSD != 100 {
		t.Errorf("prices: min=%v max=%v", s.MinPriceUSD, s.MaxPriceUSD)
	}
}

func TestParseSubscriptionCityWithSpace(t *testing.T) {
	s, err := ParseSubscription(strings.Fields("city=New York max=500"))
	if err != nil {
		t.Fatal(err)
	}
	if s.City != "New York" {
		t.Errorf("city = %q", s.City)
	}
}

func TestParseSubscriptionStudio(t *testing.T) {
	s, err := ParseSubscription([]string{"rooms=studio"})
	if err != nil || s.Rooms == nil || *s.Rooms != 0 {
		t.Fatalf("err=%v rooms=%v", err, s.Rooms)
	}
}

func TestParseSubscriptionErrors(t *testing.T) {
	bad := map[string]string{
		"пусто":            "",
		"без ключа":        "Minsk",
		"неизвестный ключ": "foo=bar",
		"плохая комната":   "rooms=abc",
		"отрицательная":    "max=-5",
		"плохой deal":      "deal=swap",
		"плохая страна":    "country=BLR",
		"min больше max":   "min=500 max=100",
	}
	for name, in := range bad {
		if _, err := ParseSubscription(strings.Fields(in)); err == nil {
			t.Errorf("%s: ожидалась ошибка для %q", name, in)
		}
	}
}