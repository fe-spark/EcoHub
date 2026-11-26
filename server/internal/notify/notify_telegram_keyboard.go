package notify

import (
	"fmt"
	"regexp"
	"server/internal/model"
	"strings"
)

func buildOverviewKeyboard(sess model.NotifyBatchSession, sitePage int) *InlineKeyboardMarkup {
	if len(sess.Sources) == 0 || strings.TrimSpace(sess.BatchID) == "" {
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
		if src.Status == model.CollectStatusFailed || src.Error != "" {
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
		if c.Count <= 0 {
			continue
		}
		row = append(row, InlineKeyboardButton{
			Text:         fmt.Sprintf("%s (%d)", c.CategoryName, c.Count),
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
