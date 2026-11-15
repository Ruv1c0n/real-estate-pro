package bot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

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

// recordingStore оборачивает реальную Memory и запоминает вызовы методов
// пользователя — чтобы тест мог проверить, что бот их вызывает.
type recordingStore struct {
	store.Store
	ensured  []int64
	deleted  []int64
	deletedN int
}

func (r *recordingStore) EnsureUser(_ context.Context, chatID int64) error {
	r.ensured = append(r.ensured, chatID)
	return r.Store.EnsureUser(context.Background(), chatID)
}

func (r *recordingStore) DeleteUser(_ context.Context, chatID int64) (int, error) {
	n, err := r.Store.DeleteUser(context.Background(), chatID)
	r.deleted = append(r.deleted, chatID)
	r.deletedN = n
	return n, err
}

// errStore — Store поверх Memory с управляемыми отказами: заданное поле —
// метод возвращает ошибку вместо похода в базу. Так тесты проверяют каждую
// команду бота при недоступном хранилище.
type errStore struct {
	store.Store
	countErr, addErr, byChatErr, deleteErr, ensureErr, delUserErr error
}

func (e *errStore) CountForChat(ctx context.Context, chatID int64) (int, error) {
	if e.countErr != nil {
		return 0, e.countErr
	}
	return e.Store.CountForChat(ctx, chatID)
}

func (e *errStore) Add(ctx context.Context, s model.Subscription) (model.Subscription, error) {
	if e.addErr != nil {
		return model.Subscription{}, e.addErr
	}
	return e.Store.Add(ctx, s)
}

func (e *errStore) ByChat(ctx context.Context, chatID int64) ([]model.Subscription, error) {
	if e.byChatErr != nil {
		return nil, e.byChatErr
	}
	return e.Store.ByChat(ctx, chatID)
}

func (e *errStore) Delete(ctx context.Context, chatID, id int64) (bool, error) {
	if e.deleteErr != nil {
		return false, e.deleteErr
	}
	return e.Store.Delete(ctx, chatID, id)
}

func (e *errStore) EnsureUser(ctx context.Context, chatID int64) error {
	if e.ensureErr != nil {
		return e.ensureErr
	}
	return e.Store.EnsureUser(ctx, chatID)
}

func (e *errStore) DeleteUser(ctx context.Context, chatID int64) (int, error) {
	if e.delUserErr != nil {
		return 0, e.delUserErr
	}
	return e.Store.DeleteUser(ctx, chatID)
}

// TestStoreErrorShowsNeutralText проверяет: при отказе хранилища в чат идёт
// только нейтральный текст, а детали ошибки (хост/пользователь БД) — в лог (L6).
func TestStoreErrorShowsNeutralText(t *testing.T) {
	api := &fakeAPI{}
	dbErr := errors.New("connect: host=secret-host user=secret-user dbname=secret-db")
	subs := &errStore{
		Store:      store.NewMemory(),
		countErr:   dbErr,
		addErr:     dbErr,
		byChatErr:  dbErr,
		deleteErr:  dbErr,
		ensureErr:  dbErr,
		delUserErr: dbErr,
	}
	b := New(api, subs, stubValidator{})

	// Перехватываем лог: детали ошибки должны попадать в лог, не в чат.
	var logBuf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(oldOut)

	for _, cmd := range []string{"/start", "/list", "/stop", "/subscribe city=Minsk", "/unsubscribe 1"} {
		logBuf.Reset()
		b.handle(context.Background(), 10, cmd)
		if got := api.last(); got != msgUnavailable {
			t.Errorf("%s: ответ %q, want нейтральный текст %q", cmd, got, msgUnavailable)
		}
		if !strings.Contains(logBuf.String(), "bot:") {
			t.Errorf("%s: в логе нет префикса источника bot:: %q", cmd, logBuf.String())
		}
		if !strings.Contains(logBuf.String(), "secret-host") {
			t.Errorf("%s: в логе нет деталей ошибки: %q", cmd, logBuf.String())
		}
	}
}

