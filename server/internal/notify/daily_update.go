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

const (
	dailyCallbackPrefix = "ndu"
	dailySessionPref    = ":Notify:DailyPage:"
	dailySessionTTL     = 12 * time.Hour
)

// dailySession 近 24 小时采集变更聚合视图。
// mid 来源与采集概要进列表一致：各批次 notify_change_mid。
type dailySession struct {
	SiteName   string              `json:"siteName"`
	PageSize   int                 `json:"pageSize"`
	AllMids    []int64             `json:"allMids"`
	Sources    map[string]string   `json:"sources"` // mid 字符串 → 源名
	Cats       []CategoryCountItem `json:"cats"`
	CatMids    [][]int64           `json:"catMids"` // 与 Cats 对齐
	FromLabel  string              `json:"fromLabel"`
	UntilLabel string              `json:"untilLabel"`
	// 兼容旧会话 JSON（自然日字段，可忽略）
	DayLabel   string `json:"dayLabel,omitempty"`
	PromptText string `json:"promptText"` // 返回分类时还原
}

func handleDailyUpdateCommand(token, chatID, chatType string) {
	now := time.Now()
	from, to := Rolling24hWindow(now)
	items, err := LoadChangeMidsBetween(from, to, 0)
	if err != nil {
		log.Printf("[Notify] 每日更新加载失败: %v", err)
		_ = replyTextWithMenu(token, chatID, chatType, "<b>📅 每日更新</b>\n加载失败，请稍后重试。")
		return
	}
	if len(items) == 0 {
		_ = replyTextWithMenu(token, chatID, chatType, "<b>📅 每日更新</b>\n近 24 小时暂无更新")
		return
	}

	pageSize := clampPageSize(GetConfig().MaxFilmsInMessage)
	cats, catMids, err := BuildCategoryPlanForMids(items)
	if err != nil {
		log.Printf("[Notify] 每日更新分类加载失败: %v", err)
		_ = replyTextWithMenu(token, chatID, chatType, "<b>📅 每日更新</b>\n加载失败，请稍后重试。")
		return
	}
	sources := make(map[string]string, len(items))
	allMids := make([]int64, 0, len(items))
	for _, it := range items {
		allMids = append(allMids, it.Mid)
		if it.SourceName != "" {
			sources[strconv.FormatInt(it.Mid, 10)] = it.SourceName
		}
	}

	loc := notifyCST()
	const ts = "01-02 15:04"
	sess := dailySession{
		SiteName:   siteName(),
		PageSize:   pageSize,
		AllMids:    allMids,
		Sources:    sources,
		Cats:       cats,
		CatMids:    catMids,
		FromLabel:  from.In(loc).Format(ts),
		UntilLabel: to.In(loc).Format(ts),
	}
	sess.PromptText = formatDailyCategoryPrompt(sess)

	sessionID, err := saveDailySession(sess)
	if err != nil {
		log.Printf("[Notify] 每日更新会话保存失败: %v", err)
		// Redis 不可用时仍可发分类入口，但无法翻页；降级为一次性列表第一页
		_ = sendDailyListWithoutSession(token, chatID, sess, catIdxAll)
		return
	}

	markup := buildCategoryKeyboard(dailyCallbackPrefix, sessionID, sess.Cats)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.sendMessageWithMarkup(ctx, token, chatID, sess.PromptText, markup); err != nil {
		log.Printf("[Notify] 每日更新发送失败 chat=%s err=%v", chatID, err)
		_ = replyTextWithMenu(token, chatID, chatType, "发送失败，请稍后重试。")
	}
}

func formatDailyCategoryPrompt(sess dailySession) string {
	return fmt.Sprintf(
		"<b>%s 每日更新</b>\n🕒 近 24 小时 %s – %s",
		formatTitlePrefix(sess.SiteName),
		html.EscapeString(sess.FromLabel),
		html.EscapeString(sess.UntilLabel),
	)
}

func sendDailyListWithoutSession(token, chatID string, sess dailySession, catIdx int) error {
	chunk, total, start, end, page := dailyPageChunk(sess, catIdx, 1)
	text := formatDailyListPage(sess, catIdx, page, chunk, total, start, end)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.sendMessage(ctx, token, chatID, text)
}

func dailySourceOf(sess dailySession, mid int64) string {
	if sess.Sources == nil {
		return ""
	}
	return sess.Sources[strconv.FormatInt(mid, 10)]
}

func dailyMidsForCat(sess dailySession, catIdx int) []int64 {
	if catIdx < 0 {
		return sess.AllMids
	}
	if catIdx >= len(sess.CatMids) {
		return nil
	}
	return sess.CatMids[catIdx]
}

func dailyCatName(sess dailySession, catIdx int) string {
	if catIdx < 0 || catIdx >= len(sess.Cats) {
		return ""
	}
	return sess.Cats[catIdx].CategoryName
}

