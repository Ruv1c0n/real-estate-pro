// Package telegram — минимальный клиент Telegram Bot API поверх net/http.
// Внешние библиотеки не нужны: нам достаточно getUpdates и sendMessage.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ErrRateLimited — дозорная ошибка: Telegram ответил 429. Проверять через
// errors.Is; конкретную рекомендованную паузу несёт *RateLimitError
// (errors.As). Доставка возвращает задание в очередь отложенных вместо
// терминального unknown — см. notify.deliver.
var ErrRateLimited = errors.New("telegram: rate limited")

// RateLimitError — отказ 429 от Telegram: явный отказ с рекомендованной
// паузой, а не неопределённый исход (ТЗ 4.3: «Для 429 учитывается
// Retry-After»), поэтому доставку можно повторить через очередь отложенных.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("telegram: rate limited (retry after %s)", e.RetryAfter)
}

// Unwrap связывает ошибку с дозорной ErrRateLimited для errors.Is.
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// defaultRetryAfter — пауза по умолчанию, когда Telegram не прислал
// parameters.retry_after (поле ответа необязательное).
const defaultRetryAfter = 2 * time.Second

type Client struct {
	token string
	base  string
	http  *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token: token,
		base:  "https://api.telegram.org",
		// Больше, чем long polling timeout (30 с) в GetUpdates.
		http: &http.Client{Timeout: 70 * time.Second},
	}
}

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	Text string `json:"text"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
}

type apiResponse struct {
	OK          bool                   `json:"ok"`
	Result      json.RawMessage        `json:"result"`
	Description string                 `json:"description"`
	Parameters  *apiResponseParameters `json:"parameters"`
}

// apiResponseParameters — дополнительные параметры ошибки (Telegram Bot API):
// при 429 содержит retry_after в секундах.
type apiResponseParameters struct {
	RetryAfter int `json:"retry_after"`
}

// retryAfterOf извлекает parameters.retry_after (секунды); если поле
// отсутствует или нулевое — defaultRetryAfter.
func retryAfterOf(r *apiResponse) time.Duration {
	if r.Parameters != nil && r.Parameters.RetryAfter > 0 {
		return time.Duration(r.Parameters.RetryAfter) * time.Second
	}
	return defaultRetryAfter
}

func (c *Client) call(ctx context.Context, method string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/bot%s/%s", c.base, c.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram %s: build request", method)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error содержит полный URL вместе с токеном — в лог его пускать нельзя.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	defer resp.Body.Close()

	var r apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("telegram %s: bad response (HTTP %d)", method, resp.StatusCode)
	}
	// 429 — явный отказ с рекомендованной паузой (ТЗ 4.3): отдаём
	// типизированную ошибку, чтобы доставка вернула задание в очередь
	// отложенных, а не фиксировала терминальный unknown (contracts.md §2).
	// Сигнатура Sender не меняется: RateLimitError возвращается как error.
	if resp.StatusCode == http.StatusTooManyRequests {
		return &RateLimitError{RetryAfter: retryAfterOf(&r)}
	}
	if !r.OK {
		return fmt.Errorf("telegram %s: %s", method, r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

// SendMessage отправляет обычное текстовое сообщение.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	return c.call(ctx, "sendMessage", map[string]any{
		"chat_id": chatID,
		"text":    text,
	}, nil)
}

// GetUpdates — long polling: ждёт новые сообщения до 30 секунд.
func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	var updates []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         30,
		"allowed_updates": []string{"message"},
	}, &updates)
	return updates, err
}
