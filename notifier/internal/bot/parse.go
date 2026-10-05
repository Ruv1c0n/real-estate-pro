package bot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"notifier/internal/model"
)

// ParseSubscription разбирает аргументы команды /subscribe вида
//
//	city=Minsk rooms=1 max=400 deal=rent country=BY min=100
//
// Значения с пробелами работают без кавычек: city=New York max=500.
func ParseSubscription(args []string) (model.Subscription, error) {
	type pair struct{ key, val string }
	var pairs []pair
	for _, a := range args {
		if k, v, ok := strings.Cut(a, "="); ok {
			pairs = append(pairs, pair{strings.ToLower(k), v})
			continue
		}
		if len(pairs) == 0 {
			return model.Subscription{}, fmt.Errorf("ожидал key=value, а получил %q", a)
		}
		pairs[len(pairs)-1].val += " " + a
	}

	var s model.Subscription
	for _, p := range pairs {
		v := strings.TrimSpace(p.val)
		switch p.key {
		case "city":
			s.City = v
		case "country":
			if len(v) != 2 {
				return s, fmt.Errorf("country: нужен код из двух букв, например BY")
			}
			s.Country = strings.ToUpper(v)
		case "deal":
			switch strings.ToLower(v) {
			case "rent", "аренда":
				s.DealType = model.DealRent
			case "sale", "buy", "покупка":
				s.DealType = model.DealSale
			default:
				return s, fmt.Errorf("deal: допустимо rent или sale")
			}
		case "rooms":
			if strings.EqualFold(v, "studio") || strings.EqualFold(v, "студия") {
				s.Rooms = model.IntPtr(0)
				break
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return s, fmt.Errorf("rooms: нужно целое число >= 0 или studio")
			}
			s.Rooms = model.IntPtr(n)
		case "min", "max":
			f, err := strconv.ParseFloat(strings.TrimPrefix(v, "$"), 64)
			if err != nil || f <= 0 {
				return s, fmt.Errorf("%s: нужна положительная цена в USD", p.key)
			}
			if p.key == "min" {
				s.MinPriceUSD = model.FloatPtr(f)
			} else {
				s.MaxPriceUSD = model.FloatPtr(f)
			}
		default:
			return s, fmt.Errorf("неизвестный параметр %q", p.key)
		}
	}

	if s.City == "" && s.Country == "" && s.DealType == "" &&
		s.Rooms == nil && s.MinPriceUSD == nil && s.MaxPriceUSD == nil {
		return s, errors.New("укажите хотя бы один параметр")
	}
	if s.MinPriceUSD != nil && s.MaxPriceUSD != nil && *s.MinPriceUSD > *s.MaxPriceUSD {
		return s, errors.New("min больше max")
	}
	return s, nil
}