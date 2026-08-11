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
	"unicode/utf8"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	filmrepo "server/internal/repository/film"
)

const (
	searchCallbackPrefix = "nsr"
	searchSessionPref    = ":Notify:SearchPage:"
	searchPageSize       = 10
	searchMaxFetch       = 200
	searchSessionTTL     = 48 * time.Hour
)

// searchSession 搜索结果（条数有限，仍用 Redis 存 mid 列表）。
type searchSession struct {
	SiteName string  `json:"siteName"`
	PageSize int     `json:"pageSize"`
	Mids     []int64 `json:"mids"`
	Keyword  string  `json:"keyword"`
	HitTotal int     `json:"hitTotal"`
}

func handleBotMessage(token string, msg *telegramMessage) {
	if msg == nil || msg.Chat == nil {
		return
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}
	chatID := strconv.FormatInt(msg.Chat.ID, 10)
	cmd, args := parseBotCommand(text)
	if cmd == "" {
		return
	}

	if !isAllowedChat(chatID, msg.Chat.Username) {
		log.Printf("[Notify] 忽略指令 chat=%s cmd=/%s（不在通知 Chat ID 列表）", chatID, cmd)
		_ = replyText(token, chatID,
			fmt.Sprintf("当前会话 Chat ID 为 <code>%s</code>，未在后台「通知设置 → 通信连接」的 Chat ID 列表中，无法使用指令。\n请把该 ID 加入配置并保存。",
				html.EscapeString(chatID)))
		return
	}

	switch cmd {
	case "start", "help":
		_ = replyText(token, chatID,
			"<b>EcoHub Bot</b>\n"+
				"<code>/search 关键词</code> — 搜索影片\n"+
				"<code>/s 关键词</code> — 同上")
	case "search", "s":
		handleSearchCommand(token, chatID, args)
	default:
		_ = replyText(token, chatID,
			"<b>可用指令</b>\n"+
				"<code>/search 关键词</code> — 搜索影片\n"+
				"<code>/s 关键词</code> — 同上")
	}
}

// isAllowedChat 校验 chat 是否在通知白名单。数值 chatID 与 @username 任一匹配即通过：
// Bot 回调/指令携带的 chat 数值 ID 是权威标识；@username 用于配置侧直观表达，靠
// update 中的 chat.username 兜底匹配。
func isAllowedChat(chatID, chatUsername string) bool {
	return chatAllowed(GetConfig(), chatID, chatUsername)
}

func chatAllowed(cfg model.NotifyConfig, chatID, chatUsername string) bool {
	uname := strings.TrimSpace(chatUsername)
	if uname != "" {
		uname = "@" + strings.TrimPrefix(uname, "@")
	}
	// 与发送路由同源：走 effectiveTargets（Targets 优先，空则回落 ChatIDs）
	for _, t := range effectiveTargets(cfg) {
		if !t.Enabled {
			continue
		}
		id := strings.TrimSpace(t.ChatID)
		if id == "" {
			continue
		}
		if id == chatID {
			return true
		}
		if uname != "" && id == uname {
			return true
		}
	}
	return false
}

func parseBotCommand(text string) (cmd, args string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	rest := strings.TrimSpace(text[1:])
	if rest == "" {
		return "", ""
	}
	sp := strings.IndexAny(rest, " \t\n")
	head := rest
	if sp >= 0 {
		head = rest[:sp]
		args = strings.TrimSpace(rest[sp+1:])
	}
	if at := strings.IndexByte(head, '@'); at >= 0 {
		head = head[:at]
	}
	return strings.ToLower(strings.TrimSpace(head)), args
}

func replyText(token, chatID, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.sendMessage(ctx, token, chatID, text)
}

