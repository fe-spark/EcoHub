package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"server/internal/repobase"
	"server/internal/utils"
	"strconv"
	"strings"
	"time"
)

const (
	telegramAPIBase       = "https://api.telegram.org"
	telegramCallbackPref  = "nfp"
	telegramMaxMessageLen = 4096
	notifyInfoAllSources  = -2
)

// InlineKeyboardButton Telegram 内联键盘按钮
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

// InlineKeyboardMarkup Telegram 内联键盘布局
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type tgAPIResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
	Result      json.RawMessage `json:"result"`
}

type tgUpdate struct {
	UpdateID      int64            `json:"update_id"`
	CallbackQuery *tgCallbackQuery `json:"callback_query,omitempty"`
}

type tgCallbackQuery struct {
	ID      string     `json:"id"`
	Data    string     `json:"data"`
	Message *tgMessage `json:"message,omitempty"`
}

type tgMessage struct {
	MessageID       int64   `json:"message_id"`
	MessageThreadID int64   `json:"message_thread_id,omitempty"`
	Chat            *tgChat `json:"chat,omitempty"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

// tgAPIBaseURL 允许测试覆盖 Telegram API 地址
var tgAPIBaseURL = telegramAPIBase

func getTelegramHTTPClient(timeout time.Duration) (*http.Client, error) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ok, proxyURL, err := repobase.ResolveNotifyProxy()
	if err != nil {
		return nil, err
	}
	// 未启用代理时用共享直连 Transport（不读 HTTPS_PROXY 等环境变量），与 TMDB、版本检查一致。
	if !ok {
		proxyURL = ""
	}
	return &http.Client{
		Transport: utils.GetOrCreateProxyTransport(proxyURL),
		Timeout:   timeout,
	}, nil
}

func parseChatTarget(target string) (chatID string, threadID int64, err error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", 0, errors.New("空的 Chat ID")
	}
	if idx := strings.IndexByte(target, ':'); idx > 0 {
		cPart := strings.TrimSpace(target[:idx])
		tPart := strings.TrimSpace(target[idx+1:])
		if cPart == "" || tPart == "" {
			return "", 0, fmt.Errorf("无效的 Chat ID 格式: %s", target)
		}
		tid, parseErr := strconv.ParseInt(tPart, 10, 64)
		if parseErr != nil || tid <= 0 {
			return "", 0, fmt.Errorf("无效的话题 Thread ID: %s", target)
		}
		return cPart, tid, nil
	}
	return target, 0, nil
}

func callTelegramJSON(ctx context.Context, token, method string, payload any, timeout time.Duration) (tgAPIResponse, error) {
	var apiResp tgAPIResponse
	token = strings.TrimSpace(token)
	if token == "" {
		return apiResp, errors.New("Bot Token 不能为空")
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return apiResp, err
	}
	endpoint := fmt.Sprintf("%s/bot%s/%s", strings.TrimRight(tgAPIBaseURL, "/"), token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return apiResp, err
	}
	req.Header.Set("Content-Type", "application/json")

	client, err := getTelegramHTTPClient(timeout)
	if err != nil {
		return apiResp, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return apiResp, fmt.Errorf("请求 Telegram 失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return apiResp, fmt.Errorf("读取响应失败: %w", err)
	}
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return apiResp, fmt.Errorf("解析 Telegram 响应失败 (HTTP %d)", resp.StatusCode)
	}
	if !apiResp.OK {
		desc := strings.TrimSpace(apiResp.Description)
		if desc == "" {
			desc = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return apiResp, fmt.Errorf("Telegram 错误 (%d): %s", apiResp.ErrorCode, desc)
	}
	return apiResp, nil
}

func sendTelegramMessage(ctx context.Context, token, target, htmlText string, markup *InlineKeyboardMarkup) error {
	chatID, threadID, err := parseChatTarget(target)
	if err != nil {
		return err
	}
	htmlText = safeTruncateHTML(htmlText, telegramMaxMessageLen)
	body := map[string]any{
		"chat_id":                  chatID,
		"text":                     htmlText,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if threadID > 0 {
		body["message_thread_id"] = threadID
	}
	if markup != nil && len(markup.InlineKeyboard) > 0 {
		body["reply_markup"] = markup
	}
	_, err = callTelegramJSON(ctx, token, "sendMessage", body, 15*time.Second)
	return err
}

func sendTelegramReplyMessage(ctx context.Context, token, chatID string, threadID, replyToMsgID int64, htmlText string, markup *InlineKeyboardMarkup) error {
	htmlText = safeTruncateHTML(htmlText, telegramMaxMessageLen)
	body := map[string]any{
		"chat_id":                     chatID,
		"text":                        htmlText,
		"parse_mode":                  "HTML",
		"disable_web_page_preview":    true,
		"reply_to_message_id":         replyToMsgID,
		"allow_sending_without_reply": true,
	}
	if threadID > 0 {
		body["message_thread_id"] = threadID
	}
	if markup != nil && len(markup.InlineKeyboard) > 0 {
		body["reply_markup"] = markup
	}
	_, err := callTelegramJSON(ctx, token, "sendMessage", body, 15*time.Second)
	return err
}

func editTelegramMessage(ctx context.Context, token, chatID string, messageID int64, htmlText string, markup *InlineKeyboardMarkup) error {
	htmlText = safeTruncateHTML(htmlText, telegramMaxMessageLen)
	body := map[string]any{
		"chat_id":                  chatID,
		"message_id":               messageID,
		"text":                     htmlText,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if markup != nil && len(markup.InlineKeyboard) > 0 {
		body["reply_markup"] = markup
	}
	_, err := callTelegramJSON(ctx, token, "editMessageText", body, 10*time.Second)
	return err
}

func answerTelegramCallback(ctx context.Context, token, callbackID, text string, showAlert bool) error {
	body := map[string]any{
		"callback_query_id": callbackID,
		"show_alert":        showAlert,
	}
	if strings.TrimSpace(text) != "" {
		body["text"] = truncateRunes(strings.TrimSpace(text), 180)
	}
	_, err := callTelegramJSON(ctx, token, "answerCallbackQuery", body, 10*time.Second)
	return err
}
