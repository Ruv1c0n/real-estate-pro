package bot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"notifier/internal/dicts"
	"notifier/internal/model"
	"notifier/internal/store"
	"notifier/internal/telegram"
)

// fakeAPI записывает отправленные сообщения вместо реального Telegram.
type fakeAPI struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakeAPI) SendMessage(_ context.Context, _ int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
	return nil
}

func (f *fakeAPI) GetUpdates(context.Context, int64) ([]telegram.Update, error) {
	return nil, nil
}

func (f *fakeAPI) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return ""
	}
	return f.sent[len(f.sent)-1]
}

// stubValidator подменяет справочники Core API.
type stubValidator struct {
	country, city string
	err           error
}

func (s stubValidator) Validate(context.Context, string, string) (string, string, error) {
	return s.country, s.city, s.err
}

func newTestBot(validator Validator) (*Bot, *fakeAPI, *store.Memory) {
	api := &fakeAPI{}
	subs := store.NewMemory()
	return New(api, subs, validator), api, subs
}

func TestStopRemovesAllSubscriptions(t *testing.T) {
	b, api, subs := newTestBot(stubValidator{})
	chat := int64(10)
	subs.Add(model.Subscription{ChatID: chat, City: "Minsk"})
	subs.Add(model.Subscription{ChatID: chat, City: "Brest"})
	subs.Add(model.Subscription{ChatID: 20, City: "Minsk"})

	b.handle(context.Background(), chat, "/stop")

	if n := subs.CountForChat(chat); n != 0 {
		t.Errorf("подписок чата осталось %d, want 0", n)
	}
	if n := subs.CountForChat(20); n != 1 {
		t.Errorf("чужие подписки задеты: %d, want 1", n)
	}
	if !strings.Contains(api.last(), "удалено подписок — 2") {
		t.Errorf("ответ /stop: %q", api.last())
	}
}

func TestSubscribeLimitTenPerUser(t *testing.T) {
	b, api, _ := newTestBot(stubValidator{})
	chat := int64(10)
	for i := 0; i < MaxSubscriptionsPerUser; i++ {
		b.handle(context.Background(), chat, fmt.Sprintf("/subscribe min=%d", 100+i))
	}

	b.handle(context.Background(), chat, "/subscribe city=Minsk")
	if !strings.Contains(api.last(), "максимум") {
		t.Errorf("11-я подписка должна отклоняться, ответ: %q", api.last())
	}
}

func TestSubscribeValidatesWithDicts(t *testing.T) {
	b, _, subs := newTestBot(stubValidator{country: "BY", city: "Minsk"})

	b.handle(context.Background(), 10, "/subscribe country=by city=минск")

	got := subs.ByChat(10)
	if len(got) != 1 {
		t.Fatalf("подписок %d, want 1", len(got))
	}
	if got[0].Country != "BY" || got[0].City != "Minsk" {
		t.Errorf("канонизация не применена: %+v", got[0])
	}
}

func TestSubscribeRejectsWhenDictsUnavailable(t *testing.T) {
	b, api, subs := newTestBot(stubValidator{err: dicts.ErrUnavailable})

	b.handle(context.Background(), 10, "/subscribe city=Minsk")

	if len(subs.ByChat(10)) != 0 {
		t.Error("подписка не должна сохраняться при недоступных справочниках")
	}
	if !strings.Contains(api.last(), dicts.ErrUnavailable.Error()) {
		t.Errorf("ответ должен содержать понятное сообщение: %q", api.last())
	}
}

func TestFormatEventNewContract(t *testing.T) {
	address := "ул. Примерная, 1"
	e := model.Event{
		DealType: model.DealRent, City: "Minsk", Rooms: 1,
		Price: 1100, Currency: "BYN", PriceUSD: 350,
		Address: &address, URL: "https://example.com/123",
	}
	text := FormatEvent(e)

	for _, want := range []string{"Minsk", "1-к", "$350", "1100 BYN", address, e.URL} {
		if !strings.Contains(text, want) {
			t.Errorf("в уведомлении нет %q: %q", want, text)
		}
	}

	e.Rooms = 0
	e.Currency = "USD"
	e.Address = nil
	text = FormatEvent(e)
	if !strings.Contains(text, "студия") {
		t.Errorf("rooms=0 должен печататься как студия: %q", text)
	}
	if strings.Contains(text, "USD") || strings.Contains(text, "📍") {
		t.Errorf("USD-цена и пустой адрес не должны попадать в текст: %q", text)
	}
}
