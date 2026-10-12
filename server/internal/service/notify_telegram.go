package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/progress"
	"server/internal/utils"
)

const (
	telegramAPIBase       = "https://api.telegram.org"
	telegramCallbackPref  = "nfp"
	telegramMaxMessageLen = 4096
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
	MessageID int64   `json:"message_id"`
	Chat      *tgChat `json:"chat,omitempty"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

// tgAPIBaseURL 允许测试覆盖 Telegram API 地址
var tgAPIBaseURL = telegramAPIBase

func getTelegramHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if ok, proxyURL := repository.ResolveNotifyProxy(); ok && strings.TrimSpace(proxyURL) != "" {
		return &http.Client{
			Transport: utils.GetOrCreateProxyTransport(proxyURL),
			Timeout:   timeout,
		}
	}
	return &http.Client{Timeout: timeout}
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

	resp, err := getTelegramHTTPClient(timeout).Do(req)
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
	if utf8.RuneCountInString(htmlText) > telegramMaxMessageLen {
		htmlText = truncateRunes(htmlText, telegramMaxMessageLen-16) + "\n..."
	}
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

func editTelegramMessage(ctx context.Context, token, chatID string, messageID int64, htmlText string, markup *InlineKeyboardMarkup) error {
	if utf8.RuneCountInString(htmlText) > telegramMaxMessageLen {
		htmlText = truncateRunes(htmlText, telegramMaxMessageLen-16) + "\n..."
	}
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
	_, err := callTelegramJSON(ctx, token, "editMessageText", body, 15*time.Second)
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

func formatTitlePrefix(siteName string) string {
	siteName = strings.TrimSpace(siteName)
	if siteName == "" || strings.EqualFold(siteName, "EcoHub") {
		return "[EcoHub]"
	}
	return fmt.Sprintf("[EcoHub · %s]", html.EscapeString(siteName))
}

func triggerLabel(trigger string) string {
	switch trigger {
	case model.CollectTriggerCron:
		return "定时任务"
	case model.CollectTriggerSingleUpdate:
		return "单片更新"
	case model.CollectTriggerRecover:
		return "失败重试"
	case model.CollectTriggerManual:
		return "手动采集"
	default:
		if strings.TrimSpace(trigger) == "" {
			return "采集任务"
		}
		return trigger
	}
}

func formatDurationSec(sec int64) string {
	if sec <= 0 {
		return "1秒"
	}
	if sec < 60 {
		return fmt.Sprintf("%d秒", sec)
	}
	m := sec / 60
	s := sec % 60
	if m < 60 {
		if s == 0 {
			return fmt.Sprintf("%d分", m)
		}
		return fmt.Sprintf("%d分%d秒", m, s)
	}
	h := m / 60
	rm := m % 60
	return fmt.Sprintf("%d时%d分", h, rm)
}

func formatBatchOverviewHTML(siteName string, payload model.CollectBatchNotifyPayload, updatedCount int) string {
	durSec := int64(payload.FinishedAt.Sub(payload.StartedAt).Seconds())
	if durSec < 0 {
		durSec = 0
	}
	successSrc, failedSrc := 0, 0
	for _, s := range payload.Sources {
		if s.Status == progress.StatusFailed || s.Error != "" {
			failedSrc++
		} else {
			successSrc++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 采集汇总</b>\n", formatTitlePrefix(siteName))
	fmt.Fprintf(&b, "⚡ 触发：%s · ⏱ 耗时：%s\n",
		html.EscapeString(triggerLabel(payload.Trigger)),
		html.EscapeString(formatDurationSec(durSec)),
	)
	if failedSrc > 0 {
		fmt.Fprintf(&b, "📊 站点：成功 <b>%d</b> 个，失败 <b>%d</b> 个\n", successSrc, failedSrc)
	} else {
		fmt.Fprintf(&b, "📊 站点：成功 <b>%d</b> 个\n", successSrc)
	}
	fmt.Fprintf(&b, "🎬 本次更新：<b>%d</b> 部影片\n", updatedCount)

	if len(payload.Sources) > 0 {
		b.WriteString("\n<b>站点明细：</b>\n")
		for _, src := range payload.Sources {
			icon := "✅"
			if src.Status == progress.StatusFailed || src.Error != "" {
				icon = "❌"
			} else if src.Status == progress.StatusStopped {
				icon = "⏹"
			}
			line := fmt.Sprintf("%s <b>%s</b> (%d 部", icon, html.EscapeString(src.SourceName), src.FilmsTotal)
			if src.FailedCnt > 0 {
				line += fmt.Sprintf("，失败 %d 页", src.FailedCnt)
			}
			line += ")"
			if src.Error != "" {
				line += " — <code>" + html.EscapeString(truncateRunes(src.Error, 80)) + "</code>"
			}
			b.WriteString(line + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func isTelegramSupportedWebURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func formatFilmListPageHTML(sess model.NotifyBatchSession, catIdx, page int) (string, int) {
	var films []model.NotifyFilmItem
	catTitle := "全部"
	if catIdx >= 0 && catIdx < len(sess.Categories) {
		films = sess.Categories[catIdx].Films
		catTitle = sess.Categories[catIdx].CategoryName
	} else {
		films = sess.AllFilms
	}

	pageSize := sess.PageSize
	if pageSize <= 0 {
		pageSize = model.DefaultNotifyPageSize
	}
	total := len(films)
	totalPages := 1
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 更新片单 · %s</b>\n", formatTitlePrefix(sess.SiteName), html.EscapeString(catTitle))
	if total == 0 {
		b.WriteString("<i>暂无更新影片</i>\n")
		return b.String(), 1
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if end > total {
		end = total
	}
	fmt.Fprintf(&b, "📄 第 <b>%d/%d</b> 页 · <code>%d–%d</code> / 共 <b>%d</b> 部\n\n",
		page, totalPages, start+1, end, total)

	siteURL := strings.TrimRight(strings.TrimSpace(repository.GetSiteBasic().SiteURL), "/")
	canLink := isTelegramSupportedWebURL(siteURL)
	for _, f := range films[start:end] {
		name := truncateRunes(strings.TrimSpace(f.Name), 60)
		if name == "" {
			name = fmt.Sprintf("#%d", f.Mid)
		}
		if canLink && f.Mid > 0 {
			playHref := fmt.Sprintf("%s/play?id=%d", siteURL, f.Mid)
			fmt.Fprintf(&b, "· <a href=\"%s\">%s</a> (<code>#%d</code>)\n",
				html.EscapeString(playHref), html.EscapeString(name), f.Mid)
		} else {
			fmt.Fprintf(&b, "· %s (<code>#%d</code>)\n", html.EscapeString(name), f.Mid)
		}
	}
	return b.String(), totalPages
}

func buildCategoryKeyboard(batchID string, cats []model.NotifyCategoryGroup, total int) *InlineKeyboardMarkup {
	return buildCategoryKeyboardForSite(repository.GetSiteBasic().SiteURL, cats, total)
}

func buildCategoryKeyboardForSite(rawSiteURL string, cats []model.NotifyCategoryGroup, total int) *InlineKeyboardMarkup {
	if total <= 0 {
		return nil
	}
	siteURL := strings.TrimRight(strings.TrimSpace(rawSiteURL), "/")
	if !isTelegramSupportedWebURL(siteURL) {
		return nil
	}
	var rows [][]InlineKeyboardButton
	var row []InlineKeyboardButton
	for _, c := range cats {
		if len(c.Films) == 0 {
			continue
		}
		targetURL := siteURL
		if c.CategoryID > 0 {
			targetURL = fmt.Sprintf("%s/filmClassifySearch?Pid=%d", siteURL, c.CategoryID)
		}
		row = append(row, InlineKeyboardButton{
			Text: fmt.Sprintf("%s (%d)", c.CategoryName, len(c.Films)),
			URL:  targetURL,
		})
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(cats) > 1 || len(rows) == 0 {
		row = append(row, InlineKeyboardButton{
			Text: fmt.Sprintf("📋 全部 (%d)", total),
			URL:  siteURL,
		})
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func buildPagedKeyboard(batchID string, catIdx, page, totalPages int) *InlineKeyboardMarkup {
	var rows [][]InlineKeyboardButton
	if totalPages > 1 {
		prevData := fmt.Sprintf("%s:%s:noop:0", telegramCallbackPref, batchID)
		prevText := "·"
		if page > 1 {
			prevText = "◀ 上一页"
			prevData = fmt.Sprintf("%s:%s:%d:%d", telegramCallbackPref, batchID, catIdx, page-1)
		}
		nextData := fmt.Sprintf("%s:%s:noop:0", telegramCallbackPref, batchID)
		nextText := "·"
		if page < totalPages {
			nextText = "下一页 ▶"
			nextData = fmt.Sprintf("%s:%s:%d:%d", telegramCallbackPref, batchID, catIdx, page+1)
		}
		rows = append(rows, []InlineKeyboardButton{
			{Text: prevText, CallbackData: prevData},
			{Text: fmt.Sprintf("%d / %d", page, totalPages), CallbackData: fmt.Sprintf("%s:%s:info:0", telegramCallbackPref, batchID)},
			{Text: nextText, CallbackData: nextData},
		})
	}
	rows = append(rows, []InlineKeyboardButton{{
		Text:         "🔙 返回采集汇总",
		CallbackData: fmt.Sprintf("%s:%s:back:0", telegramCallbackPref, batchID),
	}})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
