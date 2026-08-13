package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

const telegramAPIBase = "https://api.telegram.org"

// botTokenURLPattern 匹配错误信息中的 /bot<token>/，避免把 Token 回传前端。
var botTokenURLPattern = regexp.MustCompile(`(?i)/bot[0-9]{5,}:[A-Za-z0-9_-]+/`)

type telegramClient struct {
	httpClient *http.Client
	proxyURL   string // 脱敏展示用，仅 host:port
}

func newTelegramClient() *telegramClient {
	transport, proxyLabel, err := buildTelegramTransport()
	if err != nil {
		// 通知可选：代理配置错误不拖垮整进程，回退直连并打日志
		log.Printf("[Notify] Telegram 代理配置无效，回退直连: %v", err)
		transport = defaultTelegramTransport()
		proxyLabel = ""
	}
	if proxyLabel != "" {
		log.Printf("[Notify] Telegram API 使用代理: %s", proxyLabel)
	}
	return &telegramClient{
		httpClient: &http.Client{
			Timeout:   45 * time.Second,
			Transport: transport,
		},
		proxyURL: proxyLabel,
	}
}

func defaultTelegramTransport() *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   12 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   12 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
}

// buildTelegramTransport 优先 TG_PROXY，其次 HTTPS_PROXY / HTTP_PROXY / ALL_PROXY。
// 支持 http / https / socks5 代理。
func buildTelegramTransport() (*http.Transport, string, error) {
	proxyRaw := firstNonEmpty(
		os.Getenv("TG_PROXY"),
		os.Getenv("HTTPS_PROXY"),
		os.Getenv("https_proxy"),
		os.Getenv("HTTP_PROXY"),
		os.Getenv("http_proxy"),
		os.Getenv("ALL_PROXY"),
		os.Getenv("all_proxy"),
	)
	base := defaultTelegramTransport()
	if proxyRaw == "" {
		return base, "", nil
	}
	u, err := url.Parse(proxyRaw)
	if err != nil {
		return nil, "", fmt.Errorf("解析代理地址失败: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	label := proxyDisplay(u)

	switch scheme {
	case "http", "https":
		base.Proxy = http.ProxyURL(u)
		return base, label, nil
	case "socks5", "socks5h":
		// socks5h = 在代理侧解析 DNS
		var auth *xproxy.Auth
		if u.User != nil {
			pass, _ := u.User.Password()
			auth = &xproxy.Auth{User: u.User.Username(), Password: pass}
		}
		dialer, err := xproxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{
			Timeout:   12 * time.Second,
			KeepAlive: 30 * time.Second,
		})
		if err != nil {
			return nil, "", fmt.Errorf("创建 SOCKS5 代理失败: %w", err)
		}
		if cd, ok := dialer.(xproxy.ContextDialer); ok {
			base.DialContext = cd.DialContext
		} else {
			base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			}
		}
		return base, label, nil
	default:
		return nil, "", fmt.Errorf("不支持的代理协议 %q（请用 http:// 或 socks5://）", scheme)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func proxyDisplay(u *url.URL) string {
	if u == nil {
		return ""
	}
	host := u.Host
	if host == "" {
		host = u.String()
	}
	return u.Scheme + "://" + host
}

// Inline keyboard types (Telegram Bot API).
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"` // 打开链接（站内播放页）
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// Reply keyboard（输入框上方常驻按钮）。
type KeyboardButton struct {
	Text string `json:"text"`
}

type ReplyKeyboardMarkup struct {
	Keyboard        [][]KeyboardButton `json:"keyboard"`
	ResizeKeyboard  bool               `json:"resize_keyboard,omitempty"`
	IsPersistent    bool               `json:"is_persistent,omitempty"`
	OneTimeKeyboard bool               `json:"one_time_keyboard,omitempty"`
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
	// URL 含 token，仅用于请求；错误信息必须脱敏
	apiURL := fmt.Sprintf("%s/bot%s/%s", telegramAPIBase, token, method)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, sanitizeTelegramErr(ctx.Err(), token, c.proxyURL)
			case <-time.After(400 * time.Millisecond):
			}
		}
		var rdr io.Reader = bodyReader
		if payload != nil {
			raw, _ := json.Marshal(payload)
			rdr = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, rdr)
		if err != nil {
			return nil, sanitizeTelegramErr(err, token, c.proxyURL)
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
		return nil, sanitizeTelegramErr(lastErr, token, c.proxyURL)
	}
	return nil, fmt.Errorf("telegram 请求失败")
}

