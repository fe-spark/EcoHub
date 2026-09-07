package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"

	"github.com/redis/go-redis/v9"
)

const (
	botPollerLockTTL      = 90 * time.Second
	botPollerStandbyWait  = 10 * time.Second
	botPollerConflictWait = 60 * time.Second
	botPollerMaxBackoff   = 30 * time.Second
)

type pollerGeneration struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var (
	pollerMu    sync.Mutex
	pollerGen   *pollerGeneration
	pollerToken string
)

// EnsureBotPoller 按已保存配置启停轮询。未启用通知或无 Token 时停止；Token 变化则重启。
func EnsureBotPoller() {
	ensureBotPoller(resolvePollerToken(GetConfig()), runBotPoller)
}

// StopBotPoller 停止当前正在运行的 Telegram Bot 轮询。
func StopBotPoller() {
	ensureBotPoller("", runBotPoller)
}

func resolvePollerToken(cfg model.NotifyConfig) string {
	if !cfg.Enabled {
		return ""
	}
	return strings.TrimSpace(cfg.BotToken)
}

func ensureBotPoller(token string, runner func(ctx context.Context, token string)) {
	for {
		pollerMu.Lock()
		if pollerGen != nil {
			select {
			case <-pollerGen.done:
				// 协程已退出，回收陈旧句柄
				pollerGen = nil
				pollerToken = ""
			default:
				if pollerToken == token {
					pollerMu.Unlock()
					return
				}
			}
		}

		old := takeStopLocked()
		pollerMu.Unlock()
		waitStopped(old)

		if token == "" {
			return
		}

		pollerMu.Lock()
		if pollerGen != nil && pollerToken == token {
			pollerMu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		gen := &pollerGeneration{
			cancel: cancel,
			done:   make(chan struct{}),
		}
		pollerGen = gen
		pollerToken = token
		pollerMu.Unlock()

		go func(g *pollerGeneration, tok string) {
			defer close(g.done)
			defer func() {
				pollerMu.Lock()
				if pollerGen == g {
					pollerGen = nil
					pollerToken = ""
				}
				pollerMu.Unlock()
			}()
			runner(ctx, tok)
		}(gen, token)
		return
	}
}

func takeStopLocked() *pollerGeneration {
	if pollerGen == nil {
		return nil
	}
	g := pollerGen
	pollerGen = nil
	pollerToken = ""
	g.cancel()
	return g
}

func waitStopped(g *pollerGeneration) {
	if g == nil {
		return
	}
	select {
	case <-g.done:
	case <-time.After(5 * time.Second):
		log.Printf("[Notify] 等待旧 Telegram Bot 轮询退出超时")
	}
}

func runBotPoller(ctx context.Context, token string) {
	owner := newPollerOwnerID()
	defer releaseBotPollerLock(owner)

	var (
		offset         int64
		backoff        = 3 * time.Second
		isLeader       = false
		loggedStandby  = false
		webhookCleared = false
	)

	log.Printf("[Notify] 启动 Telegram Bot 轮询 owner=%s", owner)
	defer log.Printf("[Notify] Telegram Bot 轮询已退出 owner=%s", owner)

	for {
		if ctx.Err() != nil {
			return
		}

		// 检查通知总开关与 Token 是否仍有效：若已关闭或 Token 变更，主动释放领导权并退出
		if tok := resolvePollerToken(GetConfig()); tok == "" || tok != token {
			log.Printf("[Notify] 检测到 Telegram 开关已关闭或 Token 变更，主动退出轮询")
			return
		}

		if !holdBotPollerLock(ctx, owner) {
			if isLeader {
				log.Printf("[Notify] 失去 Bot 轮询领导权，转为待命")
				isLeader = false
			}
			if !loggedStandby {
				log.Printf("[Notify] 已有其它 EcoHub 实例负责 Telegram Bot 轮询，本实例待命")
				loggedStandby = true
			}
			if !sleepCtx(ctx, botPollerStandbyWait) {
				return
			}
			if tok := resolvePollerToken(GetConfig()); tok == "" || tok != token {
				log.Printf("[Notify] 待命期间检测到 Telegram 开关关闭或 Token 变更，主动退出待命")
				return
			}
			continue
		}

		if !isLeader {
			log.Printf("[Notify] 本实例取得 Bot 轮询领导权 owner=%s", owner)
			isLeader = true
			loggedStandby = false
			webhookCleared = false
		}

		if !webhookCleared {
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			if err := client.deleteWebhook(cctx, token); err != nil {
				log.Printf("[Notify] deleteWebhook: %v", err)
			} else {
				webhookCleared = true
			}
			cancel()
		}

		reqCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		updates, err := client.getUpdates(reqCtx, token, offset, 25)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if isTelegramFatalAuthError(err) {
				syslog.Errorf("[Notify] Telegram Bot Token 鉴权失败或 Bot 不存在 (%v)，主动终止轮询", err)
				return
			}
			if isTelegramGetUpdatesConflict(err) {
				log.Printf("[Notify] getUpdates 冲突：同一 Bot Token 另有实例在跑。释放领导权并 %s 后重试", botPollerConflictWait)
				releaseBotPollerLock(owner)
				isLeader = false
				if !sleepCtx(ctx, botPollerConflictWait) {
					return
				}
				backoff = 3 * time.Second
				continue
			}
			if isTelegramWebhookActiveError(err) {
				log.Printf("[Notify] getUpdates 被拒：webhook 激活，重新清除后重试")
				webhookCleared = false
				if !sleepCtx(ctx, time.Second) {
					return
				}
				continue
			}
			syslog.Errorf("[Notify] getUpdates 失败: %v", err)
			if !sleepCtx(ctx, backoff) {
				return
			}
			if backoff < botPollerMaxBackoff {
				backoff *= 2
				if backoff < 3*time.Second {
					backoff = 3 * time.Second
				}
			}
			continue
		}
		backoff = 3 * time.Second

		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.CallbackQuery != nil {
				cb := u.CallbackQuery
				go dispatchCallback(token, cb)
			}
		}
	}
}

