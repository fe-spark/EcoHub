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
	"regexp"
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
	notifyInfoPrimary     = -1
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
	ok, proxyURL, err := repository.ResolveNotifyProxy()
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

func formatBatchOverviewHTML(siteName string, payload model.CollectBatchNotifyPayload, primaryName string, updatedCount int) string {
	durSec := int64(payload.FinishedAt.Sub(payload.StartedAt).Seconds())
	if durSec < 0 {
		durSec = 0
	}
	successSrc, failedSrc, totalStored := 0, 0, 0
	for _, s := range payload.Sources {
		if s.Status == progress.StatusFailed || s.Error != "" {
			failedSrc++
		} else {
			successSrc++
		}
		totalStored += s.FilmsTotal
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 采集汇总</b>\n", formatTitlePrefix(siteName))
	fmt.Fprintf(&b, "⚡ 触发：%s · ⏱ 耗时：%s\n",
		html.EscapeString(triggerLabel(payload.Trigger)),
		html.EscapeString(formatDurationSec(durSec)),
	)
	if failedSrc > 0 {
		fmt.Fprintf(&b, "📊 站点：成功 <b>%d</b> 个，失败 <b>%d</b> 个 · 共入库 <b>%d</b> 部\n", successSrc, failedSrc, totalStored)
	} else {
		fmt.Fprintf(&b, "📊 站点：成功 <b>%d</b> 个 · 共入库 <b>%d</b> 部\n", successSrc, totalStored)
	}
	if name := strings.TrimSpace(primaryName); name != "" {
		fmt.Fprintf(&b, "⭐ 主站：<b>%s</b> · 前台可见 <b>%d</b> 部\n", html.EscapeString(singleLine(name)), updatedCount)
	} else {
		fmt.Fprintf(&b, "🎬 前台可见：<b>%d</b> 部\n", updatedCount)
	}
	if len(payload.Sources) > 0 {
		b.WriteString("\n👇 点击站点查看分类与片单")
	}
	return strings.TrimRight(b.String(), "\n")
}

func sourceStatusIconAndLabel(status, errMsg string) (string, string) {
	if status == progress.StatusFailed || errMsg != "" {
		return "❌", "失败"
	}
	if status == progress.StatusStopped {
		return "⏹", "已停止"
	}
	return "✅", "完成"
}

func formatSourceDetailHTML(siteName string, src model.SourceNotifyResult, isPrimary bool, filmCount int) string {
	icon, statusLabel := sourceStatusIconAndLabel(src.Status, src.Error)
	title := html.EscapeString(singleLine(src.SourceName))
	if title == "" {
		title = html.EscapeString(singleLine(src.SourceID))
	}
	primaryBadge := ""
	if isPrimary {
		primaryBadge = "⭐ "
	}
	displayTotal := src.FilmsTotal
	if filmCount > displayTotal {
		displayTotal = filmCount
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 站点明细 · %s%s</b>\n", formatTitlePrefix(siteName), primaryBadge, title)
	fmt.Fprintf(&b, "%s 状态：%s · 🎬 入库：<b>%d</b> 部", icon, statusLabel, displayTotal)
	if src.PageTotal > 0 {
		fmt.Fprintf(&b, " · 📄 页数：%d/%d", src.SuccessCnt, src.PageTotal)
	}
	b.WriteString("\n")
	if src.FailedCnt > 0 || src.CategorySkip > 0 {
		var extras []string
		if src.FailedCnt > 0 {
			extras = append(extras, fmt.Sprintf("失败 %d 页", src.FailedCnt))
		}
		if src.CategorySkip > 0 {
			extras = append(extras, fmt.Sprintf("跳过未映射 %d 部", src.CategorySkip))
		}
		b.WriteString("ℹ️ " + strings.Join(extras, "，") + "\n")
	}
	if strings.TrimSpace(src.Error) != "" {
		fmt.Fprintf(&b, "⚠️ 原因：<code>%s</code>\n", html.EscapeString(truncateRunes(singleLine(src.Error), 300)))
	}
	if filmCount > 0 {
		b.WriteString("\n👇 选择分类查看片单")
	}
	return strings.TrimRight(b.String(), "\n")
}

func isTelegramSupportedWebURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func clampPageRange(total, pageSize, page int) (curPage, totalPages, start, end int) {
	if pageSize <= 0 {
		pageSize = model.DefaultNotifyPageSize
	}
	totalPages = 1
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	curPage = page
	if curPage < 1 {
		curPage = 1
	}
	if curPage > totalPages {
		curPage = totalPages
	}
	if total == 0 {
		return curPage, totalPages, 0, 0
	}
	start = (curPage - 1) * pageSize
	end = start + pageSize
	if end > total {
		end = total
	}
	return curPage, totalPages, start, end
}

func formatFilmItemsPageHTML(siteName, title string, page, totalPages, start, total int, items []model.NotifyFilmItem) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 更新片单 · %s</b>\n", formatTitlePrefix(siteName), html.EscapeString(title))
	if total == 0 {
		b.WriteString("<i>暂无更新影片</i>\n")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "📄 第 <b>%d/%d</b> 页 · <code>%d–%d</code> / 共 <b>%d</b> 部\n\n",
		page, totalPages, start+1, start+len(items), total)

	basic, err := repository.GetSiteBasic()
	if err != nil {
		return "", err
	}
	siteURL := strings.TrimRight(strings.TrimSpace(basic.SiteURL), "/")
	canLink := isTelegramSupportedWebURL(siteURL)
	for _, f := range items {
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
	return b.String(), nil
}

func formatFilmListPageHTML(sess model.NotifyBatchSession, catIdx, page int) (string, int, error) {
	var films []model.NotifyFilmItem
	catTitle := "全部"
	if catIdx >= 0 && catIdx < len(sess.Categories) {
		films = sess.Categories[catIdx].Films
		catTitle = sess.Categories[catIdx].CategoryName
	} else {
		films = sess.AllFilms
	}
	total := len(films)
	curPage, totalPages, start, end := clampPageRange(total, sess.PageSize, page)
	text, err := formatFilmItemsPageHTML(sess.SiteName, catTitle, curPage, totalPages, start, total, films[start:end])
	return text, totalPages, err
}

func formatSourceFilmListPageHTML(sess model.NotifyBatchSession, srcIdx, catIdx, page int) (string, int, error) {
	if srcIdx < 0 || srcIdx >= len(sess.Sources) {
		return "", 1, errors.New("站点不存在")
	}
	src := sess.Sources[srcIdx]
	mids := src.AllMids
	catTitle := "全部"
	total := src.TotalFilmCount()
	var selectedCat model.NotifySourceCategoryGroup
	if catIdx >= 0 && catIdx < len(src.Categories) {
		selectedCat = src.Categories[catIdx]
		mids = selectedCat.Mids
		catTitle = selectedCat.CategoryName
		total = selectedCat.FilmCount()
	}
	curPage, totalPages, start, end := clampPageRange(total, sess.PageSize, page)
	var items []model.NotifyFilmItem
	if total > 0 {
		if len(mids) > 0 {
			pageMids := mids[start:end]
			metas, err := repository.LoadNotifyFilmMetas(pageMids)
			if err != nil {
				return "", 1, err
			}
			nameByMid := make(map[int64]string, len(metas))
			for _, m := range metas {
				nameByMid[m.Mid] = m.Name
			}
			items = make([]model.NotifyFilmItem, 0, len(pageMids))
			for _, mid := range pageMids {
				items = append(items, model.NotifyFilmItem{Mid: mid, Name: nameByMid[mid]})
			}
		} else if strings.TrimSpace(sess.CollectBatchID) != "" {
			matcher, err := loadSourceCategoryMatcher(src.SourceID)
			if err != nil {
				return "", 1, err
			}
			items, err = repository.LoadBatchSourceCategoryPage(
				matcher.buildPageQuery(sess.CollectBatchID, src.SourceID, catIdx, selectedCat, end-start, start),
			)
			if err != nil {
				return "", 1, err
			}
		}
	}
	srcTitle := singleLine(src.SourceName)
	if srcTitle == "" {
		srcTitle = src.SourceID
	}
	text, err := formatFilmItemsPageHTML(sess.SiteName, fmt.Sprintf("%s · %s", srcTitle, catTitle), curPage, totalPages, start, total, items)
	return text, totalPages, err
}

const maxSiteButtonsPerPage = 90

func buildOverviewKeyboard(sess model.NotifyBatchSession, sitePage int) *InlineKeyboardMarkup {
	if len(sess.Sources) == 0 {
		return buildCategoryKeyboard(sess.BatchID, sess.Categories, len(sess.AllFilms))
	}
	if strings.TrimSpace(sess.BatchID) == "" {
		return nil
	}
	total := len(sess.Sources)
	curPage, totalPages, start, end := clampPageRange(total, maxSiteButtonsPerPage, sitePage)
	var rows [][]InlineKeyboardButton
	var row []InlineKeyboardButton
	for idx := start; idx < end; idx++ {
		src := sess.Sources[idx]
		icon, _ := sourceStatusIconAndLabel(src.Status, src.Error)
		if src.IsPrimary && icon == "✅" {
			icon = "⭐"
		}
		name := truncateRunes(singleLine(src.SourceName), 16)
		if name == "" {
			name = truncateRunes(singleLine(src.SourceID), 16)
		}
		label := fmt.Sprintf("%s %s (%d)", icon, name, src.FilmsTotal)
		if src.Status == progress.StatusFailed || src.Error != "" {
			if src.FilmsTotal > 0 {
				label = fmt.Sprintf("%s %s (%d/失败)", icon, name, src.FilmsTotal)
			} else {
				label = fmt.Sprintf("%s %s (失败)", icon, name)
			}
		}
		row = append(row, InlineKeyboardButton{
			Text:         label,
			CallbackData: fmt.Sprintf("%s:%s:s:%d", telegramCallbackPref, sess.BatchID, idx),
		})
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	if totalPages > 1 {
		rows = append(rows, buildPagedNavRow(sess.BatchID, curPage, totalPages, notifyInfoAllSources, func(p int) string {
			return fmt.Sprintf("%s:%s:sp:%d", telegramCallbackPref, sess.BatchID, p)
		}))
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func buildSourceCategoryKeyboard(batchID string, srcIdx int, cats []model.NotifySourceCategoryGroup, total int) *InlineKeyboardMarkup {
	if total <= 0 || strings.TrimSpace(batchID) == "" {
		return nil
	}
	var rows [][]InlineKeyboardButton
	var row []InlineKeyboardButton
	for idx, c := range cats {
		cnt := c.FilmCount()
		if cnt == 0 {
			continue
		}
		row = append(row, InlineKeyboardButton{
			Text:         fmt.Sprintf("%s (%d)", c.CategoryName, cnt),
			CallbackData: fmt.Sprintf("%s:%s:sc:%d:%d:1", telegramCallbackPref, batchID, srcIdx, idx),
		})
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(cats) > 1 || len(rows) == 0 {
		row = append(row, InlineKeyboardButton{
			Text:         fmt.Sprintf("📋 全部 (%d)", total),
			CallbackData: fmt.Sprintf("%s:%s:sc:%d:-1:1", telegramCallbackPref, batchID, srcIdx),
		})
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// buildCategoryKeyboard 构建采集汇总底部的分类内联翻页按钮（点击进入对应分类的分页片单）
func buildCategoryKeyboard(batchID string, cats []model.NotifyCategoryGroup, total int) *InlineKeyboardMarkup {
	if total <= 0 || strings.TrimSpace(batchID) == "" {
		return nil
	}
	var rows [][]InlineKeyboardButton
	var row []InlineKeyboardButton
	for idx, c := range cats {
		if len(c.Films) == 0 {
			continue
		}
		row = append(row, InlineKeyboardButton{
			Text:         fmt.Sprintf("%s (%d)", c.CategoryName, len(c.Films)),
			CallbackData: fmt.Sprintf("%s:%s:%d:1", telegramCallbackPref, batchID, idx),
		})
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(cats) > 1 || len(rows) == 0 {
		row = append(row, InlineKeyboardButton{
			Text:         fmt.Sprintf("📋 全部 (%d)", total),
			CallbackData: fmt.Sprintf("%s:%s:-1:1", telegramCallbackPref, batchID),
		})
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func buildPagedNavRow(batchID string, page, totalPages, infoSrcIdx int, pageCallback func(int) string) []InlineKeyboardButton {
	prevData := fmt.Sprintf("%s:%s:noop:0", telegramCallbackPref, batchID)
	prevText := "·"
	if page > 1 {
		prevText = "◀ 上一页"
		prevData = pageCallback(page - 1)
	}
	nextData := fmt.Sprintf("%s:%s:noop:0", telegramCallbackPref, batchID)
	nextText := "·"
	if page < totalPages {
		nextText = "下一页 ▶"
		nextData = pageCallback(page + 1)
	}
	return []InlineKeyboardButton{
		{Text: prevText, CallbackData: prevData},
		{Text: fmt.Sprintf("%d / %d", page, totalPages), CallbackData: fmt.Sprintf("%s:%s:info:%d", telegramCallbackPref, batchID, infoSrcIdx)},
		{Text: nextText, CallbackData: nextData},
	}
}

func buildPagedKeyboard(batchID string, catIdx, page, totalPages int) *InlineKeyboardMarkup {
	var rows [][]InlineKeyboardButton
	if totalPages > 1 {
		rows = append(rows, buildPagedNavRow(batchID, page, totalPages, notifyInfoPrimary, func(p int) string {
			return fmt.Sprintf("%s:%s:%d:%d", telegramCallbackPref, batchID, catIdx, p)
		}))
	}
	rows = append(rows, []InlineKeyboardButton{{
		Text:         "🔙 返回采集汇总",
		CallbackData: fmt.Sprintf("%s:%s:back:0", telegramCallbackPref, batchID),
	}})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func buildSourcePagedKeyboard(batchID string, srcIdx, catIdx, page, totalPages int) *InlineKeyboardMarkup {
	var rows [][]InlineKeyboardButton
	if totalPages > 1 {
		rows = append(rows, buildPagedNavRow(batchID, page, totalPages, srcIdx, func(p int) string {
			return fmt.Sprintf("%s:%s:sc:%d:%d:%d", telegramCallbackPref, batchID, srcIdx, catIdx, p)
		}))
	}
	rows = append(rows, []InlineKeyboardButton{{
		Text:         "🔙 返回站点分类",
		CallbackData: fmt.Sprintf("%s:%s:sb:%d", telegramCallbackPref, batchID, srcIdx),
	}})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

var htmlTagStripRe = regexp.MustCompile(`<[^>]*>`)

// safeTruncateHTML 按完整行安全截断 Telegram HTML 消息，避免切断标签或 HTML 实体导致发送报错。
func safeTruncateHTML(htmlText string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(htmlText) <= maxRunes {
		return htmlText
	}
	budget := maxRunes - 16
	if budget <= 0 {
		return html.EscapeString(truncateRunes(htmlTagStripRe.ReplaceAllString(htmlText, ""), maxRunes))
	}
	lines := strings.Split(htmlText, "\n")
	var kept []string
	used := 0
	for _, line := range lines {
		lineRunes := utf8.RuneCountInString(line)
		sep := 0
		if len(kept) > 0 {
			sep = 1
		}
		if used+sep+lineRunes > budget {
			break
		}
		kept = append(kept, line)
		used += sep + lineRunes
	}
	if len(kept) == 0 {
		// 单行本身超长时，剥离标签后做纯文本转义截断作为兜底
		plain := html.UnescapeString(htmlTagStripRe.ReplaceAllString(htmlText, ""))
		return html.EscapeString(truncateRunes(plain, budget)) + "\n..."
	}
	return strings.Join(kept, "\n") + "\n..."
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}

// 定时任务失败原因的截断上限：多步错误按行拼接，逐行截断，避免第一行过长把后面的步骤挤掉。
const (
	cronReasonLineMax  = 200
	cronReasonTotalMax = 600
)

func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}

// formatReasonCodeLines 按行截断并各自包裹 <code>，保证多行错误被 safeTruncateHTML 截断后标签仍自闭合。
func formatReasonCodeLines(s string, lineMax, totalMax int) string {
	text := truncateReasonLines(s, lineMax, totalMax)
	if text == "" {
		return "<code></code>"
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "<code>" + html.EscapeString(line) + "</code>"
	}
	return strings.Join(lines, "\n")
}

// truncateReasonLines 按行截断（按 rune 计数，不会截坏中文），再整体截断。空行丢弃。
func truncateReasonLines(s string, lineMax, totalMax int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept = append(kept, truncateRunes(line, lineMax))
	}
	return truncateRunes(strings.Join(kept, "\n"), totalMax)
}
