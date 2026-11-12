// Package bot — Telegram-бот: команды управления подписками и отправка уведомлений.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"notifier/internal/dicts"
	"notifier/internal/model"
	"notifier/internal/store"
	"notifier/internal/telegram"
)

// API — то, что боту нужно от Telegram-клиента (удобно подменять в тестах).
type API interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
	GetUpdates(ctx context.Context, offset int64) ([]telegram.Update, error)
}

// Validator проверяет страну и город подписки по справочникам Core API
// и приводит их к каноническим значениям (ФТ-3.1, contracts.md §3).
type Validator interface {
	Validate(ctx context.Context, country, city string) (canonCountry, canonCity string, err error)
}

// MaxSubscriptionsPerUser — лимит подписок на пользователя (ТЗ 3.6).
const MaxSubscriptionsPerUser = 10

// msgUnavailable — единственный ответ при недоступности хранилища или
// справочников: детали ошибки (хост/пользователь БД, URL core-api) в чат
// не утекают — они уходят в лог (L6).
const msgUnavailable = "Сервис временно недоступен, попробуйте позже."

type Bot struct {
	api   API
	subs  store.Store
	dicts Validator
}

func New(api API, subs store.Store, dicts Validator) *Bot {
	return &Bot{api: api, subs: subs, dicts: dicts}
}

const helpText = `Я присылаю новые объявления о недвижимости под ваши фильтры.

/start — зарегистрироваться и показать помощь
/subscribe city=Minsk rooms=1 max=400 deal=rent
  параметры (нужен хотя бы один):
  city — город (латиницей), country — код страны (BY),
  deal — rent или sale, rooms — число комнат или studio,
  min / max — цена в USD (целое)
/list — мои подписки
/unsubscribe <id> — удалить подписку
/stop — удалить меня и все мои подписки`

// Run крутит long polling до отмены контекста.
func (b *Bot) Run(ctx context.Context) {
	log.Println("telegram bot: polling started")
	var offset int64
	for ctx.Err() == nil {
		updates, err := b.api.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("telegram getUpdates: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message == nil || u.Message.Text == "" {
				continue
			}
			b.handle(ctx, u.Message.Chat.ID, u.Message.Text)
		}
	}
}

func (b *Bot) handle(ctx context.Context, chatID int64, text string) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return
	}
	cmd := strings.ToLower(fields[0])
	if i := strings.IndexByte(cmd, '@'); i > 0 { // /list@my_bot -> /list
		cmd = cmd[:i]
	}
	args := fields[1:]

	var reply string
	switch cmd {
	case "/start":
		reply = b.start(ctx, chatID)
	case "/help":
		reply = helpText
	case "/subscribe":
		reply = b.subscribe(ctx, chatID, args)
	case "/list":
		reply = b.list(ctx, chatID)
	case "/unsubscribe":
		reply = b.unsubscribe(ctx, chatID, args)
	case "/stop":
		reply = b.stop(ctx, chatID)
	default:
		if !strings.HasPrefix(cmd, "/") {
			return
		}
		reply = "Не знаю такую команду. /help — список команд."
	}
	b.send(ctx, chatID, reply)
}

func (b *Bot) subscribe(ctx context.Context, chatID int64, args []string) string {
	n, err := b.subs.CountForChat(ctx, chatID)
	if err != nil {
		log.Printf("bot: count subscriptions (chat %d): %v", chatID, err)
		return msgUnavailable
	}
	if n >= MaxSubscriptionsPerUser {
		return fmt.Sprintf("У вас уже %d подписок — это максимум. /list покажет ваши подписки, /unsubscribe удалит лишнюю.", MaxSubscriptionsPerUser)
	}
	s, err := ParseSubscription(args)
	if err != nil {
		return "Не получилось: " + err.Error() + "\nПример: /subscribe city=Minsk rooms=1 max=400"
	}
	s.Country, s.City, err = b.dicts.Validate(ctx, s.Country, s.City)
	if err != nil {
		// Недоступность справочников может нести детали (URL core-api) — в чат
		// идёт нейтральный текст (L6). Ошибки валидации («неизвестный город»)
		// — пользовательские, их показываем как есть.
		if errors.Is(err, dicts.ErrUnavailable) {
			log.Printf("bot: validate subscription (chat %d): %v", chatID, err)
			return msgUnavailable
		}
		return "Не получилось: " + err.Error()
	}
	s.ChatID = chatID
	s, err = b.subs.Add(ctx, s)
	if err != nil {
		log.Printf("bot: add subscription (chat %d): %v", chatID, err)
		return msgUnavailable
	}
	return fmt.Sprintf("Подписка #%d создана: %s", s.ID, Describe(s))
}

