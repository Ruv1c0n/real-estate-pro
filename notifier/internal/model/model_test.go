package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const testUUID = "550e8400-e29b-41d4-a716-446655440000"

func baseEvent() Event {
	address := "ул. Примерная, 1"
	return Event{
		EventID:   testUUID,
		ID:        123,
		Source:    "mock",
		DealType:  DealRent,
		Country:   "BY",
		City:      "Minsk",
		Rooms:     1,
		Price:     380,
		Currency:  "USD",
		PriceUSD:  380,
		Address:   &address,
		URL:       "https://example.com/123",
		CreatedAt: time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC),
	}
}

// TestEventMarshalJSONContractForm — MarshalJSON даёт контрактную форму
// (contracts.md §2, snake_case): все 13 ключей присутствуют, Go-имена полей
// не просачиваются, ParseEvent разбирает payload обратно без потерь.
// Юнит-тест без БД: Pg-тест round-trip скипается в CI (нет postgres).
func TestEventMarshalJSONContractForm(t *testing.T) {
	address := "ул. Примерная, 2"
	e := baseEvent()
	e.Address = &address

	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{
		"event_id", "id", "source", "deal_type", "country", "city", "rooms",
		"price", "currency", "price_usd", "address", "url", "created_at",
	}
	if len(keys) != len(want) {
		t.Fatalf("ключей в payload %d, want %d: %s", len(keys), len(want), raw)
	}
	for _, k := range want {
		if _, ok := keys[k]; !ok {
			t.Errorf("нет контрактного ключа %q: %s", k, raw)
		}
	}
	for _, k := range []string{"EventID", "PriceUSD", "DealType", "CreatedAt", "Address"} {
		if _, ok := keys[k]; ok {
			t.Errorf("Go-имя поля %q просочилось в payload: %s", k, raw)
		}
	}

	back, err := ParseEvent(raw)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if back.EventID != e.EventID || back.ID != e.ID || back.Source != e.Source ||
		back.DealType != e.DealType || back.Country != e.Country || back.City != e.City ||
		back.Rooms != e.Rooms || back.Price != e.Price || back.Currency != e.Currency ||
		back.PriceUSD != e.PriceUSD || back.URL != e.URL ||
		back.Address == nil || *back.Address != *e.Address ||
		!back.CreatedAt.Equal(e.CreatedAt) {
		t.Fatalf("round-trip потерял данные:\n got %+v\nwant %+v", back, e)
	}

	// address=nil: ключ присутствует со значением null, ParseEvent проходит.
	e.Address = nil
	raw2, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal null address: %v", err)
	}
	var keys2 map[string]json.RawMessage
	if err := json.Unmarshal(raw2, &keys2); err != nil {
		t.Fatalf("unmarshal null address: %v", err)
	}
	if _, ok := keys2["address"]; !ok {
		t.Errorf("address=nil: ключ отсутствует в payload: %s", raw2)
	}
	if _, err := ParseEvent(raw2); err != nil {
		t.Fatalf("ParseEvent с address=nil: %v", err)
	}
}