func handleSearchCommand(token, chatID, keyword string) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		_ = replyText(token, chatID,
			"<b>🔍 搜索影片</b>\n用法：<code>/search 关键词</code>\n例如：<code>/search 流浪地球</code>")
		return
	}
	if utf8.RuneCountInString(keyword) > 64 {
		_ = replyText(token, chatID, "关键词过长，请缩短后再试（最多 64 字）")
		return
	}

	page := &dto.Page{PageSize: searchMaxFetch, Current: 1}
	version := filmrepo.GetActiveReadModelVersion()
	snaps := filmrepo.SearchSnapshotsByKeywordFast(version, keyword, page)
	mids := make([]int64, 0, len(snaps))
	for _, s := range snaps {
		if s.Mid > 0 {
			mids = append(mids, s.Mid)
		}
	}
	hitTotal := page.Total
	if hitTotal < len(mids) {
		hitTotal = len(mids)
	}

	site := siteName()
	if len(mids) == 0 {
		_ = replyText(token, chatID, fmt.Sprintf(
			"<b>%s 搜索</b>\n🔍 关键词：<code>%s</code>\n<i>未找到匹配影片</i>",
			formatTitlePrefix(site),
			html.EscapeString(keyword),
		))
		return
	}

	sess := searchSession{
		SiteName: site,
		PageSize: searchPageSize,
		Mids:     mids,
		Keyword:  keyword,
		HitTotal: hitTotal,
	}
	sessionID, err := saveSearchSession(sess)
	if err != nil {
		log.Printf("[Notify] 保存搜索会话失败: %v", err)
		_ = replyText(token, chatID, formatSearchListPage(sess, 1))
		return
	}

	chunk, start := searchPageMids(sess, 1)
	text := formatSearchListPageWithChunk(sess, 1, chunk, start)
	markup := buildPagedKeyboard(searchCallbackPrefix, sessionID, 1, searchTotalPages(sess), false)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.sendMessageWithMarkup(ctx, token, chatID, text, markup); err != nil {
		log.Printf("[Notify] 搜索结果发送失败 chat=%s err=%v", chatID, err)
	}
}

func newSearchSessionID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()%1e12)
	}
	return hex.EncodeToString(b[:])
}

func searchRedisKey(sessionID string) string {
	return config.RedisKeyPrefix + searchSessionPref + sessionID
}

func saveSearchSession(sess searchSession) (string, error) {
	if db.Rdb == nil {
		return "", fmt.Errorf("redis 未就绪")
	}
	if sess.PageSize <= 0 {
		sess.PageSize = searchPageSize
	}
	id := newSearchSessionID()
	raw, err := json.Marshal(sess)
	if err != nil {
		return "", err
	}
	// 搜索会话仍用 Redis（短 TTL）；更新列表在 MySQL 批次表
	if err := db.Rdb.Set(db.Cxt, searchRedisKey(id), raw, searchSessionTTL).Err(); err != nil {
		return "", err
	}
	return id, nil
}

func loadSearchSession(sessionID string) (searchSession, error) {
	var sess searchSession
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sess, fmt.Errorf("empty session")
	}
	data, err := db.Rdb.Get(db.Cxt, searchRedisKey(sessionID)).Result()
	if err != nil {
		return sess, err
	}
	if err := json.Unmarshal([]byte(data), &sess); err != nil {
		return sess, err
	}
	if sess.PageSize <= 0 {
		sess.PageSize = searchPageSize
	}
	_ = db.Rdb.Expire(db.Cxt, searchRedisKey(sessionID), searchSessionTTL).Err()
	return sess, nil
}

func searchTotalPages(sess searchSession) int {
	n := len(sess.Mids)
	if n == 0 {
		return 0
	}
	ps := sess.PageSize
	if ps <= 0 {
		ps = searchPageSize
	}
	return (n + ps - 1) / ps
}

