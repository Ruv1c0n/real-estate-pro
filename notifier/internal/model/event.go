// Package model содержит доменные типы Notifier: событие о новом объявлении,
// подписку пользователя и правила их сопоставления.
package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DealRent = "rent"
	DealSale = "sale"
)

// Event — событие из стрима Redis `listings.new`: поле `data` записи
// содержит JSON этого вида. Формат описан в docs/contracts.md §2.
type Event struct {
	EventID   string    // UUID outbox-задания; не меняется при повторной публикации
	ID        int64     // id объявления в Postgres Core API
	Source    string    // код источника: avito, cian, mock, ...
	DealType  string    // rent / sale
	Country   string    // ISO 3166-1 alpha-2: BY, RU, PL
	City      string    // каноническое название: Minsk, Moscow
	Rooms     int       // обязательно, >= 0; 0 = студия; null запрещён контрактом
	Price     float64   // цена в исходной валюте
	Currency  string    // ISO 4217
	PriceUSD  int64     // округлённая цена в USD
	Address   *string   // может быть null
	URL       string    // ссылка на первоисточник
	CreatedAt time.Time // ISO 8601 UTC
}

// eventJSON — теневая структура: указатели отличают отсутствующий ключ
// от нулевого значения (контракт требует наличия всех ключей).
type eventJSON struct {
	EventID   *string    `json:"event_id"`
	ID        *int64     `json:"id"`
	Source    *string    `json:"source"`
	DealType  *string    `json:"deal_type"`
	Country   *string    `json:"country"`
	City      *string    `json:"city"`
	Rooms     *int       `json:"rooms"`
	Price     *float64   `json:"price"`
	Currency  *string    `json:"currency"`
	PriceUSD  *int64     `json:"price_usd"`
	Address   *string    `json:"address"`
	URL       *string    `json:"url"`
	CreatedAt *time.Time `json:"created_at"`
}

// ParseEvent разбирает JSON из поля `data` и проверяет обязательные поля.
func ParseEvent(data []byte) (Event, error) {
	var j eventJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return Event{}, fmt.Errorf("decode event: %w", err)
	}
	// rooms: 0 = студия — легальное значение, поэтому присутствие ключа
	// проверяем по указателю, а не по нулевому значению.
	if j.Rooms == nil {
		return Event{}, errors.New("rooms: поле обязательно, null запрещён")
	}
	e := Event{
		EventID:  strOrEmpty(j.EventID),
		ID:       int64OrZero(j.ID),
		Source:   strOrEmpty(j.Source),
		DealType: strOrEmpty(j.DealType),
		Country:  strOrEmpty(j.Country),
		City:     strOrEmpty(j.City),
		Rooms:    *j.Rooms,
		Address:  j.Address,
		URL:      strOrEmpty(j.URL),
	}
	if j.Price != nil {
		e.Price = *j.Price
	}
	if j.Currency != nil {
		e.Currency = *j.Currency
	}
	if j.PriceUSD != nil {
		e.PriceUSD = *j.PriceUSD
	}
	if j.CreatedAt != nil {
		e.CreatedAt = *j.CreatedAt
	}
	if err := e.Validate(); err != nil {
		return Event{}, err
	}
	return e, nil
}

// Validate проверяет, что событие соответствует контракту.
func (e Event) Validate() error {
	var errs []error
	if !validUUID(e.EventID) {
		errs = append(errs, errors.New("event_id must be a UUID"))
	}
	if e.ID <= 0 {
		errs = append(errs, errors.New("id must be positive"))
	}
	if strings.TrimSpace(e.Source) == "" {
		errs = append(errs, errors.New("source is required"))
	}
	if e.DealType != DealRent && e.DealType != DealSale {
		errs = append(errs, fmt.Errorf("deal_type must be %q or %q, got %q", DealRent, DealSale, e.DealType))
	}
	if !isAlphaUpper2(e.Country) {
		errs = append(errs, fmt.Errorf("country must be ISO alpha-2 code like BY, got %q", e.Country))
	}
	if strings.TrimSpace(e.City) == "" {
		errs = append(errs, errors.New("city is required"))
	}
	if e.Rooms < 0 {
		errs = append(errs, errors.New("rooms must be >= 0"))
	}
	if e.Price <= 0 {
		errs = append(errs, errors.New("price must be > 0"))
	}
	if strings.TrimSpace(e.Currency) == "" {
		errs = append(errs, errors.New("currency is required"))
	}
	if e.PriceUSD <= 0 {
		errs = append(errs, errors.New("price_usd must be > 0"))
	}
	if !strings.HasPrefix(e.URL, "http://") && !strings.HasPrefix(e.URL, "https://") {
		errs = append(errs, fmt.Errorf("url must be an absolute http(s) link, got %q", e.URL))
	}
	if e.CreatedAt.IsZero() {
		errs = append(errs, errors.New("created_at is required"))
	}
	return errors.Join(errs...)
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func int64OrZero(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// validUUID проверяет форму UUID 8-4-4-4-12 без внешних зависимостей.
func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHexDigit(c) {
				return false
			}
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isAlphaUpper2(s string) bool {
	if len(s) != 2 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'A' && s[1] <= 'Z'
}
