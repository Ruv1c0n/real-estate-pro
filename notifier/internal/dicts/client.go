// Package dicts обращается к справочникам Core API (contracts.md §3) и
// кэширует их: при недоступности Core API подписки проверяются по кэшу,
// без кэша — отклоняются с понятным сообщением (ФТ-3.1).
package dicts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrUnavailable — Core API недоступен, а кэша ещё нет.
var ErrUnavailable = errors.New("справочники недоступны, попробуйте позже")

type Country struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type City struct {
	Country string   `json:"country"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

// Client читает справочники Core API с кэшем (TTL 10 минут, как кэш списков
// в ТЗ 3.5). Успешно полученный справочник остаётся запасом на случай,
// если Core API ляжет: устаревший кэш лучше отказа.
//
// Блокировки: mu защищает только данные кэша и держится коротко — читатели
// свежего кэша не ждут сетевой фетч; fetchMu сериализует сами фетчи
// (single-flight): параллельные запросы при просроченном кэше ждут один
// сетевой вызов, а не дублируют его, после ожидания повторно проверяют кэш.
type Client struct {
	base string
	http *http.Client
	ttl  time.Duration

	mu        sync.Mutex
	countries []Country
	cities    map[string][]City
	fetched   map[string]time.Time

	fetchMu sync.Mutex
}

func New(baseURL string) *Client {
	return &Client{
		base:    strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 5 * time.Second}, // НФТ-9
		ttl:     10 * time.Minute,
		cities:  make(map[string][]City),
		fetched: make(map[string]time.Time),
	}
}

// Validate проверяет пару (страна, город) по справочникам и возвращает
// канонические значения: код страны в верхнем регистре и каноническое
// название города. Пустые входы означают «не задано» и возвращаются как есть.
// Город без страны ищется по всем странам; совпадение в нескольких странах
// неоднозначно и отклоняется (contracts.md: «неоднозначные значения
// отклоняются»).
func (c *Client) Validate(ctx context.Context, country, city string) (string, string, error) {
	var canonCountry string
	if country != "" {
		countries, err := c.Countries(ctx)
		if err != nil {
			return "", "", err
		}
		for _, c2 := range countries {
			if strings.EqualFold(c2.Code, country) {
				canonCountry = c2.Code
				break
			}
		}
		if canonCountry == "" {
			return "", "", fmt.Errorf("неизвестная страна %q — справочник: /help", country)
		}
	}
	if city == "" {
		return canonCountry, "", nil
	}

	candidates := []string{canonCountry}
	if canonCountry == "" {
		list, err := c.Countries(ctx)
		if err != nil {
			return "", "", err
		}
		candidates = candidates[:0]
		for _, c2 := range list {
			candidates = append(candidates, c2.Code)
		}
	}

	foundCountry, foundCity := "", ""
	ambiguous := false
	for _, code := range candidates {
		cities, err := c.Cities(ctx, code)
		if err != nil {
			return "", "", err
		}
		for _, ct := range cities {
			if !matchCity(ct, city) {
				continue
			}
			if foundCity != "" && ct.Name != foundCity {
				ambiguous = true
				continue
			}
			foundCountry, foundCity = code, ct.Name
		}
	}
	if ambiguous {
		return "", "", fmt.Errorf("город %q есть в нескольких странах — укажите country=XX", city)
	}
	if foundCity == "" {
		if canonCountry != "" {
			return "", "", fmt.Errorf("город %q не найден в стране %s", city, canonCountry)
		}
		return "", "", fmt.Errorf("неизвестный город %q", city)
	}
	return foundCountry, foundCity, nil
}

// matchCity сравнивает название города с каноном или алиасом.
func matchCity(c City, name string) bool {
	if strings.EqualFold(strings.TrimSpace(c.Name), strings.TrimSpace(name)) {
		return true
	}
	for _, a := range c.Aliases {
		if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// Countries возвращает справочник стран: из кэша, из Core API или из
// устаревшего кэша, если Core API недоступен.
func (c *Client) Countries(ctx context.Context) ([]Country, error) {
	if out, ok := c.freshCountries(); ok {
		return out, nil
	}
	// Фетч без удержания mu: читатели свежего кэша не блокируются, вторичные
	// горуты ждут только завершения фетча (см. fetchMu у Client).
	c.fetchMu.Lock()
	defer c.fetchMu.Unlock()
	// Двойная проверка: фетч мог уже выполниться параллельной горутой.
	if out, ok := c.freshCountries(); ok {
		return out, nil
	}
	var out []Country
	if err := c.get(ctx, "/api/v1/countries", &out); err != nil {
		if stale := c.staleCountries(); stale != nil {
			return stale, nil // устаревший кэш лучше отказа
		}
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	c.setCountries(out)
	return out, nil
}

// Cities возвращает справочник городов страны.
func (c *Client) Cities(ctx context.Context, country string) ([]City, error) {
	key := strings.ToUpper(country)
	if out, ok := c.freshCities(key); ok {
		return out, nil
	}
	// Фетч без удержания mu: см. Countries.
	c.fetchMu.Lock()
	defer c.fetchMu.Unlock()
	if out, ok := c.freshCities(key); ok {
		return out, nil // фетч уже выполнен параллельной горутой
	}
	var out []City
	if err := c.get(ctx, "/api/v1/cities?country="+key, &out); err != nil {
		if stale := c.staleCities(key); stale != nil {
			return stale, nil // устаревший кэш лучше отказа
		}
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	c.setCities(key, out)
	return out, nil
}

// Healthy сообщает, доступны ли справочники (для readiness /health/ready,
// ФТ-3.7): выполняет сетевой вызов при просроченном кэше; свежий или
// устаревший кэш считается доступностью.
func (c *Client) Healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := c.Countries(ctx)
	return err == nil
}

// CachedHealthy сообщает, есть ли у клиента СВЕЖИЙ (в пределах TTL) кэш
// справочников, без сетевого вызова к Core API. Используется liveness
// /health: liveness не должен ходить по сети, чтобы шторм healthcheck'ов
// не усиливал нагрузку на Core API. В отличие от Healthy (истина по сетевой
// проверке, включая устаревший кэш), здесь истина означает «данные кэша
// актуальны прямо сейчас».
func (c *Client) CachedHealthy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return fresh(c.fetched["countries"], c.ttl)
}

func (c *Client) freshCountries() ([]Country, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if fresh(c.fetched["countries"], c.ttl) {
		return c.countries, true
	}
	return nil, false
}

func (c *Client) staleCountries() []Country {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.countries) > 0 {
		return c.countries
	}
	return nil
}

func (c *Client) setCountries(out []Country) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.countries = out
	c.fetched["countries"] = time.Now()
}

func (c *Client) freshCities(key string) ([]City, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if fresh(c.fetched["cities:"+key], c.ttl) {
		return c.cities[key], true
	}
	return nil, false
}

func (c *Client) staleCities(key string) []City {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cities[key]) > 0 {
		return c.cities[key]
	}
	return nil
}

func (c *Client) setCities(key string, out []City) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cities[key] = out
	c.fetched["cities:"+key] = time.Now()
}

func fresh(fetchedAt time.Time, ttl time.Duration) bool {
	return !fetchedAt.IsZero() && time.Since(fetchedAt) < ttl
}

// get выполняет GET-запрос к Core API и декодирует JSON-ответ.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("core-api %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