func searchPageMids(sess searchSession, page int) (chunk []int64, start int) {
	totalPages := searchTotalPages(sess)
	if totalPages == 0 {
		return nil, 0
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	ps := sess.PageSize
	if ps <= 0 {
		ps = searchPageSize
	}
	start = (page - 1) * ps
	end := start + ps
	if end > len(sess.Mids) {
		end = len(sess.Mids)
	}
	return sess.Mids[start:end], start
}

func formatSearchListPage(sess searchSession, page int) string {
	if searchTotalPages(sess) == 0 {
		return fmt.Sprintf("<b>%s 搜索</b>\n🔍 <code>%s</code>\n<i>无结果</i>",
			formatTitlePrefix(sess.SiteName), html.EscapeString(sess.Keyword))
	}
	chunk, start := searchPageMids(sess, page)
	return formatSearchListPageWithChunk(sess, page, chunk, start)
}

// formatSearchListPageWithChunk 使用已加载的 mid 页渲染，避免回调路径重复取 mid。
func formatSearchListPageWithChunk(sess searchSession, page int, chunk []int64, start int) string {
	totalPages := searchTotalPages(sess)
	if totalPages == 0 || len(chunk) == 0 {
		return fmt.Sprintf("<b>%s 搜索</b>\n🔍 <code>%s</code>\n<i>无结果</i>",
			formatTitlePrefix(sess.SiteName), html.EscapeString(sess.Keyword))
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	meta := ResolveFilmMeta(chunk)

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 搜索结果</b>\n", formatTitlePrefix(sess.SiteName))
	fmt.Fprintf(&b, "🔍 <code>%s</code>\n", html.EscapeString(sess.Keyword))
	if sess.HitTotal > len(sess.Mids) {
		fmt.Fprintf(&b, "📄 第 <b>%d/%d</b> 页 · 本页 <b>%d</b> · 展示 <b>%d</b> / 约 <b>%d</b> 条\n",
			page, totalPages, len(chunk), len(sess.Mids), sess.HitTotal)
	} else {
		fmt.Fprintf(&b, "📄 第 <b>%d/%d</b> 页 · 本页 <b>%d</b> · 共 <b>%d</b> 条\n",
			page, totalPages, len(chunk), len(sess.Mids))
	}
	if !siteURLConfigured() {
		fmt.Fprintf(&b, "<i>未配置网站地址，无法跳转播放。请在后台「网站配置」填写公网地址。</i>\n")
	} else {
		fmt.Fprintf(&b, "<i>点片名打开播放页</i>\n")
	}
	b.WriteByte('\n')
	for i, mid := range chunk {
		line := formatFilmLine(model.FilmNotifyItem{Mid: mid, Name: meta[mid].Name})
		line = strings.TrimPrefix(line, "· ")
		b.WriteString(fmt.Sprintf("%d. ", start+i+1))
		b.WriteString(line)
	}
	return b.String()
}

func handleSearchPageCallback(token string, cb *telegramCallback) {
	if cb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sessionID, page, _, _, kind, ok := parsePagedCallback(searchCallbackPrefix, cb.Data)
	if !ok {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "无效操作", false)
		return
	}
	sess, err := loadSearchSession(sessionID)
	if err != nil {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "搜索结果已过期，请重新 /search", true)
		return
	}
	totalPages := searchTotalPages(sess)
	switch kind {
	case "noop":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "没有更多页了", false)
		return
	case "info":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, fmt.Sprintf("共 %d 页 · %d 条", totalPages, len(sess.Mids)), false)
		return
	case "open", "back":
		// 搜索键盘不生成这些按钮；伪造回调按无效处理
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "无效操作", false)
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
	chunk, start := searchPageMids(sess, page)
	text := formatSearchListPageWithChunk(sess, page, chunk, start)
	markup := buildPagedKeyboard(searchCallbackPrefix, sessionID, page, totalPages, false)
	if err := client.editMessageText(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil {
		if !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] search editMessageText 失败: %v", err)
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "翻页失败", true)
			return
		}
	}
	_ = client.answerCallbackQuery(ctx, token, cb.ID, fmt.Sprintf("第 %d/%d 页", page, totalPages), false)
}
