package model

import "strings"

// Subscription — сохранённый поиск пользователя.
// Пустая строка или nil означают «любое значение» для соответствующего поля.
//
// Пример: «1-к в Минске до $400» —
//
//	Subscription{City: "Minsk", Rooms: IntPtr(1), MaxPriceUSD: FloatPtr(400)}
type Subscription struct {
	ID          int64
	ChatID      int64    // Telegram chat_id, куда слать уведомление
	DealType    string   // "rent" / "sale" / "" (любой)
	Country     string   // ISO alpha-2 или ""
	City        string   // каноническое название или ""
	Rooms       *int     // точное количество комнат; 0 = студия
	MinPriceUSD *float64 // нижняя граница, включительно
	MaxPriceUSD *float64 // верхняя граница, включительно
}

// Matches сообщает, подходит ли событие под подписку.
func (s Subscription) Matches(e Event) bool {
	if s.DealType != "" && s.DealType != e.DealType {
		return false
	}
	if s.Country != "" && !sameText(s.Country, e.Country) {
		return false
	}
	if s.City != "" && !sameText(s.City, e.City) {
		return false
	}
	if s.Rooms != nil {
		// Если комнатность неизвестна, считаем, что не подходит:
		// лучше пропустить уведомление, чем прислать нерелевантное.
		if e.Rooms == nil || *e.Rooms != *s.Rooms {
			return false
		}
	}
	if s.MinPriceUSD != nil && e.PriceUSD < *s.MinPriceUSD {
		return false
	}
	if s.MaxPriceUSD != nil && e.PriceUSD > *s.MaxPriceUSD {
		return false
	}
	return true
}

// MatchAll возвращает подписки, подходящие под событие.
func MatchAll(e Event, subs []Subscription) []Subscription {
	var out []Subscription
	for _, s := range subs {
		if s.Matches(e) {
			out = append(out, s)
		}
	}
	return out
}

func sameText(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// IntPtr и FloatPtr упрощают создание опциональных полей.
func IntPtr(v int) *int           { return &v }
func FloatPtr(v float64) *float64 { return &v }