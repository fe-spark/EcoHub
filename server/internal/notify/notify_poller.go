package notify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/repobase"
)

const (
	pollerConflictWait = 60 * time.Second
	pollerMaxBackoff   = 30 * time.Second
	pollerOffsetTTL    = 7 * 24 * time.Hour
)

type pollerInstance struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var (
	pollerCfgMu    sync.Mutex
	pollerMu       sync.Mutex
	activePoller   *pollerInstance
	activeBotToken string
	activeProxyURL string
)

// EnsureBotPoller 根据当前通知配置与代理配置启停 Telegram getUpdates 轮询协程
func (s *NotifyService) EnsureBotPoller() {
	cfg, err := GetNotifyConfig()
	if err != nil {
		syslog.Errorf("[Notify] 读取通知配置失败，保持轮询状态: %v", err)
		return
	}
	token := ""
	if cfg.Enabled {
		token = strings.TrimSpace(cfg.BotToken)
	}
	ok, proxyURL, err := repobase.ResolveNotifyProxy()
	if err != nil {
		syslog.Errorf("[Notify] 读取通知代理失败，保持轮询状态: %v", err)
		return
	}
	if !ok {
		proxyURL = ""
	}
	ensurePollerState(token, strings.TrimSpace(proxyURL))
}

// StopBotPoller 停止 Telegram 轮询协程
func (s *NotifyService) StopBotPoller() {
	ensurePollerState("", "")
}

