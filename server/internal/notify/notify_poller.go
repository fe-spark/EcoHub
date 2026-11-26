package notify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	maxCallbackWorkers = 8
	callbackDBTimeout  = 6 * time.Second
)

type pollerInstance struct {
	cancel   context.CancelFunc
	pollDone chan struct{}
	done     chan struct{}
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
		// 等待旧 getUpdates 循环退出再启动新轮询，避免同 Token 并发拉取触发 409 冲突。
		select {
		case <-old.pollDone:
		case <-old.done:
		case <-time.After(5 * time.Second):
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
		cancel:   cancel,
		pollDone: make(chan struct{}),
		done:     make(chan struct{}),
	}
	activePoller = inst
	activeBotToken = token
	activeProxyURL = proxyURL
	pollerMu.Unlock()

	go func(p *pollerInstance, tok string) {
		defer close(p.done)
		runTelegramPoller(ctx, tok, p.pollDone)
	}(inst, token)
}

func pollerOffsetKey(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return config.NotifyBatchCachePrefix + ":offset:" + hex.EncodeToString(sum[:8])
}

func loadPollerOffset(ctx context.Context, token string) int64 {
	if db.Rdb == nil {
		return 0
	}
	opCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	val, err := db.Rdb.Get(opCtx, pollerOffsetKey(token)).Result()
	if err != nil {
		return 0
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func savePollerOffset(ctx context.Context, token string, offset int64) {
	if db.Rdb == nil || offset <= 0 {
		return
	}
	opCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = db.Rdb.Set(opCtx, pollerOffsetKey(token), offset, pollerOffsetTTL).Err()
}

func runTelegramPoller(ctx context.Context, token string, pollDone chan struct{}) {
	offset := loadPollerOffset(ctx, token)
	var errStreak int
	callbackSem := make(chan struct{}, maxCallbackWorkers)
	var callbackWg sync.WaitGroup
	defer callbackWg.Wait()
	defer close(pollDone)

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
		}
		if offset > prevOffset {
			savePollerOffset(ctx, token, offset)
		}
		// 并发处理回调，避免单个大站点查库或慢代理阻塞整个 getUpdates 轮询与后续点击响应
		for _, u := range updates {
			if u.CallbackQuery == nil {
				continue
			}
			cq := u.CallbackQuery
			select {
			case callbackSem <- struct{}{}:
				callbackWg.Add(1)
				go func(cb *tgCallbackQuery) {
					defer func() {
						callbackWg.Done()
						<-callbackSem
						if r := recover(); r != nil {
							syslog.Errorf("[Notify] Telegram 回调处理异常: %v", r)
						}
					}()
					handleNotifyCallback(ctx, token, cb)
				}(cq)
			case <-ctx.Done():
				return
			}
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

// parseNotifyCallbackCmd 解析内联按钮 callback_data：
// - nfp:{batchId}:noop:0 / info:{srcIdx} / s:{srcIdx} / sb:{srcIdx} / sp:{sitePage} / sc:{srcIdx}:{catIdx}:{page}
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
	case "noop":
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
	return notifyCallbackCmd{}, false
}

func isAllowedNotifyChat(chatIDs []string, chatID, threadID int64) bool {
	wantChat := strconv.FormatInt(chatID, 10)
	for _, raw := range chatIDs {
		cid, tid, err := parseChatTarget(raw)
		if err != nil || cid != wantChat {
			continue
		}
		if tid == 0 || tid == threadID {
			return true
		}
	}
	return false
}

func handleNotifyCallback(parentCtx context.Context, token string, cb *tgCallbackQuery) {
	if cb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parentCtx, 15*time.Second)
	defer cancel()

	// 独立短超时回答回调，避免 editTelegramMessage 超时取消 ctx 后客户端按钮一直转圈
	answerCallback := func(text string, showAlert bool) {
		ansCtx, ansCancel := context.WithTimeout(parentCtx, 5*time.Second)
		defer ansCancel()
		if err := answerTelegramCallback(ansCtx, token, cb.ID, text, showAlert); err != nil {
			log.Printf("[Notify] answerCallbackQuery 失败 data=%q err=%v", cb.Data, err)
		}
	}

	if cb.Message == nil || cb.Message.Chat == nil {
		answerCallback("无法定位原消息", true)
		return
	}
	cfg, cfgErr := GetNotifyConfig()
	if cfgErr != nil || !cfg.Enabled || !isAllowedNotifyChat(cfg.ChatIDs, cb.Message.Chat.ID, cb.Message.MessageThreadID) {
		answerCallback("当前会话无权操作", true)
		return
	}

	cmd, ok := parseNotifyCallbackCmd(cb.Data)
	if !ok {
		answerCallback("无效操作", false)
		return
	}

	sess, err := LoadNotifyBatchSessionContext(ctx, cmd.batchID)
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
		default:
			totalStored := 0
			for _, src := range sess.Sources {
				totalStored += src.FilmsTotal
			}
			answerCallback(fmt.Sprintf("共 %d 个站点，入库 %d 部", len(sess.Sources), totalStored), false)
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
		markup := buildSourceCategoryKeyboard(sess.BatchID, cmd.srcIdx, src.Categories, src.AllCount)
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
		markup := buildSourceCategoryKeyboard(sess.BatchID, cmd.srcIdx, src.Categories, src.AllCount)
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
		if cmd.catIdx < -1 || cmd.catIdx >= len(sess.Sources[cmd.srcIdx].Categories) {
			answerCallback("分类不存在", true)
			return
		}
		dbCtx, dbCancel := context.WithTimeout(ctx, callbackDBTimeout)
		text, curPage, totalPages, err := formatSourceFilmListPageHTML(dbCtx, sess, cmd.srcIdx, cmd.catIdx, cmd.page)
		dbCancel()
		if err != nil {
			log.Printf("[Notify] 构建站点片单失败 srcIdx=%d catIdx=%d page=%d err=%v", cmd.srcIdx, cmd.catIdx, cmd.page, err)
			if errors.Is(err, context.DeadlineExceeded) {
				answerCallback("片单查询超时，请稍后重试", true)
			} else {
				answerCallback("片单加载失败", false)
			}
			return
		}
		markup := buildSourcePagedKeyboard(sess.BatchID, cmd.srcIdx, cmd.catIdx, curPage, totalPages)
		if err := editTelegramMessage(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil && !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] 站点片单翻页更新失败: %v", err)
			answerCallback("翻页失败", false)
			return
		}
		answerCallback(fmt.Sprintf("第 %d/%d 页", curPage, totalPages), false)
		return
	}
	answerCallback("无效操作", false)
}