func TestSubscriptionMatches(t *testing.T) {
	tests := []struct {
		name string
		sub  Subscription
		mod  func(*Event)
		want bool
	}{
		{"пустая подписка подходит всему", Subscription{}, nil, true},
		{"1-к в Минске до $400", Subscription{City: "Minsk", Rooms: IntPtr(1), MaxPriceUSD: Int64Ptr(400)}, nil, true},
		{"город без учёта регистра", Subscription{City: "minsk"}, nil, true},
		{"другой город", Subscription{City: "Brest"}, nil, false},
		{"другая страна", Subscription{Country: "PL"}, nil, false},
		{"другой тип сделки", Subscription{DealType: DealSale}, nil, false},
		{"цена ровно на границе", Subscription{MaxPriceUSD: Int64Ptr(380)}, nil, true},
		{"дороже максимума", Subscription{MaxPriceUSD: Int64Ptr(300)}, nil, false},
		{"дешевле минимума", Subscription{MinPriceUSD: Int64Ptr(400)}, nil, false},
		{"другая комнатность", Subscription{Rooms: IntPtr(2)}, nil, false},
		{"студия", Subscription{Rooms: IntPtr(0)}, func(e *Event) { e.Rooms = 0 }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := baseEvent()
			if tt.mod != nil {
				tt.mod(&e)
			}
			if got := tt.sub.Matches(e); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchAll(t *testing.T) {
	subs := []Subscription{
		{ID: 1, City: "Minsk"},
		{ID: 2, City: "Brest"},
		{ID: 3, MaxPriceUSD: Int64Ptr(400)},
	}
	got := MatchAll(baseEvent(), subs)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("MatchAll() = %+v, want subs 1 and 3", got)
	}
}

func TestRecipientsDedupsChats(t *testing.T) {
	subs := []Subscription{
		{ID: 1, ChatID: 10, City: "Minsk"},
		{ID: 2, ChatID: 10, Rooms: IntPtr(1)},           // тот же чат — одно уведомление
		{ID: 3, ChatID: 20, MaxPriceUSD: Int64Ptr(500)}, // другой чат
		{ID: 4, ChatID: 30, City: "Brest"},              // не совпал
	}
	got := Recipients(baseEvent(), subs)
	if len(got) != 2 || got[0] != 10 || got[1] != 20 {
		t.Fatalf("Recipients() = %v, want [10 20]", got)
	}
}

const validJSON = `{
	"event_id": "550e8400-e29b-41d4-a716-446655440000",
	"id": 123,
	"source": "mock",
	"deal_type": "rent",
	"country": "BY",
	"city": "Minsk",
	"rooms": 1,
	"price": 380,
	"currency": "USD",
	"price_usd": 380,
	"address": "ул. Примерная, 1",
	"url": "https://example.com/listing/123",
	"created_at": "2026-10-28T10:00:00Z"
}`

func TestParseEventValid(t *testing.T) {
	e, err := ParseEvent([]byte(validJSON))
	if err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	if e.EventID != testUUID || e.ID != 123 || e.Rooms != 1 || e.PriceUSD != 380 {
		t.Errorf("unexpected parse result: %+v", e)
	}
	if e.Address == nil || *e.Address != "ул. Примерная, 1" {
		t.Errorf("address = %v", e.Address)
	}
	if e.CreatedAt.IsZero() {
		t.Error("created_at not parsed")
	}
}

func TestParseEventAddressNull(t *testing.T) {
	raw := `{"event_id":"` + testUUID + `","id":1,"source":"mock","deal_type":"sale",
		"country":"BY","city":"Minsk","rooms":0,"price":100,"currency":"USD",
		"price_usd":100,"address":null,"url":"https://x.y/1","created_at":"2026-10-28T10:00:00Z"}`
	e, err := ParseEvent([]byte(raw))
	if err != nil {
		t.Fatalf("address null must be accepted: %v", err)
	}
	if e.Address != nil {
		t.Errorf("address = %v, want nil", *e.Address)
	}
}

func TestParseEventRejects(t *testing.T) {
	with := func(repls ...string) string {
		s := validJSON
		for i := 0; i+1 < len(repls); i += 2 {
			s = strings.Replace(s, repls[i], repls[i+1], 1)
		}
		return s
	}
	bad := map[string]string{
		"не JSON":                `{oops`,
		"старый формат (до #24)": with(`"event_id": "`+testUUID+`",`, `"title": "1-к",`),
		"rooms null":             with(`"rooms": 1,`, `"rooms": null,`),
		"rooms отсутствует":      with(`"rooms": 1,`, ``),
		"нет event_id":           with(`"event_id": "`+testUUID+`",`, ``),
		"битый UUID":             with(`550e8400-e29b-41d4-a716-446655440000`, `550e840-e29b-41d4-a716-446655440000`),
		"price_usd дробный":      with(`"price_usd": 380,`, `"price_usd": 380.5,`),
		"price_usd ноль":         with(`"price_usd": 380,`, `"price_usd": 0,`),
		"нет created_at":         with(`"created_at": "2026-10-28T10:00:00Z"`, `"created_at": ""`),
		"неверный deal_type":     with(`"deal_type": "rent"`, `"deal_type": "buy"`),
		"country не alpha-2":     with(`"country": "BY"`, `"country": "blr"`),
		"относительный url":      with(`"url": "https://example.com/listing/123"`, `"url": "listing/123"`),
	}
	for name, payload := range bad {
		if _, err := ParseEvent([]byte(payload)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}
