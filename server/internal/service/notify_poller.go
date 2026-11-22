package service

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
	"server/internal/repository"
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
	cfg, err := repository.GetNotifyConfig()
	if err != nil {
		syslog.Errorf("[Notify] 读取通知配置失败，保持轮询状态: %v", err)
		return
	}
	token := ""
	if cfg.Enabled {
		token = strings.TrimSpace(cfg.BotToken)
	}
	ok, proxyURL, err := repository.ResolveNotifyProxy()
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

// parseNotifyCallback 解析内联按钮 callback_data：nfp:{batchId}:{catIdx|action}:{page}
func parseNotifyCallback(data string) (batchID, action string, catIdx, page int, ok bool) {
	parts := strings.Split(strings.TrimSpace(data), ":")
	if len(parts) != 4 || parts[0] != telegramCallbackPref {
		return "", "", -1, 0, false
	}
	batchID = strings.TrimSpace(parts[1])
	if batchID == "" {
		return "", "", -1, 0, false
	}
	third := strings.TrimSpace(parts[2])
	switch third {
	case "noop", "info", "back":
		return batchID, third, -1, 0, true
	}
	cIdx, err1 := strconv.Atoi(third)
	p, err2 := strconv.Atoi(strings.TrimSpace(parts[3]))
	if err1 != nil || err2 != nil || p < 1 {
		return "", "", -1, 0, false
	}
	return batchID, "page", cIdx, p, true
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

	batchID, action, catIdx, page, ok := parseNotifyCallback(cb.Data)
	if !ok {
		answerCallback("无效操作", false)
		return
	}

	sess, err := repository.LoadNotifyBatchSession(batchID)
	if err != nil {
		answerCallback("该批次记录已过期", true)
		return
	}

	switch action {
	case "noop":
		answerCallback("已经到底啦", false)
		return
	case "info":
		answerCallback(fmt.Sprintf("本次共更新 %d 部影片", len(sess.AllFilms)), false)
		return
	case "back":
		if cb.Message == nil || cb.Message.Chat == nil {
			answerCallback("无法定位原消息", true)
			return
		}
		chatID := strconv.FormatInt(cb.Message.Chat.ID, 10)
		markup := buildCategoryKeyboard(sess.BatchID, sess.Categories, len(sess.AllFilms))
		if err := editTelegramMessage(ctx, token, chatID, cb.Message.MessageID, sess.OverviewHTML, markup); err != nil && !strings.Contains(err.Error(), "message is not modified") {
			log.Printf("[Notify] 返回汇总失败: %v", err)
			answerCallback("返回失败", false)
			return
		}
		answerCallback("已返回采集汇总", false)
		return
	}

	if cb.Message == nil || cb.Message.Chat == nil {
		answerCallback("无法定位原消息", true)
		return
	}
	chatID := strconv.FormatInt(cb.Message.Chat.ID, 10)
	text, totalPages, err := formatFilmListPageHTML(sess, catIdx, page)
	if err != nil {
		log.Printf("[Notify] 读取站点配置失败: %v", err)
		answerCallback("翻页失败", false)
		return
	}
	markup := buildPagedKeyboard(sess.BatchID, catIdx, page, totalPages)
	if err := editTelegramMessage(ctx, token, chatID, cb.Message.MessageID, text, markup); err != nil && !strings.Contains(err.Error(), "message is not modified") {
		log.Printf("[Notify] 翻页更新失败: %v", err)
		answerCallback("翻页失败", false)
		return
	}
	answerCallback(fmt.Sprintf("第 %d/%d 页", page, totalPages), false)
}