func (b *Bot) list(ctx context.Context, chatID int64) string {
	subs, err := b.subs.ByChat(ctx, chatID)
	if err != nil {
		log.Printf("bot: list subscriptions (chat %d): %v", chatID, err)
		return msgUnavailable
	}
	if len(subs) == 0 {
		return "Подписок пока нет. Создайте: /subscribe city=Minsk rooms=1 max=400"
	}
	var sb strings.Builder
	for _, s := range subs {
		fmt.Fprintf(&sb, "#%d: %s\n", s.ID, Describe(s))
	}
	return strings.TrimSpace(sb.String())
}

func (b *Bot) unsubscribe(ctx context.Context, chatID int64, args []string) string {
	if len(args) != 1 {
		return "Укажите номер: /unsubscribe 1"
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64)
	if err != nil {
		return "Номер подписки должен быть числом: /unsubscribe 1"
	}
	ok, err := b.subs.Delete(ctx, chatID, id)
	if err != nil {
		log.Printf("bot: delete subscription %d (chat %d): %v", id, chatID, err)
		return msgUnavailable
	}
	if !ok {
		return "Такой подписки нет."
	}
	return fmt.Sprintf("Подписка #%d удалена.", id)
}

// start регистрирует пользователя в notifier.users (/start, ФТ-3.1) и
// показывает помощь. Повторный /start идемпотентен.
func (b *Bot) start(ctx context.Context, chatID int64) string {
	if err := b.subs.EnsureUser(ctx, chatID); err != nil {
		log.Printf("bot: ensure user (chat %d): %v", chatID, err)
		return msgUnavailable
	}
	return helpText
}

// stop удаляет пользователя и все его данные (ФТ-3.1): в Postgres каскад
// чистит подписки и журнал; для ответа боту хранилище возвращает число подписок.
func (b *Bot) stop(ctx context.Context, chatID int64) string {
	n, err := b.subs.DeleteUser(ctx, chatID)
	if err != nil {
		log.Printf("bot: delete user (chat %d): %v", chatID, err)
		return msgUnavailable
	}
	return fmt.Sprintf("Готово: удалено подписок — %d. Вы больше не будете получать уведомления. /subscribe вернёт их в любой момент.", n)
}

func (b *Bot) send(ctx context.Context, chatID int64, text string) {
	if err := b.api.SendMessage(ctx, chatID, text); err != nil {
		log.Printf("telegram send to %d: %v", chatID, err)
	}
}

// Send доставляет уведомление об объявлении в чат. Используется диспетчером
// доставки как отправитель; контекст уже ограничен по времени снаружи.
func (b *Bot) Send(ctx context.Context, chatID int64, e model.Event) error {
	return b.api.SendMessage(ctx, chatID, FormatEvent(e))
}

// maxMessageLen — рабочий потолок длины сообщения (в рунах — символах, не
// байтах). Официальный лимит Telegram — 4096 символов, но вне-BMP-эмодзи
// (🏠 U+1F3E0, 📍 U+1F4CD в шапке уведомления) считаются в UTF-16 как две
// единицы; для офсетов entities Bot API документированы именно UTF-16
// единицы, поэтому оставляем запас в 2 единицы от формального лимита.
// Сообщение длиннее лимита SendMessage отклонит целиком, поэтому длина
// ограничивается при форматировании (см. FormatEvent).
const maxMessageLen = 4094

// ellipsis — признак усечённого поля в конце.
const ellipsis = "…"

