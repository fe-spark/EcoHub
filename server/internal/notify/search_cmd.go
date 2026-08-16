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
	searchAwaitPref      = ":Notify:SearchAwait:"
	searchPageSize       = 10
	searchMaxFetch       = 200
	searchSessionTTL     = 48 * time.Hour
	searchAwaitTTL       = 5 * time.Minute

	// 底部常驻键盘按钮文案（与用户点击发送的纯文本完全一致）
	btnDailyUpdate = "📅 每日更新"
	btnSearchQuery = "🔍 搜索查询"
	btnHelp        = "❓ 帮助"
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
	userID := int64(0)
	if msg.From != nil {
		userID = msg.From.ID
	}
	cmd, args := parseBotCommand(text)

	// 白名单：指令 / 按钮 / 搜索待命态均需校验
	if !isAllowedChat(chatID, msg.Chat.Username) {
		// 未授权时仅对明确指令/按钮回应，避免群聊闲聊被刷
		if cmd == "" && !isMenuButtonText(text) {
			return
		}
		label := cmd
		if label == "" {
			label = text
		}
		log.Printf("[Notify] 忽略指令 chat=%s cmd=%s（不在通知 Chat ID 列表）", chatID, label)
		_ = replyText(token, chatID,
			fmt.Sprintf("当前会话 Chat ID 为 <code>%s</code>，未在后台「通知设置 → 通信连接」的 Chat ID 列表中，无法使用指令。\n请把该 ID 加入配置并保存。",
				html.EscapeString(chatID)))
		return
	}

	chatType := ""
	if msg.Chat != nil {
		chatType = msg.Chat.Type
	}

	// 底部常驻按钮（纯文本）
	switch text {
	case btnDailyUpdate:
		clearSearchAwait(chatID, userID)
		handleDailyUpdateCommand(token, chatID, chatType)
		return
	case btnSearchQuery:
		handleSearchPrompt(token, chatID, userID, chatType)
		return
	case btnHelp:
		clearSearchAwait(chatID, userID)
		_ = replyTextWithMenu(token, chatID, chatType, botHelpHTML())
		return
	}

	if cmd != "" {
		switch cmd {
		case "start":
			clearSearchAwait(chatID, userID)
			_ = replyTextWithMenu(token, chatID, chatType, botWelcomeHTML())
		case "help":
			clearSearchAwait(chatID, userID)
			_ = replyTextWithMenu(token, chatID, chatType, botHelpHTML())
		case "search":
			clearSearchAwait(chatID, userID)
			handleSearchCommand(token, chatID, chatType, args)
		case "daily", "updates":
			clearSearchAwait(chatID, userID)
			handleDailyUpdateCommand(token, chatID, chatType)
		default:
			clearSearchAwait(chatID, userID)
			_ = replyTextWithMenu(token, chatID, chatType, botHelpHTML())
		}
		return
	}

	// 点过「搜索查询」后的下一条纯文本视为关键词（仅私聊待命）
	if consumeSearchAwait(chatID, userID) {
		handleSearchCommand(token, chatID, chatType, text)
	}
}

func isMenuButtonText(text string) bool {
	switch strings.TrimSpace(text) {
	case btnDailyUpdate, btnSearchQuery, btnHelp:
		return true
	default:
		return false
	}
}

func mainReplyKeyboard() *ReplyKeyboardMarkup {
	return &ReplyKeyboardMarkup{
		Keyboard: [][]KeyboardButton{
			{{Text: btnDailyUpdate}, {Text: btnSearchQuery}},
			{{Text: btnHelp}},
		},
		ResizeKeyboard: true,
		IsPersistent:   true,
	}
}

func botProjectHTML() string {
	url := strings.TrimSpace(config.ProjectURL)
	if url == "" {
		return ""
	}
	label := strings.TrimPrefix(url, "https://")
	label = strings.TrimPrefix(label, "http://")
	return fmt.Sprintf("🔗 项目地址 <a href=\"%s\">%s</a>", html.EscapeString(url), html.EscapeString(label))
}

func botWelcomeHTML() string {
	return "<b>EcoHub Bot</b>\n" +
		"📅 每日更新\n" +
		"🔍 搜索查询\n" +
		"❓ 帮助\n\n" +
		"<code>/daily</code>\n" +
		"<code>/search 关键词</code>\n\n" +
		botProjectHTML()
}