func ensurePollerState(token, proxyURL string) {
	pollerCfgMu.Lock()
	defer pollerCfgMu.Unlock()

	pollerMu.Lock()
	if activePoller != nil {
		select {
		case <-activePoller.done:
			activePoller = nil
			activeBotToken = ""
			activeProxyURL = ""
		default:
			if activeBotToken == token && activeProxyURL == proxyURL {
				pollerMu.Unlock()
				return
			}
		}
	}

	old := activePoller
	if old != nil {
		old.cancel()
		activePoller = nil
		activeBotToken = ""
		activeProxyURL = ""
	}
	pollerMu.Unlock()

	if old != nil {
		select {
		case <-old.done:
		case <-time.After(3 * time.Second):
		}
	}

	if token == "" {
		return
	}

	pollerMu.Lock()
	if activePoller != nil && activeBotToken == token && activeProxyURL == proxyURL {
		pollerMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	inst := &pollerInstance{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	activePoller = inst
	activeBotToken = token
	activeProxyURL = proxyURL
	pollerMu.Unlock()

	go func(p *pollerInstance, tok string) {
		defer close(p.done)
		runTelegramPoller(ctx, tok)
	}(inst, token)
}

func pollerOffsetKey(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return config.NotifyBatchCachePrefix + ":offset:" + hex.EncodeToString(sum[:8])
}

func loadPollerOffset(token string) int64 {
	if db.Rdb == nil {
		return 0
	}
	val, err := db.Rdb.Get(db.Cxt, pollerOffsetKey(token)).Result()
	if err != nil {
		return 0
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func savePollerOffset(token string, offset int64) {
	if db.Rdb == nil || offset <= 0 {
		return
	}
	_ = db.Rdb.Set(db.Cxt, pollerOffsetKey(token), offset, pollerOffsetTTL).Err()
}

func runTelegramPoller(ctx context.Context, token string) {
	offset := loadPollerOffset(token)
	var errStreak int

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, conflict, err := fetchCallbackUpdates(ctx, token, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			errStreak++
			wait := time.Duration(1<<min(errStreak, 5)) * time.Second
			if wait > pollerMaxBackoff {
				wait = pollerMaxBackoff
			}
			if conflict {
				wait = pollerConflictWait
				if errStreak == 1 {
					syslog.Warnf("[Notify] Telegram Bot getUpdates 发生 409 冲突（可能有其它实例正在轮询同一 Token），退避 %s", wait)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
				continue
			}
		}

		errStreak = 0
		prevOffset := offset
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.CallbackQuery != nil {
				handleNotifyCallback(ctx, token, u.CallbackQuery)
			}
		}
		if offset > prevOffset {
			savePollerOffset(token, offset)
		}
	}
}

func fetchCallbackUpdates(ctx context.Context, token string, offset int64) ([]tgUpdate, bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	body := map[string]any{
		"offset":          offset,
		"timeout":         20,
		"allowed_updates": []string{"callback_query"},
	}
	resp, err := callTelegramJSON(reqCtx, token, "getUpdates", body, 32*time.Second)
	if err != nil {
		return nil, resp.ErrorCode == 409 || strings.Contains(err.Error(), "409"), err
	}
	var updates []tgUpdate
	if len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, &updates); err != nil {
			return nil, false, err
		}
	}
	return updates, false, nil
}

type notifyCallbackCmd struct {
	batchID string
	action  string
	srcIdx  int
	catIdx  int
	page    int
}

// parseNotifyCallback 解析内联按钮 callback_data：
// - nfp:{batchId}:{catIdx|action}:{page}
// - nfp:{batchId}:s:{srcIdx} / sb:{srcIdx} / sp:{sitePage} / sc:{srcIdx}:{catIdx}:{page}
func parseNotifyCallback(data string) (batchID, action string, catIdx, page int, ok bool) {
	cmd, ok := parseNotifyCallbackCmd(data)
	if !ok {
		return "", "", -1, 0, false
	}
	return cmd.batchID, cmd.action, cmd.catIdx, cmd.page, true
}

func parseNotifyCallbackCmd(data string) (notifyCallbackCmd, bool) {
	parts := strings.Split(strings.TrimSpace(data), ":")
	if len(parts) < 4 || parts[0] != telegramCallbackPref {
		return notifyCallbackCmd{}, false
	}
	batchID := strings.TrimSpace(parts[1])
	if batchID == "" {
		return notifyCallbackCmd{}, false
	}
	third := strings.TrimSpace(parts[2])
	switch third {
	case "noop", "back":
		if len(parts) != 4 {
			return notifyCallbackCmd{}, false
		}
		return notifyCallbackCmd{batchID: batchID, action: third, srcIdx: -1, catIdx: -1}, true
	case "info":
		if len(parts) != 4 {
			return notifyCallbackCmd{}, false
		}
		srcIdx, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil {
			srcIdx = -1
		}
		return notifyCallbackCmd{batchID: batchID, action: "info", srcIdx: srcIdx, catIdx: -1}, true
	case "s", "sb":
		if len(parts) != 4 {
			return notifyCallbackCmd{}, false
		}
		srcIdx, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil || srcIdx < 0 {
			return notifyCallbackCmd{}, false
		}
		act := "src"
		if third == "sb" {
			act = "sback"
		}
		return notifyCallbackCmd{batchID: batchID, action: act, srcIdx: srcIdx, catIdx: -1}, true
	case "sp":
		if len(parts) != 4 {
			return notifyCallbackCmd{}, false
		}
		p, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil || p < 1 {
			return notifyCallbackCmd{}, false
		}
		return notifyCallbackCmd{batchID: batchID, action: "spage", srcIdx: -1, catIdx: -1, page: p}, true
	case "sc":
		if len(parts) != 6 {
			return notifyCallbackCmd{}, false
		}
		srcIdx, err1 := strconv.Atoi(strings.TrimSpace(parts[3]))
		catIdx, err2 := strconv.Atoi(strings.TrimSpace(parts[4]))
		p, err3 := strconv.Atoi(strings.TrimSpace(parts[5]))
		if err1 != nil || err2 != nil || err3 != nil || srcIdx < 0 || p < 1 {
			return notifyCallbackCmd{}, false
		}
		return notifyCallbackCmd{batchID: batchID, action: "spage_cat", srcIdx: srcIdx, catIdx: catIdx, page: p}, true
	}
	if len(parts) != 4 {
		return notifyCallbackCmd{}, false
	}
	cIdx, err1 := strconv.Atoi(third)
	p, err2 := strconv.Atoi(strings.TrimSpace(parts[3]))
	if err1 != nil || err2 != nil || p < 1 {
		return notifyCallbackCmd{}, false
	}
	return notifyCallbackCmd{batchID: batchID, action: "page", srcIdx: -1, catIdx: cIdx, page: p}, true
}

func handleNotifyCallback(parentCtx context.Context, token string, cb *tgCallbackQuery) {
	if cb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parentCtx, 12*time.Second)
	defer cancel()

	// 独立短超时回答回调，避免 editTelegramMessage 超时取消 ctx 后客户端按钮一直转圈
	answerCallback := func(text string, showAlert bool) {
		ansCtx, ansCancel := context.WithTimeout(parentCtx, 5*time.Second)
		defer ansCancel()
		_ = answerTelegramCallback(ansCtx, token, cb.ID, text, showAlert)
	}

	cmd, ok := parseNotifyCallbackCmd(cb.Data)
	if !ok {
		answerCallback("无效操作", false)
		return
	}

	sess, err := LoadNotifyBatchSession(cmd.batchID)
	if err != nil {
		answerCallback("该批次记录已过期", true)
		return
	}

	switch cmd.action {
	case "noop":
		answerCallback("已经到底啦", false)
		return
	case "info":
		switch {
		case cmd.srcIdx >= 0 && cmd.srcIdx < len(sess.Sources):
			src := sess.Sources[cmd.srcIdx]
			answerCallback(fmt.Sprintf("%s 本次入库 %d 部", src.SourceName, src.FilmsTotal), false)
		case cmd.srcIdx == notifyInfoAllSources:
			totalStored := 0
			for _, src := range sess.Sources {
				totalStored += src.FilmsTotal
			}
			answerCallback(fmt.Sprintf("共 %d 个站点，入库 %d 部", len(sess.Sources), totalStored), false)
		default:
			answerCallback(fmt.Sprintf("本次共更新 %d 部影片", len(sess.AllFilms)), false)
		}
		return
	}

	if cb.Message == nil || cb.Message.Chat == nil {
		answerCallback("无法定位原消息", true)
		return
	}
	chatID := strconv.FormatInt(cb.Message.Chat.ID, 10)

	switch cmd.action {
	case "src":
		if cmd.srcIdx < 0 || cmd.srcIdx >= len(sess.Sources) {
			answerCallback("站点信息不存在", true)
			return
		}
		src := sess.Sources[cmd.srcIdx]
		markup := buildSourceCategoryKeyboard(sess.BatchID, cmd.srcIdx, src.Categories, src.TotalFilmCount())
		if err := sendTelegramReplyMessage(ctx, token, chatID, cb.Message.MessageThreadID, cb.Message.MessageID, src.OverviewHTML, markup); err != nil {
			log.Printf("[Notify] 回复站点分类失败 source=%s err=%v", src.SourceName, err)
			answerCallback("回复失败", false)
			return
		}
		answerCallback(fmt.Sprintf("已回复 %s 分类", src.SourceName), false)
		return
	case "sback":
		if cmd.srcIdx < 0 || cmd.srcIdx >= len(sess.Sources) {
			answerCallback("站点信息不存在", true)
			return
		}
		src := sess.Sources[cmd.srcIdx]
		markup := buildSourceCategoryKeyboard(sess.BatchID, cmd.srcIdx, src.Categories, src.TotalFilmCount())
		if err := editTelegramMessage(ctx, token, chatID, cb.Message.MessageID, src.OverviewHTML, markup); err != nil && !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] 返回站点分类失败: %v", err)
			answerCallback("返回失败", false)
			return
		}
		answerCallback("已返回站点分类", false)
		return
	case "spage":
		markup := buildOverviewKeyboard(sess, cmd.page)
		if err := editTelegramMessage(ctx, token, chatID, cb.Message.MessageID, sess.OverviewHTML, markup); err != nil && !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] 站点翻页失败: %v", err)
			answerCallback("翻页失败", false)
			return
		}
		answerCallback(fmt.Sprintf("站点第 %d 页", cmd.page), false)
		return
	case "spage_cat":
		if cmd.srcIdx < 0 || cmd.srcIdx >= len(sess.Sources) {
			answerCallback("站点信息不存在", true)
			return
		}
		text, totalPages, err := formatSourceFilmListPageHTML(sess, cmd.srcIdx, cmd.catIdx, cmd.page)
		if err != nil {
			log.Printf("[Notify] 构建站点片单失败: %v", err)
			answerCallback("翻页失败", false)
			return
		}
		markup := buildSourcePagedKeyboard(sess.BatchID, cmd.srcIdx, cmd.catIdx, cmd.page, totalPages)
		if err := editTelegramMessage(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil && !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] 站点片单翻页更新失败: %v", err)
			answerCallback("翻页失败", false)
			return
		}
		answerCallback(fmt.Sprintf("第 %d/%d 页", cmd.page, totalPages), false)
		return
	case "back":
		markup := buildOverviewKeyboard(sess, 1)
		if err := editTelegramMessage(ctx, token, chatID, cb.Message.MessageID, sess.OverviewHTML, markup); err != nil && !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] 返回汇总失败: %v", err)
			answerCallback("返回失败", false)
			return
		}
		answerCallback("已返回采集汇总", false)
		return
	}

	text, totalPages, err := formatFilmListPageHTML(sess, cmd.catIdx, cmd.page)
	if err != nil {
		log.Printf("[Notify] 读取站点配置失败: %v", err)
		answerCallback("翻页失败", false)
		return
	}
	markup := buildPagedKeyboard(sess.BatchID, cmd.catIdx, cmd.page, totalPages)
	if err := editTelegramMessage(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil && !strings.Contains(err.Error(), "message is not modified") {
		log.Printf("[Notify] 翻页更新失败: %v", err)
		answerCallback("翻页失败", false)
		return
	}
	answerCallback(fmt.Sprintf("第 %d/%d 页", cmd.page, totalPages), false)
}
