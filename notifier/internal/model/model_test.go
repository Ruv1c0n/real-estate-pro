package model

import "testing"

func baseEvent() Event {
	return Event{
		ID:       1,
		Source:   "mock",
		DealType: DealRent,
		Country:  "BY",
		City:     "Minsk",
		Rooms:    IntPtr(1),
		PriceUSD: 350,
		Title:    "1-к квартира",
		URL:      "https://example.com/1",
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
		{"1-к в Минске до $400", Subscription{City: "Minsk", Rooms: IntPtr(1), MaxPriceUSD: FloatPtr(400)}, nil, true},
		{"город без учёта регистра", Subscription{City: "minsk"}, nil, true},
		{"другой город", Subscription{City: "Brest"}, nil, false},
		{"другая страна", Subscription{Country: "PL"}, nil, false},
		{"другой тип сделки", Subscription{DealType: DealSale}, nil, false},
		{"цена ровно на границе", Subscription{MaxPriceUSD: FloatPtr(350)}, nil, true},
		{"дороже максимума", Subscription{MaxPriceUSD: FloatPtr(300)}, nil, false},
		{"дешевле минимума", Subscription{MinPriceUSD: FloatPtr(400)}, nil, false},
		{"другая комнатность", Subscription{Rooms: IntPtr(2)}, nil, false},
		{"студия", Subscription{Rooms: IntPtr(0)}, func(e *Event) { e.Rooms = IntPtr(0) }, true},
		{"комнатность неизвестна, а подписка её требует", Subscription{Rooms: IntPtr(1)}, func(e *Event) { e.Rooms = nil }, false},
		{"комнатность неизвестна, подписка не требует", Subscription{City: "Minsk"}, func(e *Event) { e.Rooms = nil }, true},
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
		{ID: 3, MaxPriceUSD: FloatPtr(400)},
	}
	got := MatchAll(baseEvent(), subs)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("MatchAll() = %+v, want subs 1 and 3", got)
	}
}

func TestParseEvent(t *testing.T) {
	valid := `{"id":123,"source":"avito","deal_type":"rent","country":"BY","city":"Minsk",
	"rooms":1,"price":1100,"currency":"BYN","price_usd":350,"title":"1-к","url":"https://x.y/1",
	"created_at":"2026-10-05T12:30:00Z"}`

	e, err := ParseEvent([]byte(valid))
	if err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	if e.ID != 123 || e.Rooms == nil || *e.Rooms != 1 || e.CreatedAt.IsZero() {
		t.Errorf("unexpected parse result: %+v", e)
	}

	nullRooms := `{"id":1,"deal_type":"sale","country":"BY","city":"Minsk","rooms":null,"price_usd":50000,"url":"u"}`
	if e, err := ParseEvent([]byte(nullRooms)); err != nil || e.Rooms != nil {
		t.Errorf("null rooms: err=%v rooms=%v", err, e.Rooms)
	}

	bad := map[string]string{
		"не JSON":            `{oops`,
		"нет id":             `{"deal_type":"rent","country":"BY","city":"Minsk","price_usd":1,"url":"u"}`,
		"неверный deal_type": `{"id":1,"deal_type":"buy","country":"BY","city":"Minsk","price_usd":1,"url":"u"}`,
		"нулевая цена":       `{"id":1,"deal_type":"rent","country":"BY","city":"Minsk","price_usd":0,"url":"u"}`,
		"нет города":         `{"id":1,"deal_type":"rent","country":"BY","price_usd":1,"url":"u"}`,
	}
	for name, payload := range bad {
		if _, err := ParseEvent([]byte(payload)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}