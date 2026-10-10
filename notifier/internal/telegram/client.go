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
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
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