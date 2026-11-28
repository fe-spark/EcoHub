package notify

import (
	"fmt"
	"regexp"
	"server/internal/model"
	"strings"
)

// buildPrimaryOverviewKeyboard 在汇总卡片底部直接列出当前主站分类按钮，并附带可选的后台直达 URL 按钮。
func buildPrimaryOverviewKeyboard(sess model.NotifyBatchSession, manageURL string) *InlineKeyboardMarkup {
	var rows [][]InlineKeyboardButton
	if sess.PrimaryTotal > 0 && strings.TrimSpace(sess.BatchID) != "" {
		var row []InlineKeyboardButton
		for idx, c := range sess.Categories {
			if c.Count <= 0 {
				continue
			}
			row = append(row, InlineKeyboardButton{
				Text:         fmt.Sprintf("%s (%d)", c.CategoryName, c.Count),
				CallbackData: fmt.Sprintf("%s:%s:c:%d:1", telegramCallbackPref, sess.BatchID, idx),
			})
			if len(row) == 2 {
				rows = append(rows, row)
				row = nil
			}
		}
		if len(sess.Categories) > 1 || len(rows) == 0 {
			row = append(row, InlineKeyboardButton{
				Text:         fmt.Sprintf("📋 全部 (%d)", sess.PrimaryTotal),
				CallbackData: fmt.Sprintf("%s:%s:c:-1:1", telegramCallbackPref, sess.BatchID),
			})
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	if isTelegramSupportedWebURL(manageURL) {
		rows = append(rows, []InlineKeyboardButton{{
			Text: "🛠 后台查看 / 重试",
			URL:  strings.TrimSpace(manageURL),
		}})
	}
	if len(rows) == 0 {
		return nil
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func buildPagedNavRow(batchID string, page, totalPages int, pageCallback func(int) string) []InlineKeyboardButton {
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
		{Text: fmt.Sprintf("%d / %d", page, totalPages), CallbackData: fmt.Sprintf("%s:%s:info:0", telegramCallbackPref, batchID)},
		{Text: nextText, CallbackData: nextData},
	}
}

func buildPrimaryPagedKeyboard(batchID string, catIdx, page, totalPages int) *InlineKeyboardMarkup {
	var rows [][]InlineKeyboardButton
	if totalPages > 1 {
		rows = append(rows, buildPagedNavRow(batchID, page, totalPages, func(p int) string {
			return fmt.Sprintf("%s:%s:c:%d:%d", telegramCallbackPref, batchID, catIdx, p)
		}))
	}
	rows = append(rows, []InlineKeyboardButton{{
		Text:         "🔙 返回采集汇总",
		CallbackData: fmt.Sprintf("%s:%s:home:0", telegramCallbackPref, batchID),
	}})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

var htmlTagStripRe = regexp.MustCompile(`<[^>]*>`)
