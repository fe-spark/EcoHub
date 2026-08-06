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
		httpClient: &http.Client{Timeout: 8 * time.Second},
	}
}

type sendMessageRequest struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

type telegramAPIResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

func (c *telegramClient) sendMessage(ctx context.Context, token, chatID, text string) error {
	token = strings.TrimSpace(token)
	chatID = strings.TrimSpace(chatID)
	if token == "" || chatID == "" {
		return fmt.Errorf("bot token 或 chat id 为空")
	}
	body, err := json.Marshal(sendMessageRequest{
		ChatID:                chatID,
		Text:                  text,
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
	})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", telegramAPIBase, token)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(400 * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			return fmt.Errorf("telegram rate limited: %s", strings.TrimSpace(string(raw)))
		}
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			continue
		}
		var apiResp telegramAPIResponse
		if err := json.Unmarshal(raw, &apiResp); err != nil {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			return fmt.Errorf("telegram 响应解析失败: %w", err)
		}
		if !apiResp.OK {
			desc := strings.TrimSpace(apiResp.Description)
			if desc == "" {
				desc = strings.TrimSpace(string(raw))
			}
			return fmt.Errorf("telegram api: %s", desc)
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("telegram 发送失败")
}
