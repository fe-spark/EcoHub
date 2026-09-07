package notify

import (
	"context"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"server/internal/model"
)

const callbackPrefix = "nfp"

func siteURLConfigured() bool {
	return strings.TrimSpace(sitePlayBaseURLFn()) != ""
}

// buildCategoryKeyboard 分类入口键盘（2列排列，尾部带全部）
func buildCategoryKeyboard(prefix, sessionID string, cats []CategoryCountItem) *InlineKeyboardMarkup {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if len(cats) == 0 {
		return &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{{
				{
					Text:         "📋 查看更新列表",
					CallbackData: formatOpenCallback(prefix, sessionID, catIdxAll),
				},
			}},
		}
	}

	var rows [][]InlineKeyboardButton
	var currentRow []InlineKeyboardButton
	totalSum := 0

	for i, c := range cats {
		totalSum += c.Count
		btn := InlineKeyboardButton{
			Text:         fmt.Sprintf("%s (%d)", c.CategoryName, c.Count),
			CallbackData: formatOpenCallback(prefix, sessionID, i),
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
			CallbackData: formatOpenCallback(prefix, sessionID, catIdxAll),
		}
		currentRow = append(currentRow, btnAll)
	}

	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func batchCatName(sess FilmBatchSession, catIdx int) string {
	if catIdx < 0 || catIdx >= len(sess.Cats) {
		return ""
	}
	return sess.Cats[catIdx].CategoryName
}

func batchPageChunk(sess FilmBatchSession, catIdx, page int) (chunk []ChangeMidItem, total, start, end, pageOut int) {
	var targetMids []int64
	var allMode bool
	if catIdx < 0 {
		allMode = true
		total = len(sess.AllItems)
	} else if catIdx < len(sess.CatMids) {
		targetMids = sess.CatMids[catIdx]
		total = len(targetMids)
	}

	ps := sess.PageSize
	if ps <= 0 {
		ps = model.DefaultMaxFilmsInMessage
	}
	if total == 0 {
		return nil, 0, 0, 0, 1
	}
	totalPages := (total + ps - 1) / ps
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start = (page - 1) * ps
	end = start + ps
	if end > total {
		end = total
	}

	if allMode {
		chunk = sess.AllItems[start:end]
		return chunk, total, start, end, page
	}

	subMids := targetMids[start:end]
	midSet := make(map[int64]struct{}, len(subMids))
	for _, m := range subMids {
		midSet[m] = struct{}{}
	}
	sourceMap := make(map[int64]string, len(subMids))
	for _, it := range sess.AllItems {
		if _, ok := midSet[it.Mid]; ok {
			sourceMap[it.Mid] = it.SourceName
			if len(sourceMap) == len(subMids) {
				break
			}
		}
	}
	chunk = make([]ChangeMidItem, 0, len(subMids))
	for _, mid := range subMids {
		chunk = append(chunk, ChangeMidItem{Mid: mid, SourceName: sourceMap[mid]})
	}
	return chunk, total, start, end, page
}

func formatFilmListPageWithChunkCategory(sess FilmBatchSession, page int, chunk []ChangeMidItem, total, start, end int, category, listTitle string) string {
	listTitle = html.EscapeString(strings.TrimSpace(listTitle))
	if listTitle == "" {
		listTitle = "本次更新列表"
	}
	categoryTitle := ""
	if category != "" && category != "全部" {
		categoryTitle = fmt.Sprintf(" · %s", html.EscapeString(category))
	}
	if total <= 0 && len(chunk) == 0 {
		return fmt.Sprintf("<b>%s %s%s</b>\n<i>本分类暂无变更内容</i>\n", formatTitlePrefix(sess.SiteName), listTitle, categoryTitle)
	}
	mids := make([]int64, 0, len(chunk))
	for _, item := range chunk {
		mids = append(mids, item.Mid)
	}
	names := resolveFilmNames(mids)

	totalPages := 1
	if sess.PageSize > 0 {
		totalPages = (total + sess.PageSize - 1) / sess.PageSize
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s %s%s</b>\n", formatTitlePrefix(sess.SiteName), listTitle, categoryTitle)
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
		name := names[item.Mid]
		if utf8.RuneCountInString(name) > 40 {
			r := []rune(name)
			name = string(r[:40]) + "…"
		}
		line := formatFilmLine(model.FilmNotifyItem{Mid: item.Mid, Name: name, SourceName: item.SourceName})
		line = strings.TrimPrefix(line, "· ")
		next := fmt.Sprintf("%d. %s", start+i+1, line)
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

	batchID, page, catIdx, _, kind, ok := parsePagedCallback(callbackPrefix, cb.Data)
	if !ok {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "无效操作", false)
		return
	}

	sess, err := loadFilmBatchSession(batchID)
	if err != nil {
		log.Printf("[Notify] 更新列表回调批次不可用 batch=%s err=%v", batchID, err)
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "列表已过期或不存在", true)
		return
	}

	switch kind {
	case "noop":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "没有更多页了", false)
		return
	case "info":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, fmt.Sprintf("共 %d 部影片", sess.Total), false)
		return
	case "back":
		if cb.Message == nil || cb.Message.Chat == nil {
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "无法定位消息", true)
			return
		}
		chatID := strconv.FormatInt(cb.Message.Chat.ID, 10)
		text := strings.TrimSpace(sess.OverviewText)
		if text == "" {
			text = fmt.Sprintf("<b>%s 采集概要</b>\n📋 共 <b>%d</b> 部有更新", formatTitlePrefix(sess.SiteName), sess.Total)
		} else if utf8.RuneCountInString(text) > 4000 {
			text = truncateRunes(text, 4000)
		}
		markup := buildCategoryKeyboard(callbackPrefix, batchID, sess.Cats)
		if err := client.editMessageText(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil {
			if !strings.Contains(err.Error(), "message is not modified") {
				log.Printf("[Notify] editMessageText 返回概要失败: %v", err)
				_ = client.answerCallbackQuery(ctx, token, cb.ID, "返回失败", true)
				return
			}
		}
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "已返回分类", false)
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
	chunk, total, start, end, page := batchPageChunk(sess, catIdx, page)
	ps := sess.PageSize
	if ps <= 0 {
		ps = 15
	}
	totalPages := 1
	if total > 0 {
		totalPages = (total + ps - 1) / ps
	}

	catName := batchCatName(sess, catIdx)
	text := formatFilmListPageWithChunkCategory(sess, page, chunk, total, start, end, catName, "")
	markup := buildPagedKeyboardCategory(callbackPrefix, batchID, catIdx, page, totalPages, true)
	if err := client.editMessageText(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil {
		if !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] editMessageText 失败: %v", err)
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "翻页失败", true)
			return
		}
	}
	hint := "更新列表"
	if catName != "" {
		hint = fmt.Sprintf("%s · 第 %d/%d 页", catName, page, totalPages)
	} else if kind == "page" {
		hint = fmt.Sprintf("第 %d/%d 页", page, totalPages)
	}
	_ = client.answerCallbackQuery(ctx, token, cb.ID, hint, false)
}