func dailyPageChunk(sess dailySession, catIdx, page int) (chunk []ChangeMidItem, total, start, end, pageOut int) {
	mids := dailyMidsForCat(sess, catIdx)
	total = len(mids)
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
	chunk = make([]ChangeMidItem, 0, end-start)
	for _, mid := range mids[start:end] {
		chunk = append(chunk, ChangeMidItem{Mid: mid, SourceName: dailySourceOf(sess, mid)})
	}
	return chunk, total, start, end, page
}

func formatDailyListPage(sess dailySession, catIdx, page int, chunk []ChangeMidItem, total, start, end int) string {
	filmSess := FilmPageSession{
		SiteName: sess.SiteName,
		PageSize: sess.PageSize,
		Total:    total,
	}
	return formatFilmListPageWithChunkCategory(filmSess, page, chunk, total, start, end, dailyCatName(sess, catIdx), "每日更新列表")
}

func newDailySessionID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()%1e12)
	}
	return hex.EncodeToString(b[:])
}

func dailyRedisKey(sessionID string) string {
	return config.RedisKeyPrefix + dailySessionPref + sessionID
}

func saveDailySession(sess dailySession) (string, error) {
	if db.Rdb == nil {
		return "", fmt.Errorf("redis 未就绪")
	}
	if sess.PageSize <= 0 {
		sess.PageSize = model.DefaultMaxFilmsInMessage
	}
	id := newDailySessionID()
	raw, err := json.Marshal(sess)
	if err != nil {
		return "", err
	}
	if err := db.Rdb.Set(db.Cxt, dailyRedisKey(id), raw, dailySessionTTL).Err(); err != nil {
		return "", err
	}
	return id, nil
}

func loadDailySession(sessionID string) (dailySession, error) {
	var sess dailySession
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sess, fmt.Errorf("empty session")
	}
	if db.Rdb == nil {
		return sess, fmt.Errorf("redis 未就绪")
	}
	data, err := db.Rdb.Get(db.Cxt, dailyRedisKey(sessionID)).Result()
	if err != nil {
		return sess, err
	}
	if err := json.Unmarshal([]byte(data), &sess); err != nil {
		return sess, err
	}
	if sess.PageSize <= 0 {
		sess.PageSize = model.DefaultMaxFilmsInMessage
	}
	_ = db.Rdb.Expire(db.Cxt, dailyRedisKey(sessionID), dailySessionTTL).Err()
	return sess, nil
}

func handleDailyPageCallback(token string, cb *telegramCallback) {
	if cb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sessionID, page, catIdx, _, kind, ok := parsePagedCallback(dailyCallbackPrefix, cb.Data)
	if !ok {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "无效操作", false)
		return
	}
	sess, err := loadDailySession(sessionID)
	if err != nil {
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "已过期，请重新点每日更新", true)
		return
	}

	switch kind {
	case "noop":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "没有更多页了", false)
		return
	case "info":
		_ = client.answerCallbackQuery(ctx, token, cb.ID, fmt.Sprintf("共 %d 条", len(sess.AllMids)), false)
		return
	case "back":
		if cb.Message == nil || cb.Message.Chat == nil {
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "无法定位消息", true)
			return
		}
		chatID := strconv.FormatInt(cb.Message.Chat.ID, 10)
		text := strings.TrimSpace(sess.PromptText)
		if text == "" {
			text = formatDailyCategoryPrompt(sess)
		}
		markup := buildCategoryKeyboard(dailyCallbackPrefix, sessionID, sess.Cats)
		if err := client.editMessageText(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil {
			if !strings.Contains(err.Error(), "message is not modified") {
				log.Printf("[Notify] daily back edit 失败: %v", err)
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
	chunk, total, start, end, page := dailyPageChunk(sess, catIdx, page)
	ps := sess.PageSize
	if ps <= 0 {
		ps = model.DefaultMaxFilmsInMessage
	}
	totalPages := 1
	if total > 0 {
		totalPages = (total + ps - 1) / ps
	}
	text := formatDailyListPage(sess, catIdx, page, chunk, total, start, end)
	markup := buildPagedKeyboardCategory(dailyCallbackPrefix, sessionID, catIdx, page, totalPages, true)
	if err := client.editMessageText(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil {
		if !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] daily list edit 失败: %v", err)
			_ = client.answerCallbackQuery(ctx, token, cb.ID, "翻页失败", true)
			return
		}
	}
	hint := "每日更新"
	if name := dailyCatName(sess, catIdx); name != "" {
		hint = fmt.Sprintf("%s · 第 %d/%d 页", name, page, totalPages)
	} else if kind == "page" {
		hint = fmt.Sprintf("第 %d/%d 页", page, totalPages)
	}
	_ = client.answerCallbackQuery(ctx, token, cb.ID, hint, false)
}
