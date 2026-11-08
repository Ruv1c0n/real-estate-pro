package model

import "strings"

// Subscription — сохранённый поиск пользователя.
// Пустая строка или nil означают «любое значение» для соответствующего поля.
//
// Пример: «1-к в Минске до $400» —
//
//	Subscription{City: "Minsk", Rooms: IntPtr(1), MaxPriceUSD: Int64Ptr(400)}
type Subscription struct {
	ID          int64
	ChatID      int64  // Telegram chat_id, куда слать уведомление
	DealType    string // "rent" / "sale" / "" (любой)
	Country     string // ISO alpha-2 или ""
	City        string // каноническое название или ""
	Rooms       *int   // точное количество комнат; 0 = студия
	MinPriceUSD *int64 // нижняя граница, включительно
	MaxPriceUSD *int64 // верхняя граница, включительно
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
	if s.Rooms != nil && *s.Rooms != e.Rooms {
		return false
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

// Recipients возвращает уникальные chat_id пользователей, чьи подписки
// совпали с событием: несколько совпавших подписок одного пользователя
// дают одно уведомление (ФТ-3.4/3.5 ТЗ).
func Recipients(e Event, subs []Subscription) []int64 {
	seen := make(map[int64]struct{}, len(subs))
	var out []int64
	for _, s := range subs {
		if _, ok := seen[s.ChatID]; ok {
			continue
		}
		if s.Matches(e) {
			seen[s.ChatID] = struct{}{}
			out = append(out, s.ChatID)
		}
	}
	return out
}

func sameText(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// IntPtr и Int64Ptr упрощают создание опциональных полей.
func IntPtr(v int) *int       { return &v }
func Int64Ptr(v int64) *int64 { return &v }
