package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
)

// 影片列分页会话（Redis），供 Telegram 内联键盘上一页/下一页使用。
const (
	filmPageSessionTTL   = 48 * time.Hour
	filmPageSessionPref  = ":Notify:FilmPage:"
	// callback_data 限制 64 字节：nfp:{sid}:{page}
	callbackPrefix = "nfp"
)

// FilmPageItem 可分页的一条影片明细。
type FilmPageItem struct {
	SourceName string `json:"sn"`
	Grade      int    `json:"g"`
	Mid        int64  `json:"m"`
	Name       string `json:"n"`
}

// FilmPageSession 一次采集摘要对应的影片列表会话。
type FilmPageSession struct {
	SiteName   string         `json:"siteName"`
	PageSize   int            `json:"pageSize"`
	TotalCount int            `json:"totalCount"` // 累计变更数（可能 > len(Items)）
	Items      []FilmPageItem `json:"items"`
}

func filmPageRedisKey(sessionID string) string {
	return config.RedisKeyPrefix + filmPageSessionPref + sessionID
}

func newSessionID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()%1e12)
	}
	return hex.EncodeToString(b[:])
}

// saveFilmPageSession 持久化列表会话，返回 sessionID。
func saveFilmPageSession(sess FilmPageSession) (string, error) {
	if sess.PageSize <= 0 {
		sess.PageSize = 30
	}
	if sess.PageSize > 80 {
		sess.PageSize = 80
	}
	id := newSessionID()
	raw, err := json.Marshal(sess)
	if err != nil {
		return "", err
	}
	if err := db.Rdb.Set(db.Cxt, filmPageRedisKey(id), raw, filmPageSessionTTL).Err(); err != nil {
		return "", err
	}
	return id, nil
}

func loadFilmPageSession(sessionID string) (FilmPageSession, error) {
	var sess FilmPageSession
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sess, fmt.Errorf("empty session")
	}
	data, err := db.Rdb.Get(db.Cxt, filmPageRedisKey(sessionID)).Result()
	if err != nil {
		return sess, err
	}
	if err := json.Unmarshal([]byte(data), &sess); err != nil {
		return sess, err
	}
	if sess.PageSize <= 0 {
		sess.PageSize = 30
	}
	// 触摸 TTL
	_ = db.Rdb.Expire(db.Cxt, filmPageRedisKey(sessionID), filmPageSessionTTL).Err()
	return sess, nil
}

func (s FilmPageSession) totalPages() int {
	n := len(s.Items)
	if n == 0 {
		return 0
	}
	ps := s.PageSize
	if ps <= 0 {
		ps = 30
	}
	return (n + ps - 1) / ps
}

