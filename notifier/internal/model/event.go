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

// Event — сообщение из канала Redis `listings.new`. Формат описан в docs/contracts.md.
type Event struct {
	ID        int64     `json:"id"`
	Source    string    `json:"source"`
	DealType  string    `json:"deal_type"`
	Country   string    `json:"country"`
	City      string    `json:"city"`
	Rooms     *int      `json:"rooms"` // nil = неизвестно, 0 = студия
	Price     float64   `json:"price"`
	Currency  string    `json:"currency"`
	PriceUSD  float64   `json:"price_usd"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

// ParseEvent разбирает JSON и проверяет обязательные поля.
func ParseEvent(data []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(data, &e); err != nil {
		return Event{}, fmt.Errorf("decode event: %w", err)
	}
	if err := e.Validate(); err != nil {
		return Event{}, err
	}
	return e, nil
}

// Validate проверяет, что событие соответствует контракту.
func (e Event) Validate() error {
	var errs []error
	if e.ID <= 0 {
		errs = append(errs, errors.New("id must be positive"))
	}
	if e.DealType != DealRent && e.DealType != DealSale {
		errs = append(errs, fmt.Errorf("deal_type must be %q or %q, got %q", DealRent, DealSale, e.DealType))
	}
	if strings.TrimSpace(e.City) == "" {
		errs = append(errs, errors.New("city is required"))
	}
	if strings.TrimSpace(e.Country) == "" {
		errs = append(errs, errors.New("country is required"))
	}
	if e.PriceUSD <= 0 {
		errs = append(errs, errors.New("price_usd must be > 0"))
	}
	if strings.TrimSpace(e.URL) == "" {
		errs = append(errs, errors.New("url is required"))
	}
	if e.Rooms != nil && *e.Rooms < 0 {
		errs = append(errs, errors.New("rooms must be >= 0"))
	}
	return errors.Join(errs...)
}