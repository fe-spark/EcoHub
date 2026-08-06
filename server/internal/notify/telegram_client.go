package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const telegramAPIBase = "https://api.telegram.org"

type telegramClient struct {
	httpClient *http.Client
}

func newTelegramClient() *telegramClient {
	return &telegramClient{
		httpClient: &http.Client{Timeout: 35 * time.Second},
	}
}

// Inline keyboard types (Telegram Bot API).
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type telegramAPIResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

func (c *telegramClient) apiCall(ctx context.Context, token, method string, payload any) (json.RawMessage, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("bot token 为空")
	}
	var bodyReader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	url := fmt.Sprintf("%s/bot%s/%s", telegramAPIBase, token, method)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(400 * time.Millisecond):
			}
		}
		var rdr io.Reader = bodyReader
		if payload != nil {
			// re-marshal for retry (body may be consumed)
			raw, _ := json.Marshal(payload)
			rdr = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, rdr)
		if err != nil {
			return nil, err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("telegram rate limited: %s", strings.TrimSpace(string(raw)))
		}
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			continue
		}
		var apiResp telegramAPIResponse
		if err := json.Unmarshal(raw, &apiResp); err != nil {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil, nil
			}
			return nil, fmt.Errorf("telegram 响应解析失败: %w", err)
		}
		if !apiResp.OK {
			desc := strings.TrimSpace(apiResp.Description)
			if desc == "" {
				desc = strings.TrimSpace(string(raw))
			}
			return nil, fmt.Errorf("telegram api: %s", desc)
		}
		return apiResp.Result, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("telegram 请求失败")
}

func (c *telegramClient) sendMessage(ctx context.Context, token, chatID, text string) error {
	return c.sendMessageWithMarkup(ctx, token, chatID, text, nil)
}

func (c *telegramClient) sendMessageWithMarkup(ctx context.Context, token, chatID, text string, markup *InlineKeyboardMarkup) error {
	token = strings.TrimSpace(token)
	chatID = strings.TrimSpace(chatID)
	if token == "" || chatID == "" {
		return fmt.Errorf("bot token 或 chat id 为空")
	}
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if markup != nil && len(markup.InlineKeyboard) > 0 {
		payload["reply_markup"] = markup
	}
	_, err := c.apiCall(ctx, token, "sendMessage", payload)
	return err
}

func (c *telegramClient) editMessageText(ctx context.Context, token, chatID string, messageID int64, text string, markup *InlineKeyboardMarkup) error {
	payload := map[string]any{
		"chat_id":                  chatID,
		"message_id":               messageID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	_, err := c.apiCall(ctx, token, "editMessageText", payload)
	return err
}

func (c *telegramClient) answerCallbackQuery(ctx context.Context, token, callbackID, text string, showAlert bool) error {
	payload := map[string]any{
		"callback_query_id": callbackID,
	}
	if text != "" {
		payload["text"] = text
		payload["show_alert"] = showAlert
	}
	_, err := c.apiCall(ctx, token, "answerCallbackQuery", payload)
	return err
}

func (c *telegramClient) deleteWebhook(ctx context.Context, token string) error {
	_, err := c.apiCall(ctx, token, "deleteWebhook", map[string]any{
		"drop_pending_updates": false,
	})
	return err
}

type telegramUpdate struct {
	UpdateID      int64              `json:"update_id"`
	CallbackQuery *telegramCallback  `json:"callback_query"`
	Message       *telegramMessage   `json:"message"`
}

type telegramCallback struct {
	ID      string           `json:"id"`
	From    *telegramUser    `json:"from"`
	Message *telegramMessage `json:"message"`
	Data    string           `json:"data"`
}

type telegramMessage struct {
	MessageID int64  `json:"message_id"`
	Chat      *struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Text string `json:"text"`
}

type telegramUser struct {
	ID int64 `json:"id"`
}

func (c *telegramClient) getUpdates(ctx context.Context, token string, offset int64, timeoutSec int) ([]telegramUpdate, error) {
	// getUpdates uses long-poll; client timeout must exceed timeoutSec
	payload := map[string]any{
		"offset":  offset,
		"timeout": timeoutSec,
		"allowed_updates": []string{"callback_query"},
	}
	result, err := c.apiCall(ctx, token, "getUpdates", payload)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 || string(result) == "null" {
		return nil, nil
	}
	var updates []telegramUpdate
	if err := json.Unmarshal(result, &updates); err != nil {
		return nil, fmt.Errorf("getUpdates 解析失败: %w", err)
	}
	return updates, nil
}