// formatFilmListPage 渲染某一页列表正文（1-based page）。
func formatFilmListPage(sess FilmPageSession, page int) string {
	totalPages := sess.totalPages()
	if totalPages == 0 {
		return fmt.Sprintf("<b>%s 影片列表</b>\n<i>暂无影片明细</i>\n", formatTitlePrefix(sess.SiteName))
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	ps := sess.PageSize
	if ps <= 0 {
		ps = 30
	}
	start := (page - 1) * ps
	end := start + ps
	if end > len(sess.Items) {
		end = len(sess.Items)
	}
	chunk := sess.Items[start:end]

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 影片列表</b>\n", formatTitlePrefix(sess.SiteName))
	fmt.Fprintf(&b, "<b>📄 第 %d/%d 页</b> · 本页 <b>%d</b> 条 · 序号 <code>%d–%d</code> / <b>%d</b>\n",
		page, totalPages, len(chunk), start+1, end, len(sess.Items))
	if sess.TotalCount > len(sess.Items) {
		fmt.Fprintf(&b, "<i>累计变更约 %d 部（明细最多保留 %d）</i>\n", sess.TotalCount, len(sess.Items))
	}

	lastSrc := ""
	for _, it := range chunk {
		srcKey := it.SourceName + "|" + strconv.Itoa(it.Grade)
		if srcKey != lastSrc {
			lastSrc = srcKey
			fmt.Fprintf(&b, "\n<b>▸ %s</b> <code>(%s)</code>\n",
				html.EscapeString(it.SourceName),
				html.EscapeString(gradeLabel(it.Grade)),
			)
		}
		b.WriteString(formatFilmLine(model.FilmNotifyItem{Mid: it.Mid, Name: it.Name}))
	}
	return b.String()
}

// buildPageKeyboard 构建上一页 / 页码 / 下一页 内联按钮。
// page 为 1-based。
func buildPageKeyboard(sessionID string, page, totalPages int) *InlineKeyboardMarkup {
	if totalPages <= 0 {
		return nil
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	row := make([]InlineKeyboardButton, 0, 3)
	// 上一页
	if page > 1 {
		row = append(row, InlineKeyboardButton{
			Text:         "◀ 上一页",
			CallbackData: fmt.Sprintf("%s:%s:%d", callbackPrefix, sessionID, page-1),
		})
	} else {
		row = append(row, InlineKeyboardButton{
			Text:         "·",
			CallbackData: fmt.Sprintf("%s:%s:noop", callbackPrefix, sessionID),
		})
	}
	// 页码指示
	row = append(row, InlineKeyboardButton{
		Text:         fmt.Sprintf("%d / %d", page, totalPages),
		CallbackData: fmt.Sprintf("%s:%s:info", callbackPrefix, sessionID),
	})
	// 下一页
	if page < totalPages {
		row = append(row, InlineKeyboardButton{
			Text:         "下一页 ▶",
			CallbackData: fmt.Sprintf("%s:%s:%d", callbackPrefix, sessionID, page+1),
		})
	} else {
		row = append(row, InlineKeyboardButton{
			Text:         "·",
			CallbackData: fmt.Sprintf("%s:%s:noop", callbackPrefix, sessionID),
		})
	}
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{row}}
}

// parsePageCallback 解析 nfp:{sid}:{page|noop|info}
func parsePageCallback(data string) (sessionID string, page int, kind string, ok bool) {
	data = strings.TrimSpace(data)
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != callbackPrefix {
		return "", 0, "", false
	}
	sessionID = parts[1]
	if sessionID == "" {
		return "", 0, "", false
	}
	switch parts[2] {
	case "noop":
		return sessionID, 0, "noop", true
	case "info":
		return sessionID, 0, "info", true
	default:
		p, err := strconv.Atoi(parts[2])
		if err != nil || p < 1 {
			return "", 0, "", false
		}
		return sessionID, p, "page", true
	}
}

// buildFilmPageSessionFromPayload 从采集摘要载荷构建可分页会话。
func buildFilmPageSessionFromPayload(payload model.CollectBatchNotifyPayload, pageSize int) FilmPageSession {
	if pageSize <= 0 {
		pageSize = 30
	}
	items := make([]FilmPageItem, 0)
	for _, src := range payload.Sources {
		for _, f := range src.Films {
			items = append(items, FilmPageItem{
				SourceName: src.SourceName,
				Grade:      src.Grade,
				Mid:        f.Mid,
				Name:       f.Name,
			})
		}
	}
	return FilmPageSession{
		SiteName:   payload.SiteName,
		PageSize:   pageSize,
		TotalCount: payload.TotalFilms,
		Items:      items,
	}
}

func handleFilmPageCallback(token string, cb *telegramCallback) {
	if cb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sessionID, page, kind, ok := parsePageCallback(cb.Data)
	if !ok {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "无效操作", false)
		return
	}

	sess, err := loadFilmPageSession(sessionID)
	if err != nil {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "列表已过期，请重新采集", true)
		return
	}
	totalPages := sess.totalPages()

	switch kind {
	case "noop":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "没有更多页了", false)
		return
	case "info":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, fmt.Sprintf("共 %d 页 · %d 条", totalPages, len(sess.Items)), false)
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
	text := formatFilmListPage(sess, page)
	markup := buildPageKeyboard(sessionID, page, totalPages)
	if err := client.editMessageText(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil {
		// 内容相同会报 message is not modified，忽略
		if !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] editMessageText 失败: %v", err)
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "翻页失败", true)
			return
		}
	}
	_ = client.answerCallbackQuery(ctx, token, cb.ID, fmt.Sprintf("第 %d/%d 页", page, totalPages), false)
}