// sanitizeTelegramErr 去掉错误中的 Bot Token，并对超时/连不通给出可操作提示。
func sanitizeTelegramErr(err error, token, proxyLabel string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if token != "" {
		msg = strings.ReplaceAll(msg, token, MaskBotToken(token))
	}
	msg = botTokenURLPattern.ReplaceAllString(msg, "/bot***/")
	// 去掉完整 URL 里可能残留的敏感段
	msg = strings.ReplaceAll(msg, "https://api.telegram.org", "Telegram API")

	lower := strings.ToLower(msg)
	isTimeout := strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "i/o timeout") ||
		strings.Contains(lower, "tls handshake timeout") ||
		strings.Contains(lower, "context canceled") ||
		strings.Contains(lower, "client.timeout")
	isNet := isTimeout ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "network is unreachable") ||
		strings.Contains(lower, "connection reset")

	if isNet {
		hint := "无法连接 Telegram API"
		if isTimeout {
			hint = "连接 Telegram API 超时"
		}
		if proxyLabel == "" {
			return fmt.Errorf("%s（直连）。可设置 TG_PROXY 后重启", hint)
		}
		return fmt.Errorf("%s（代理 %s 不可用）", hint, proxyLabel)
	}
	return fmt.Errorf("%s", msg)
}

func (c *telegramClient) sendMessage(ctx context.Context, token, chatID, text string) error {
	return c.sendMessageToTarget(ctx, token, chatID, "", text, nil)
}

func (c *telegramClient) sendMessageWithMarkup(ctx context.Context, token, chatID, text string, markup *InlineKeyboardMarkup) error {
	return c.sendMessageToTarget(ctx, token, chatID, "", text, markup)
}

// sendMessageWithReplyKeyboard 发送带底部常驻键盘的消息。
func (c *telegramClient) sendMessageWithReplyKeyboard(ctx context.Context, token, chatID, text string, keyboard *ReplyKeyboardMarkup) error {
	return c.sendMessageToTarget(ctx, token, chatID, "", text, keyboard)
}

func (c *telegramClient) sendMessageToTarget(ctx context.Context, token, chatID, threadID, text string, markup any) error {
	token = strings.TrimSpace(token)
	chatID = strings.TrimSpace(chatID)
	threadID = strings.TrimSpace(threadID)
	if token == "" || chatID == "" {
		return fmt.Errorf("bot token 或 chat id 为空")
	}
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if threadID != "" {
		// Bot API 要求 message_thread_id 为整数
		if tid, err := strconv.ParseInt(threadID, 10, 64); err == nil && tid > 0 {
			payload["message_thread_id"] = tid
		}
	}
	if rm := replyMarkupPayload(markup); rm != nil {
		payload["reply_markup"] = rm
	}
	_, err := c.apiCall(ctx, token, "sendMessage", payload)
	return err
}

// replyMarkupPayload 将内联/常驻键盘规范为可序列化 payload；空键盘返回 nil。
func replyMarkupPayload(markup any) any {
	if markup == nil {
		return nil
	}
	switch m := markup.(type) {
	case *InlineKeyboardMarkup:
		if m == nil || len(m.InlineKeyboard) == 0 {
			return nil
		}
		return m
	case InlineKeyboardMarkup:
		if len(m.InlineKeyboard) == 0 {
			return nil
		}
		return m
	case *ReplyKeyboardMarkup:
		if m == nil || len(m.Keyboard) == 0 {
			return nil
		}
		return m
	case ReplyKeyboardMarkup:
		if len(m.Keyboard) == 0 {
			return nil
		}
		return m
	default:
		return markup
	}
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

type botCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

func (c *telegramClient) setMyCommands(ctx context.Context, token string, commands []botCommand) error {
	if len(commands) == 0 {
		return nil
	}
	_, err := c.apiCall(ctx, token, "setMyCommands", map[string]any{
		"commands": commands,
	})
	return err
}

type telegramUpdate struct {
	UpdateID      int64             `json:"update_id"`
	CallbackQuery *telegramCallback `json:"callback_query"`
	Message       *telegramMessage  `json:"message"`
}

type telegramCallback struct {
	ID      string           `json:"id"`
	From    *telegramUser    `json:"from"`
	Message *telegramMessage `json:"message"`
	Data    string           `json:"data"`
}

type telegramMessage struct {
	MessageID int64 `json:"message_id"`
	Chat      *struct {
		ID       int64  `json:"id"`
		Username string `json:"username"` // 公开群/频道/用户的 @username（不含 @ 前缀）
		Type     string `json:"type"`     // private / group / supergroup / channel
	} `json:"chat"`
	From *telegramUser `json:"from"`
	Text string        `json:"text"`
}

type telegramUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

func (c *telegramClient) getUpdates(ctx context.Context, token string, offset int64, timeoutSec int) ([]telegramUpdate, error) {
	// getUpdates uses long-poll; client timeout must exceed timeoutSec
	payload := map[string]any{
		"offset":  offset,
		"timeout": timeoutSec,
		// 回调分页 + /search 文本指令
		"allowed_updates": []string{"callback_query", "message"},
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
