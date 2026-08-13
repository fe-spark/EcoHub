package notify

import (
	"fmt"
	"strconv"
	"strings"
)

// catIdxAll 表示不按分类筛选（全部）。
const catIdxAll = -1

// buildPagedKeyboard 分页内联键盘：上一页/页码/下一页；withBack 时追加「返回分类」
func buildPagedKeyboard(prefix, sessionID string, page, totalPages int, withBack bool) *InlineKeyboardMarkup {
	return buildPagedKeyboardCategory(prefix, sessionID, catIdxAll, page, totalPages, withBack)
}

// buildPagedKeyboardCategory 带分类下标的分页内联键盘。
// catIdx < 0 表示全部；callback 使用稳定短编码（page / pagec{idx}），避免分类名撑爆 64 字节限制。
func buildPagedKeyboardCategory(prefix, sessionID string, catIdx, page, totalPages int, withBack bool) *InlineKeyboardMarkup {
	rows := make([][]InlineKeyboardButton, 0, 2)
	if totalPages > 0 {
		if page < 1 {
			page = 1
		}
		if page > totalPages {
			page = totalPages
		}
		nav := make([]InlineKeyboardButton, 0, 3)
		if page > 1 {
			nav = append(nav, InlineKeyboardButton{
				Text:         "◀ 上一页",
				CallbackData: formatPageCallback(prefix, sessionID, page-1, catIdx),
			})
		} else {
			nav = append(nav, InlineKeyboardButton{
				Text:         "·",
				CallbackData: fmt.Sprintf("%s:%s:noop", prefix, sessionID),
			})
		}
		nav = append(nav, InlineKeyboardButton{
			Text:         fmt.Sprintf("%d / %d", page, totalPages),
			CallbackData: fmt.Sprintf("%s:%s:info", prefix, sessionID),
		})
		if page < totalPages {
			nav = append(nav, InlineKeyboardButton{
				Text:         "下一页 ▶",
				CallbackData: formatPageCallback(prefix, sessionID, page+1, catIdx),
			})
		} else {
			nav = append(nav, InlineKeyboardButton{
				Text:         "·",
				CallbackData: fmt.Sprintf("%s:%s:noop", prefix, sessionID),
			})
		}
		rows = append(rows, nav)
	}
	if withBack {
		rows = append(rows, []InlineKeyboardButton{{
			Text:         "🔙 返回分类",
			CallbackData: fmt.Sprintf("%s:%s:back", prefix, sessionID),
		}})
	}
	if len(rows) == 0 {
		return nil
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// formatPageCallback 生成翻页 callback_data。
// 全部: {prefix}:{id}:{page}
// 分类: {prefix}:{id}:{page}c{idx}
func formatPageCallback(prefix, sessionID string, page, catIdx int) string {
	if catIdx < 0 {
		return fmt.Sprintf("%s:%s:%d", prefix, sessionID, page)
	}
	return fmt.Sprintf("%s:%s:%dc%d", prefix, sessionID, page, catIdx)
}

// formatOpenCallback 生成打开列表 callback_data。
// 全部: open；分类: openc{idx}
func formatOpenCallback(prefix, sessionID string, catIdx int) string {
	if catIdx < 0 {
		return fmt.Sprintf("%s:%s:open", prefix, sessionID)
	}
	return fmt.Sprintf("%s:%s:openc%d", prefix, sessionID, catIdx)
}

// parsePagedCallback 解析分页回调。
// 新格式: open | openc{idx} | {page} | {page}c{idx} | noop | info | back
// 兼容旧格式: open_{分类名} | {page}_{分类名}（仍可能超长，仅服务历史消息）
// 返回 catIdx；旧格式按名称解析时 catIdx=catIdxAll 且 category 为名称。
func parsePagedCallback(prefix, data string) (id string, page int, catIdx int, category string, kind string, ok bool) {
	data = strings.TrimSpace(data)
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != prefix {
		return "", 0, catIdxAll, "", "", false
	}
	id = parts[1]
	if id == "" {
		return "", 0, catIdxAll, "", "", false
	}
	action := parts[2]
	switch action {
	case "noop":
		return id, 0, catIdxAll, "", "noop", true
	case "info":
		return id, 0, catIdxAll, "", "info", true
	case "open":
		return id, 1, catIdxAll, "", "open", true
	case "back":
		return id, 0, catIdxAll, "", "back", true
	}

	// 新格式: openc{idx}
	if strings.HasPrefix(action, "openc") {
		idxStr := strings.TrimPrefix(action, "openc")
		idx, err := strconv.Atoi(idxStr)
		if err != nil || idx < 0 {
			return "", 0, catIdxAll, "", "", false
		}
		return id, 1, idx, "", "open", true
	}

	// 兼容旧格式: open_{categoryName}
	if strings.HasPrefix(action, "open_") {
		cat := strings.TrimPrefix(action, "open_")
		return id, 1, catIdxAll, cat, "open", true
	}

	// 新格式: {page}c{idx}
	if i := strings.LastIndexByte(action, 'c'); i > 0 {
		pStr := action[:i]
		idxStr := action[i+1:]
		p, err1 := strconv.Atoi(pStr)
		idx, err2 := strconv.Atoi(idxStr)
		if err1 == nil && err2 == nil && p >= 1 && idx >= 0 {
			return id, p, idx, "", "page", true
		}
	}

	// 兼容旧格式: {page}_{categoryName}
	if idx := strings.IndexByte(action, '_'); idx > 0 {
		pStr := action[:idx]
		cat := action[idx+1:]
		p, err := strconv.Atoi(pStr)
		if err == nil && p >= 1 && cat != "" {
			return id, p, catIdxAll, cat, "page", true
		}
	}

	p, err := strconv.Atoi(action)
	if err != nil || p < 1 {
		return "", 0, catIdxAll, "", "", false
	}
	return id, p, catIdxAll, "", "page", true
}

// resolveCallbackCategory 将 callback 中的 catIdx / 遗留分类名解析为真实分类名。
func resolveCallbackCategory(batchID string, catIdx int, legacyName string) string {
	if catIdx >= 0 {
		return ResolveCategoryByIndex(batchID, catIdx)
	}
	return strings.TrimSpace(legacyName)
}
