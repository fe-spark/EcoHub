package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"server/internal/model"
)

const callbackPrefix = "nfp"

// FilmPageSession 更新列表视图状态（数据在 MySQL 批次，不在 Redis）。
type FilmPageSession struct {
	BatchID      string
	SiteName     string
	PageSize     int
	Total        int
	OverviewText string
}

func (s FilmPageSession) totalPages() int {
	if s.Total <= 0 {
		return 0
	}
	ps := s.PageSize
	if ps <= 0 {
		ps = 15
	}
	return (s.Total + ps - 1) / ps
}

func loadFilmPageSession(batchID string) (FilmPageSession, error) {
	rec, err := LoadChangeBatch(batchID)
	if err != nil {
		return FilmPageSession{}, err
	}
	total := rec.Total
	if total <= 0 {
		total = CountChangeMids(batchID)
	}
	return FilmPageSession{
		BatchID:      rec.ID,
		SiteName:     rec.SiteName,
		PageSize:     rec.PageSize,
		Total:        total,
		OverviewText: rec.Overview,
	}, nil
}

func siteURLConfigured() bool {
	return strings.TrimSpace(sitePlayBaseURLFn()) != ""
}

func getCategoryIcon(name string) string {
	switch {
	case strings.Contains(name, "动漫") || strings.Contains(name, "动画"):
		return "🎨"
	case strings.Contains(name, "短剧"):
		return "📱"
	case strings.Contains(name, "电影") || strings.Contains(name, "影"):
		return "🎬"
	case strings.Contains(name, "剧"):
		return "📺"
	case strings.Contains(name, "综艺"):
		return "📹"
	case strings.Contains(name, "纪录"):
		return "🎞️"
	default:
		return "📦"
	}
}

func buildOverviewKeyboard(batchID string) *InlineKeyboardMarkup {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return nil
	}
	cats := GetChangeBatchCategoryCounts(batchID)
	if len(cats) == 0 {
		return &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{{
				{
					Text:         "📋 查看更新列表",
					CallbackData: formatOpenCallback(callbackPrefix, batchID, catIdxAll),
				},
			}},
		}
	}

	var rows [][]InlineKeyboardButton
	var currentRow []InlineKeyboardButton
	totalSum := 0

	for i, c := range cats {
		totalSum += c.Count
		icon := getCategoryIcon(c.CategoryName)
		// 按钮文案可含完整分类名；callback 只用短下标，规避 64 字节限制
		btn := InlineKeyboardButton{
			Text:         fmt.Sprintf("%s %s (%d)", icon, c.CategoryName, c.Count),
			CallbackData: formatOpenCallback(callbackPrefix, batchID, i),
		}
		currentRow = append(currentRow, btn)
		if len(currentRow) == 2 {
			rows = append(rows, currentRow)
			currentRow = nil
		}
	}

	if len(cats) > 1 {
		btnAll := InlineKeyboardButton{
			Text:         fmt.Sprintf("📋 全部 (%d)", totalSum),
			CallbackData: formatOpenCallback(callbackPrefix, batchID, catIdxAll),
		}
		currentRow = append(currentRow, btnAll)
	}

	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func formatFilmListPageWithChunk(sess FilmPageSession, page int, chunk []ChangeMidItem, total, start, end int) string {
	return formatFilmListPageWithChunkCategory(sess, page, chunk, total, start, end, "")
}