// TestSubscribeAddErrorShowsNeutralText: хранилище ломается на самой вставке
// (лимит и справочники прошли) — пользователь получает нейтральный текст,
// а не «Подписка #0 создана».
func TestSubscribeAddErrorShowsNeutralText(t *testing.T) {
	api := &fakeAPI{}
	subs := &errStore{Store: store.NewMemory(), addErr: errors.New("db down")}
	b := New(api, subs, stubValidator{country: "BY", city: "Minsk"})

	var logBuf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(oldOut)

	b.handle(context.Background(), 10, "/subscribe city=Minsk")

	if got := api.last(); got != msgUnavailable {
		t.Errorf("ответ %q, want нейтральный текст %q", got, msgUnavailable)
	}
	if !strings.Contains(logBuf.String(), "bot: add subscription") {
		t.Errorf("в логе нет записи о сбое вставки: %q", logBuf.String())
	}
}

func TestStartEnsuresUser(t *testing.T) {
	api := &fakeAPI{}
	rec := &recordingStore{Store: store.NewMemory()}
	b := New(api, rec, stubValidator{})

	b.handle(context.Background(), 10, "/start")

	if len(rec.ensured) != 1 || rec.ensured[0] != 10 {
		t.Errorf("EnsureUser вызван %v, want [10]", rec.ensured)
	}
	if !strings.Contains(api.last(), "/subscribe") {
		t.Errorf("/start должен показывать помощь: %q", api.last())
	}
	// Повторный /start — без ошибки в ответе.
	b.handle(context.Background(), 10, "/start")
	if strings.Contains(api.last(), "Не получилось") {
		t.Errorf("повторный /start не должен падать: %q", api.last())
	}
}

func TestStopDeletesUserAndReportsCount(t *testing.T) {
	api := &fakeAPI{}
	rec := &recordingStore{Store: store.NewMemory()}
	rec.Add(context.Background(), model.Subscription{ChatID: 10, City: "Minsk"})
	rec.Add(context.Background(), model.Subscription{ChatID: 10, City: "Brest"})
	b := New(api, rec, stubValidator{})

	b.handle(context.Background(), 10, "/stop")

	if len(rec.deleted) != 1 || rec.deleted[0] != 10 {
		t.Errorf("DeleteUser вызван %v, want [10]", rec.deleted)
	}
	if !strings.Contains(api.last(), "удалено подписок — 2") {
		t.Errorf("ответ /stop: %q", api.last())
	}
}

func TestStartSubscribeStopStartScenario(t *testing.T) {
	b, api, subs := newTestBot(stubValidator{})
	chat := int64(10)

	b.handle(context.Background(), chat, "/start")
	b.handle(context.Background(), chat, "/subscribe city=Minsk")
	if n, err := subs.CountForChat(context.Background(), chat); err != nil || n != 1 {
		t.Fatalf("после /subscribe подписок %d (%v), want 1", n, err)
	}

	b.handle(context.Background(), chat, "/stop")
	if n, err := subs.CountForChat(context.Background(), chat); err != nil || n != 0 {
		t.Fatalf("после /stop подписок %d (%v), want 0", n, err)
	}
	if !strings.Contains(api.last(), "удалено подписок — 1") {
		t.Errorf("ответ /stop: %q", api.last())
	}

	// После /stop пользователь может вернуться: /start + новая подписка.
	b.handle(context.Background(), chat, "/start")
	b.handle(context.Background(), chat, "/subscribe city=Brest")
	if !strings.Contains(api.last(), "Подписка #") {
		t.Errorf("повторная подписка после /stop не удалась: %q", api.last())
	}
	if n, err := subs.CountForChat(context.Background(), chat); err != nil || n != 1 {
		t.Errorf("после повторного цикла подписок %d (%v), want 1", n, err)
	}
}

