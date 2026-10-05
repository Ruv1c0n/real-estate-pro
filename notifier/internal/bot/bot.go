// Package bot — Telegram-бот: команды управления подписками и отправка уведомлений.
package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"notifier/internal/model"
	"notifier/internal/store"
	"notifier/internal/telegram"
)

// API — то, что боту нужно от Telegram-клиента (удобно подменять в тестах).
type API interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
	GetUpdates(ctx context.Context, offset int64) ([]telegram.Update, error)
}

type Bot struct {
	api  API
	subs *store.Memory
}

func New(api API, subs *store.Memory) *Bot {
	return &Bot{api: api, subs: subs}
}

const helpText = `Я присылаю новые объявления о недвижимости под ваши фильтры.

/subscribe city=Minsk rooms=1 max=400 deal=rent
  параметры (нужен хотя бы один):
  city — город (латиницей), country — код страны (BY),
  deal — rent или sale, rooms — число комнат или studio,
  min / max — цена в USD
/list — мои подписки
/unsubscribe <id> — удалить подписку`

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
	case "/start", "/help":
		reply = helpText
	case "/subscribe":
		reply = b.subscribe(chatID, args)
	case "/list":
		reply = b.list(chatID)
	case "/unsubscribe":
		reply = b.unsubscribe(chatID, args)
	default:
		if !strings.HasPrefix(cmd, "/") {
			return
		}
		reply = "Не знаю такую команду. /help — список команд."
	}
	b.send(ctx, chatID, reply)
}

func (b *Bot) subscribe(chatID int64, args []string) string {
	s, err := ParseSubscription(args)
	if err != nil {
		return "Не получилось: " + err.Error() + "\nПример: /subscribe city=Minsk rooms=1 max=400"
	}
	s.ChatID = chatID
	s = b.subs.Add(s)
	return fmt.Sprintf("Подписка #%d создана: %s", s.ID, Describe(s))
}

func (b *Bot) list(chatID int64) string {
	subs := b.subs.ByChat(chatID)
	if len(subs) == 0 {
		return "Подписок пока нет. Создайте: /subscribe city=Minsk rooms=1 max=400"
	}
	var sb strings.Builder
	for _, s := range subs {
		fmt.Fprintf(&sb, "#%d: %s\n", s.ID, Describe(s))
	}
	return strings.TrimSpace(sb.String())
}

func (b *Bot) unsubscribe(chatID int64, args []string) string {
	if len(args) != 1 {
		return "Укажите номер: /unsubscribe 1"
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64)
	if err != nil {
		return "Номер подписки должен быть числом: /unsubscribe 1"
	}
	if !b.subs.Delete(chatID, id) {
		return "Такой подписки нет."
	}
	return fmt.Sprintf("Подписка #%d удалена.", id)
}

func (b *Bot) send(ctx context.Context, chatID int64, text string) {
	if err := b.api.SendMessage(ctx, chatID, text); err != nil {
		log.Printf("telegram send to %d: %v", chatID, err)
	}
}

// Notify отправляет уведомление об объявлении в чат.
func (b *Bot) Notify(ctx context.Context, chatID int64, e model.Event) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b.send(ctx, chatID, FormatEvent(e))
}

// FormatEvent — текст уведомления.
func FormatEvent(e model.Event) string {
	var sb strings.Builder
	sb.WriteString("🏠 Новое объявление\n")
	if e.Title != "" {
		sb.WriteString(e.Title + "\n")
	}
	line := e.City
	if e.Rooms != nil {
		if *e.Rooms == 0 {
			line += ", студия"
		} else {
			line += fmt.Sprintf(", %d-к", *e.Rooms)
		}
	}
	line += fmt.Sprintf(" · $%.0f", e.PriceUSD)
	if e.Currency != "" && !strings.EqualFold(e.Currency, "USD") && e.Price > 0 {
		line += fmt.Sprintf(" (%.0f %s)", e.Price, e.Currency)
	}
	sb.WriteString(line + "\n")
	sb.WriteString(e.URL)
	return sb.String()
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
		parts = append(parts, fmt.Sprintf("от $%.0f", *s.MinPriceUSD))
	}
	if s.MaxPriceUSD != nil {
		parts = append(parts, fmt.Sprintf("до $%.0f", *s.MaxPriceUSD))
	}
	return strings.Join(parts, ", ")
}