func formatFilmListPageWithChunkCategory(sess FilmPageSession, page int, chunk []ChangeMidItem, total, start, end int, category string) string {
	categoryTitle := ""
	if category != "" && category != "全部" {
		categoryTitle = fmt.Sprintf(" · %s%s", getCategoryIcon(category), category)
	}
	if total <= 0 && len(chunk) == 0 {
		return fmt.Sprintf("<b>%s 本次更新列表%s</b>\n<i>本分类暂无变更内容</i>\n", formatTitlePrefix(sess.SiteName), categoryTitle)
	}
	mids := make([]int64, 0, len(chunk))
	for _, item := range chunk {
		mids = append(mids, item.Mid)
	}
	meta := ResolveFilmMeta(mids)

	totalPages := 1
	if sess.PageSize > 0 {
		totalPages = (total + sess.PageSize - 1) / sess.PageSize
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 本次更新列表%s</b>\n", formatTitlePrefix(sess.SiteName), categoryTitle)
	fmt.Fprintf(&b, "📄 第 <b>%d/%d</b> 页 · 本页 <b>%d</b> · <code>%d–%d</code> / <b>%d</b>\n",
		page, totalPages, len(chunk), start+1, end, total)
	if len(chunk) > 0 {
		if !siteURLConfigured() {
			fmt.Fprintf(&b, "<i>未配置网站地址，片名不可跳转。请在后台「网站配置」填写公网地址。</i>\n")
		} else {
			fmt.Fprintf(&b, "<i>点片名打开播放页</i>\n")
		}
	}
	b.WriteByte('\n')
	for i, item := range chunk {
		name := meta[item.Mid].Name
		if utf8.RuneCountInString(name) > 40 {
			r := []rune(name)
			name = string(r[:40]) + "…"
		}
		line := formatFilmLine(model.FilmNotifyItem{Mid: item.Mid, Name: name, SourceName: item.SourceName})
		line = strings.TrimPrefix(line, "· ")
		next := fmt.Sprintf("%d. %s\n", start+i+1, line)
		// Telegram 消息上限 4096；预留尾部截断提示空间
		if utf8.RuneCountInString(b.String())+utf8.RuneCountInString(next) > telegramMaxMessageLen-80 {
			fmt.Fprintf(&b, "\n<i>…本页已截断</i>")
			break
		}
		b.WriteString(next)
	}
	return b.String()
}

func handleFilmPageCallback(token string, cb *telegramCallback) {
	if cb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	batchID, page, catIdx, legacyCat, kind, ok := parsePagedCallback(callbackPrefix, cb.Data)
	if !ok {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "无效操作", false)
		return
	}

	sess, err := loadFilmPageSession(batchID)
	if err != nil {
		log.Printf("[Notify] 更新列表回调批次不可用 batch=%s err=%v", batchID, err)
		switch {
		case errors.Is(err, ErrChangeBatchNotFound):
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "批次不存在（可能由另一实例发送）", true)
		case errors.Is(err, ErrChangeBatchExpired):
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "列表已过期，请重新采集", true)
		case errors.Is(err, ErrChangeBatchEmpty):
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "批次为空", false)
		default:
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "列表加载失败，请稍后重试", true)
		}
		return
	}

	category := resolveCallbackCategory(batchID, catIdx, legacyCat)

	switch kind {
	case "noop":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "没有更多页了", false)
		return
	case "info":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, fmt.Sprintf("共 %d 条", sess.Total), false)
		return
	case "back":
		if cb.Message == nil || cb.Message.Chat == nil {
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "无法定位消息", true)
			return
		}
		chatID := strconv.FormatInt(cb.Message.Chat.ID, 10)
		text := strings.TrimSpace(sess.OverviewText)
		if text == "" {
			text = fmt.Sprintf("<b>%s 采集概要</b>\n<i>概要内容已失效</i>", formatTitlePrefix(sess.SiteName))
		}
		markup := buildOverviewKeyboard(batchID)
		if err := client.editMessageText(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil {
			if !strings.Contains(err.Error(), "message is not modified") {
				log.Printf("[Notify] editMessageText 返回概要失败: %v", err)
				_ = client.answerCallbackQuery(ctx, token, cb.ID, "返回失败", true)
				return
			}
		}
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "已返回概要", false)
		return
	}

	if page < 1 {
		page = 1
	}

	if cb.Message == nil || cb.Message.Chat == nil {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "无法定位消息", true)
		return
	}
	chatID := strconv.FormatInt(cb.Message.Chat.ID, 10)
	chunk, total, start, end, page, err := LoadChangeMidPageCategory(batchID, category, page, sess.PageSize)
	if err != nil {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "加载失败", true)
		return
	}

	ps := sess.PageSize
	if ps <= 0 {
		ps = 15
	}
	totalPages := 1
	if total > 0 {
		totalPages = (total + ps - 1) / ps
	}
	// 键盘仍用 catIdx；遗留名称回调无法回写短编码时降级为全部翻页
	kbCatIdx := catIdx
	if kbCatIdx < 0 && category != "" {
		// 旧消息带分类名：翻页时仍按名称筛选，键盘 callback 无法稳定编码名称则保持全部键+名称解析路径
		// 为兼容翻页，尝试在当前统计列表中定位下标
		cats := GetChangeBatchCategoryCounts(batchID)
		for i, c := range cats {
			if c.CategoryName == category {
				kbCatIdx = i
				break
			}
		}
	}
	text := formatFilmListPageWithChunkCategory(sess, page, chunk, total, start, end, category)
	markup := buildPagedKeyboardCategory(callbackPrefix, batchID, kbCatIdx, page, totalPages, true)
	if err := client.editMessageText(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil {
		if !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] editMessageText 失败: %v", err)
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "翻页失败", true)
			return
		}
	}
	hint := "更新列表"
	if category != "" {
		hint = fmt.Sprintf("%s · 第 %d/%d 页", category, page, totalPages)
	} else if kind == "page" {
		hint = fmt.Sprintf("第 %d/%d 页", page, totalPages)
	}
	_ = client.answerCallbackQuery(ctx, token, cb.ID, hint, false)
}