// truncateRunes обрезает s до не более чем n рун.
func truncateRunes(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// FormatEvent — текст уведомления (ФТ-3.4: цена, адрес, комнатность, ссылка).
// Сообщение обязано влезать в лимит Telegram (maxMessageLen), иначе SendMessage
// отклонит его целиком: статус unknown без автоматического повтора —
// уведомление потеряно навсегда. Длинные поля усекаются только здесь,
// при форматировании: сначала адрес, в крайнем случае url — цена
// и комнатность не выпадают никогда (событие не отбрасывается, ТЗ 3.6).
func FormatEvent(e model.Event) string {
	addr := ""
	if e.Address != nil {
		addr = strings.TrimSpace(*e.Address)
	}
	stats := e.City
	if e.Rooms == 0 {
		stats += ", студия"
	} else {
		stats += fmt.Sprintf(", %d-к", e.Rooms)
	}
	stats += fmt.Sprintf(" · $%d", e.PriceUSD)
	if e.Currency != "" && !strings.EqualFold(e.Currency, "USD") && e.Price > 0 {
		stats += fmt.Sprintf(" (%.0f %s)", e.Price, e.Currency)
	}
	var addrLine string
	if addr != "" {
		addrLine = "📍 " + addr + "\n"
	}
	msg := "🏠 Новое объявление\n" + stats + "\n" + addrLine + e.URL
	if utf8.RuneCountInString(msg) <= maxMessageLen {
		return msg // обычное событие влезает — текст без изменений
	}

	head := "🏠 Новое объявление\n" + stats + "\n"

	// Сначала урезаем адрес: ссылка (ФТ-3.4) должна остаться целиком.
	// На строку адреса нужно минимум под «📍 …\n» — 4 руны.
	if addr != "" {
		if room := maxMessageLen - utf8.RuneCountInString(head) - utf8.RuneCountInString(e.URL); room >= 4 {
			return head + "📍 " + truncateRunes(addr, room-4) + ellipsis + "\n" + e.URL
		}
	}

	// Даже пустой адрес не спасает — не влезает url: усекаем его
	// (единственный случай, когда ссылка теряет хвост).
	if room := maxMessageLen - utf8.RuneCountInString(head); room >= 1 {
		return head + truncateRunes(e.URL, room-1) + ellipsis
	}

	// Теоретический случай: сама шапка длиннее лимита — гигантский city
	// из стрима. Урезаем город, цена и комнатность в конце строки остаются;
	// вместо url остаётся многоточие.
	if headLen := utf8.RuneCountInString(head); headLen > maxMessageLen {
		need := headLen - maxMessageLen + 1
		if cityLen := utf8.RuneCountInString(e.City); cityLen > need {
			city := truncateRunes(e.City, cityLen-need-1) + ellipsis
			return "🏠 Новое объявление\n" + strings.Replace(stats, e.City, city, 1) + "\n" + ellipsis
		}
	}
	// Безусловный последний ярус: инвариант «сообщение всегда ≤ лимита»
	// обязан держаться на любом входе (иначе SendMessage отклонит его —
	// unknown без повтора, уведомление потеряно целиком). Отдаём шапку,
	// усечённую до лимита.
	return truncateRunes(head, maxMessageLen-1) + ellipsis
}

// Describe — человекочитаемое описание фильтра подписки.
func Describe(s model.Subscription) string {
	var parts []string
	switch s.DealType {
	case model.DealRent:
		parts = append(parts, "аренда")
	case model.DealSale:
		parts = append(parts, "покупка")
	}
	if s.City != "" {
		parts = append(parts, s.City)
	}
	if s.Country != "" {
		parts = append(parts, s.Country)
	}
	if s.Rooms != nil {
		if *s.Rooms == 0 {
			parts = append(parts, "студия")
		} else {
			parts = append(parts, fmt.Sprintf("%d-к", *s.Rooms))
		}
	}
	if s.MinPriceUSD != nil {
		parts = append(parts, fmt.Sprintf("от $%d", *s.MinPriceUSD))
	}
	if s.MaxPriceUSD != nil {
		parts = append(parts, fmt.Sprintf("до $%d", *s.MaxPriceUSD))
	}
	return strings.Join(parts, ", ")
}
