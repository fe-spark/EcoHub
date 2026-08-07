package notify

import (
	"fmt"
	"strconv"
	"strings"
)

// buildPagedKeyboard 分页内联键盘：上一页/页码/下一页；withBack 时追加「返回概要」
//（更新列表用；搜索列表不返回概要）。播放跳转靠正文片名 HTML 链接，不再叠 URL 按钮。
func buildPagedKeyboard(prefix, sessionID string, page, totalPages int, withBack bool) *InlineKeyboardMarkup {
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
				CallbackData: fmt.Sprintf("%s:%s:%d", prefix, sessionID, page-1),
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
				CallbackData: fmt.Sprintf("%s:%s:%d", prefix, sessionID, page+1),
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
			Text:         "🔙 返回概要",
			CallbackData: fmt.Sprintf("%s:%s:back", prefix, sessionID),
		}})
	}
	if len(rows) == 0 {
		return nil
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// parsePagedCallback 解析分页回调数据 "{prefix}:{id}:{page|noop|info|open|back}"。
// open/back 仅更新列表键盘生成；搜索键盘不会生成，收到时由调用方按无效处理。
func parsePagedCallback(prefix, data string) (id string, page int, kind string, ok bool) {
	data = strings.TrimSpace(data)
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != prefix {
		return "", 0, "", false
	}
	id = parts[1]
	if id == "" {
		return "", 0, "", false
	}
	switch parts[2] {
	case "noop":
		return id, 0, "noop", true
	case "info":
		return id, 0, "info", true
	case "open":
		return id, 1, "open", true
	case "back":
		return id, 0, "back", true
	default:
		p, err := strconv.Atoi(parts[2])
		if err != nil || p < 1 {
			return "", 0, "", false
		}
		return id, p, "page", true
	}
}