func dispatchCallback(token string, cb *telegramCallback) {
	if cb == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			syslog.Errorf("[Notify] 处理 Telegram 回调 panic: %v", r)
		}
	}()
	if cb.Message == nil || cb.Message.Chat == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "无法定位消息", true)
		cancel()
		return
	}
	chatID := strconv.FormatInt(cb.Message.Chat.ID, 10)
	if !isAllowedChat(chatID, cb.Message.Chat.Username) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "会话未授权", true)
		cancel()
		return
	}
	data := strings.TrimSpace(cb.Data)
	if strings.HasPrefix(data, callbackPrefix+":") {
		handleFilmPageCallback(token, cb)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "未知操作", false)
		cancel()
	}
}

func isAllowedChat(chatID, username string) bool {
	cfg := GetConfig()
	if !cfg.Enabled {
		return false
	}
	chatID = strings.TrimSpace(chatID)
	username = strings.TrimSpace(strings.TrimPrefix(username, "@"))
	for _, id := range cfg.ChatIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if id == chatID {
			return true
		}
		if username != "" && strings.EqualFold(strings.TrimPrefix(id, "@"), username) {
			return true
		}
	}
	for _, t := range cfg.Targets {
		if !t.Enabled {
			continue
		}
		tChat := strings.TrimSpace(t.ChatID)
		if tChat == chatID {
			return true
		}
		if username != "" && strings.EqualFold(strings.TrimPrefix(tChat, "@"), username) {
			return true
		}
	}
	return false
}

func isTelegramGetUpdatesConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "terminated by other getupdates")
}

func isTelegramWebhookActiveError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "can't use getupdates method while webhook is active")
}

func isTelegramFatalAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "http 401") ||
		strings.Contains(msg, "http 404")
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func newPollerOwnerID() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), hex.EncodeToString(b[:]))
}

func holdBotPollerLock(ctx context.Context, owner string) bool {
	if db.Rdb == nil {
		return true
	}
	key := config.NotifyBotPollerLockKey
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	ok, err := db.Rdb.SetNX(cctx, key, owner, botPollerLockTTL).Result()
	if err != nil {
		log.Printf("[Notify] Bot 轮询锁 SetNX 失败: %v", err)
		return false
	}
	if ok {
		return true
	}

	cur, err := db.Rdb.Get(cctx, key).Result()
	if err == redis.Nil {
		ok2, err2 := db.Rdb.SetNX(cctx, key, owner, botPollerLockTTL).Result()
		if err2 != nil {
			log.Printf("[Notify] Bot 轮询锁重试 SetNX 失败: %v", err2)
			return false
		}
		return ok2
	}
	if err != nil {
		log.Printf("[Notify] Bot 轮询锁 Get 失败: %v", err)
		return false
	}
	if cur != owner {
		return false
	}
	if err := db.Rdb.Expire(cctx, key, botPollerLockTTL).Err(); err != nil {
		log.Printf("[Notify] Bot 轮询锁续期失败: %v", err)
		return false
	}
	return true
}

func releaseBotPollerLock(owner string) {
	if db.Rdb == nil || owner == "" {
		return
	}
	cctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const script = `
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0
`
	_ = db.Rdb.Eval(cctx, script, []string{config.NotifyBotPollerLockKey}, owner).Err()
}