func botHelpHTML() string {
	return "<b>使用说明</b>\n" +
		"📅 每日更新  <code>/daily</code>\n" +
		"🔍 搜索查询  <code>/search 关键词</code>\n" +
		"❓ 帮助\n\n" +
		botProjectHTML()
}

func isPrivateChat(chatType string) bool {
	return strings.EqualFold(strings.TrimSpace(chatType), "private")
}

// menuKeyboardForChat 仅私聊下发 ReplyKeyboard；群/超级群隐私模式收不到非 / 消息。
func menuKeyboardForChat(chatType string) *ReplyKeyboardMarkup {
	if isPrivateChat(chatType) {
		return mainReplyKeyboard()
	}
	return nil
}

func replyTextWithMenu(token, chatID, chatType, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if kb := menuKeyboardForChat(chatType); kb != nil {
		return client.sendMessageWithReplyKeyboard(ctx, token, chatID, text, kb)
	}
	return client.sendMessage(ctx, token, chatID, text)
}

func handleSearchPrompt(token, chatID string, userID int64, chatType string) {
	if !isPrivateChat(chatType) || db.Rdb == nil {
		_ = replyTextWithMenu(token, chatID, chatType, "<b>🔍 搜索查询</b>\n<code>/search 关键词</code>")
		return
	}
	if err := setSearchAwait(chatID, userID); err != nil {
		log.Printf("[Notify] 设置搜索待命失败: %v", err)
		_ = replyTextWithMenu(token, chatID, chatType, "<b>🔍 搜索查询</b>\n<code>/search 关键词</code>")
		return
	}
	_ = replyTextWithMenu(token, chatID, chatType, "<b>🔍 搜索查询</b>\n请发送关键词")
}

func searchAwaitRedisKey(chatID string, userID int64) string {
	return config.RedisKeyPrefix + searchAwaitPref + chatID + ":" + strconv.FormatInt(userID, 10)
}

func setSearchAwait(chatID string, userID int64) error {
	if db.Rdb == nil || strings.TrimSpace(chatID) == "" {
		return fmt.Errorf("redis 未就绪")
	}
	return db.Rdb.Set(db.Cxt, searchAwaitRedisKey(chatID, userID), "1", searchAwaitTTL).Err()
}

func clearSearchAwait(chatID string, userID int64) {
	if db.Rdb == nil || strings.TrimSpace(chatID) == "" {
		return
	}
	_ = db.Rdb.Del(db.Cxt, searchAwaitRedisKey(chatID, userID)).Err()
}

// consumeSearchAwait 若存在待命则删除并返回 true。
func consumeSearchAwait(chatID string, userID int64) bool {
	if db.Rdb == nil || strings.TrimSpace(chatID) == "" {
		return false
	}
	key := searchAwaitRedisKey(chatID, userID)
	n, err := db.Rdb.Del(db.Cxt, key).Result()
	return err == nil && n > 0
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
	return strings.ToLower(strings.TrimSpace(head)), stripCommandMentions(args)
}

// stripCommandMentions 去掉群聊客户端插在指令参数里的 @bot 提及。
// 例：/search 仙逆 @My_Bot、/search @My_Bot 仙逆、/search 仙逆@My_Bot
func stripCommandMentions(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	fields := strings.Fields(args)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if isTelegramMentionToken(f) {
			continue
		}
		if at := strings.LastIndexByte(f, '@'); at > 0 && isTelegramUsername(f[at+1:]) {
			f = f[:at]
			if strings.TrimSpace(f) == "" {
				continue
			}
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

func isTelegramMentionToken(s string) bool {
	if len(s) < 2 || s[0] != '@' {
		return false
	}
	return isTelegramUsername(s[1:])
}

// isTelegramUsername 匹配 Telegram 公开用户名：5–32 位，字母开头，仅字母数字下划线。
func isTelegramUsername(s string) bool {
	n := len(s)
	if n < 5 || n > 32 {
		return false
	}
	for i := 0; i < n; i++ {
		c := s[i]
		if i == 0 && !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
			return false
		}
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

func replyText(token, chatID, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.sendMessage(ctx, token, chatID, text)
}

func handleSearchCommand(token, chatID, chatType, keyword string) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		_ = replyTextWithMenu(token, chatID, chatType, "<b>🔍 搜索查询</b>\n<code>/search 关键词</code>")
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