func TestStopRemovesAllSubscriptions(t *testing.T) {
	b, api, subs := newTestBot(stubValidator{})
	chat := int64(10)
	subs.Add(context.Background(), model.Subscription{ChatID: chat, City: "Minsk"})
	subs.Add(context.Background(), model.Subscription{ChatID: chat, City: "Brest"})
	subs.Add(context.Background(), model.Subscription{ChatID: 20, City: "Minsk"})

	b.handle(context.Background(), chat, "/stop")

	if n, err := subs.CountForChat(context.Background(), chat); err != nil || n != 0 {
		t.Errorf("подписок чата осталось %d (%v), want 0", n, err)
	}
	if n, err := subs.CountForChat(context.Background(), 20); err != nil || n != 1 {
		t.Errorf("чужие подписки задеты: %d (%v), want 1", n, err)
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

	got, err := subs.ByChat(context.Background(), 10)
	if err != nil {
		t.Fatalf("ByChat: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("подписок %d, want 1", len(got))
	}
	if got[0].Country != "BY" || got[0].City != "Minsk" {
		t.Errorf("канонизация не применена: %+v", got[0])
	}
}

func TestSubscribeRejectsWhenDictsUnavailable(t *testing.T) {
	b, api, subs := newTestBot(stubValidator{err: dicts.ErrUnavailable})

	var logBuf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(oldOut)

	b.handle(context.Background(), 10, "/subscribe city=Minsk")

	if n, err := subs.ByChat(context.Background(), 10); err != nil || len(n) != 0 {
		t.Errorf("подписка не должна сохраняться при недоступных справочниках: %d (%v)", len(n), err)
	}
	if api.last() != msgUnavailable {
		t.Errorf("ответ должен быть нейтральным текстом без деталей: %q", api.last())
	}
	if !strings.Contains(logBuf.String(), "bot: validate subscription") {
		t.Errorf("детали недоступности справочников должны попасть в лог: %q", logBuf.String())
	}
}

func TestSubscribeUnknownCityShowsValidationText(t *testing.T) {
	// Ошибка валидации («неизвестный город») — не сбой сервиса: её показываем
	// пользователю как есть, нейтральный текст тут был бы дезинформацией.
	b, api, subs := newTestBot(stubValidator{err: errors.New(`неизвестный город "Podolsk"`)})

	b.handle(context.Background(), 10, "/subscribe city=Podolsk")

	if n, err := subs.ByChat(context.Background(), 10); err != nil || len(n) != 0 {
		t.Errorf("подписка не должна сохраняться: %d (%v)", len(n), err)
	}
	if !strings.Contains(api.last(), "неизвестный город") || api.last() == msgUnavailable {
		t.Errorf("текст валидации должен попасть в чат: %q", api.last())
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

func TestFormatEventShortEventUnchanged(t *testing.T) {
	// Обычное событие влезает в лимит — текст байт-в-байт прежний
	// (усечение не должно менять поведение коротких сообщений).
	address := "ул. Примерная, 1"
	e := model.Event{
		DealType: model.DealRent, City: "Minsk", Rooms: 1,
		Price: 1100, Currency: "BYN", PriceUSD: 350,
		Address: &address, URL: "https://example.com/123",
	}
	want := "🏠 Новое объявление\n" +
		"Minsk, 1-к · $350 (1100 BYN)\n" +
		"📍 ул. Примерная, 1\n" +
		"https://example.com/123"
	if got := FormatEvent(e); got != want {
		t.Errorf("FormatEvent =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatEventNilAddressUnchanged(t *testing.T) {
	// address=nil — как раньше: строки адреса нет вовсе.
	e := model.Event{
		DealType: model.DealSale, City: "Brest", Rooms: 0,
		PriceUSD: 50000, URL: "https://example.com/42",
	}
	want := "🏠 Новое объявление\n" +
		"Brest, студия · $50000\n" +
		"https://example.com/42"
	if got := FormatEvent(e); got != want {
		t.Errorf("FormatEvent =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatEventTruncatesLongAddress(t *testing.T) {
	// Адрес в ~5 КБ рун (10 КБ байт): сообщение обязано влезть в лимит
	// Telegram (по рунам!), цена/комнатность/ссылка не выпадают (ФТ-3.4).
	address := strings.Repeat("а", 5000) // кириллица: 2 байта на руну
	e := model.Event{
		DealType: model.DealRent, City: "Minsk", Rooms: 2,
		Price: 1100, Currency: "BYN", PriceUSD: 350,
		Address: &address, URL: "https://example.com/123",
	}
	text := FormatEvent(e)

	if n := utf8.RuneCountInString(text); n > maxMessageLen {
		t.Fatalf("длина сообщения %d рун > %d — SendMessage отклонит его", n, maxMessageLen)
	}
	for _, want := range []string{"Minsk", "2-к", "$350", "1100 BYN", "https://example.com/123", "…"} {
		if !strings.Contains(text, want) {
			t.Errorf("в уведомлении нет %q: %q", want, text)
		}
	}
	if strings.Contains(text, address) {
		t.Error("гигантский адрес должен быть усечён")
	}
	// Лимит считается рунами: при побайтовом усечении адреса осталось бы
	// вдвое меньше рун, и хвоста в 3000 рун не было бы.
	if !strings.Contains(text, strings.Repeat("а", 3000)) {
		t.Error("усечённый адрес должен сохранить большую часть рун")
	}
}

func TestFormatEventTruncatesGiantURL(t *testing.T) {
	// Гигантский url: даже без адреса сообщение не влезает — усекается сам
	// url с «…» (единственный случай, когда ссылка теряет хвост).
	url := "https://example.com/" + strings.Repeat("p", 4500)
	address := "ул. Примерная, 1"
	e := model.Event{
		DealType: model.DealRent, City: "Minsk", Rooms: 1,
		PriceUSD: 350, Address: &address, URL: url,
	}
	text := FormatEvent(e)

	if n := utf8.RuneCountInString(text); n > maxMessageLen {
		t.Fatalf("длина сообщения %d рун > %d — SendMessage отклонит его", n, maxMessageLen)
	}
	for _, want := range []string{"Minsk", "1-к", "$350", "https://example.com/"} {
		if !strings.Contains(text, want) {
			t.Errorf("в уведомлении нет %q: %q", want, text)
		}
	}
	if !strings.HasSuffix(text, "…") {
		t.Errorf("сообщение должно заканчиваться многоточием усечённого url: %q", text)
	}
	if strings.Contains(text, url) {
		t.Error("гигантский url должен быть усечён")
	}
}

// TestFormatEventGiantCurrencyUnconditionalTier — безусловный последний
// ярус усечения: гигантское не-адресное поле (currency из стрима) раздувает
// шапку сверх лимита, город усечь нельзя — итог гарантированно вписывается
// в лимит (повторный гейт R8: ветка не была покрыта тестом).
func TestFormatEventGiantCurrencyUnconditionalTier(t *testing.T) {
	e := model.Event{
		DealType: model.DealRent, City: "Minsk", Rooms: 1,
		PriceUSD: 350, Price: 380, Currency: strings.Repeat("R", 5000),
		URL: "https://example.com/1",
	}
	text := FormatEvent(e)

	if n := utf8.RuneCountInString(text); n > maxMessageLen {
		t.Fatalf("длина сообщения %d рун > %d — инвариант «всегда ≤ лимита» сломан", n, maxMessageLen)
	}
	for _, want := range []string{"Minsk", "1-к", "$350"} {
		if !strings.Contains(text, want) {
			t.Errorf("в уведомлении нет %q: %q", want, text)
		}
	}
	if !strings.HasSuffix(text, "…") {
		t.Errorf("сообщение должно заканчиваться многоточием последнего яруса: %q", text)
	}
}
