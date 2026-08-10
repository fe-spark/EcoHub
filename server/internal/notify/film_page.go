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

// formatFilmListPageWithChunk 使用已加载的 mid 页渲染，避免回调路径重复查库。
func formatFilmListPageWithChunk(sess FilmPageSession, page int, chunk []int64, total, start, end int) string {
	if total <= 0 && len(chunk) == 0 {
		return fmt.Sprintf("<b>%s 本次更新列表</b>\n<i>本批无影片内容/播放源变更</i>\n", formatTitlePrefix(sess.SiteName))
	}
	if total > 0 {
		sess.Total = total
	}
	totalPages := sess.totalPages()
	if page < 1 {
		page = 1
	}
	meta := ResolveFilmMeta(chunk)

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 本次更新列表</b>\n", formatTitlePrefix(sess.SiteName))
	fmt.Fprintf(&b, "<i>主站已有影片；任一播放源「最后一集」有变化才计入</i>\n")
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
	for i, mid := range chunk {
		name := meta[mid].Name
		if utf8.RuneCountInString(name) > 40 {
			r := []rune(name)
			name = string(r[:40]) + "…"
		}
		line := formatFilmLine(model.FilmNotifyItem{Mid: mid, Name: name})
		line = strings.TrimPrefix(line, "· ")
		b.WriteString(fmt.Sprintf("%d. ", start+i+1))
		b.WriteString(line)
		if utf8.RuneCountInString(b.String()) > telegramMaxMessageLen-80 {
			fmt.Fprintf(&b, "\n<i>…本页已截断</i>")
			break
		}
	}
	return b.String()
}

func siteURLConfigured() bool {
	return strings.TrimSpace(sitePlayBaseURLFn()) != ""
}

func buildOverviewKeyboard(batchID string) *InlineKeyboardMarkup {
	if strings.TrimSpace(batchID) == "" {
		return nil
	}
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{{
			{
				Text:         "📋 更新列表",
				CallbackData: fmt.Sprintf("%s:%s:open", callbackPrefix, batchID),
			},
		}},
	}
}

func handleFilmPageCallback(token string, cb *telegramCallback) {
	if cb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	batchID, page, kind, ok := parsePagedCallback(callbackPrefix, cb.Data)
	if !ok {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "无效操作", false)
		return
	}

	sess, err := loadFilmPageSession(batchID)
	if err != nil {
		// 打日志便于定位「刚收到就过期」类问题：批次是否真的不存在、expire_at 是否异常
		log.Printf("[Notify] 更新列表回调批次不可用 batch=%s err=%v", batchID, err)
		switch {
		case errors.Is(err, ErrChangeBatchNotFound):
			// 常见根因：多实例共用 Bot Token，回调被另一实例消费，其库无此批次
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
	totalPages := sess.totalPages()

	switch kind {
	case "noop":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "没有更多页了", false)
		return
	case "info":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, fmt.Sprintf("共 %d 页 · %d 条", totalPages, sess.Total), false)
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
	if totalPages > 0 && page > totalPages {
		page = totalPages
	}
	if cb.Message == nil || cb.Message.Chat == nil {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "无法定位消息", true)
		return
	}
	chatID := strconv.FormatInt(cb.Message.Chat.ID, 10)
	chunk, total, start, end, page, err := LoadChangeMidPage(batchID, page, sess.PageSize)
	if err != nil {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "加载失败", true)
		return
	}
	if total > 0 {
		sess.Total = total
		totalPages = sess.totalPages()
	}
	text := formatFilmListPageWithChunk(sess, page, chunk, total, start, end)
	markup := buildPagedKeyboard(callbackPrefix, batchID, page, totalPages, true)
	if err := client.editMessageText(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil {
		if !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] editMessageText 失败: %v", err)
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "翻页失败", true)
			return
		}
	}
	hint := "更新列表"
	if kind == "page" {
		hint = fmt.Sprintf("第 %d/%d 页", page, totalPages)
	}
	_ = client.answerCallbackQuery(ctx, token, cb.ID, hint, false)
}
