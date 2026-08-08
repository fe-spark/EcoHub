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

	"github.com/redis/go-redis/v9"
)

// Telegram long-polling：内联键盘回调 + /search 文本指令。
//
// 同一 Bot Token 全局只允许一个 getUpdates 消费者。本进程用：
//  1. 进程内单例（按代句柄 cancel + done，停旧再起新）
//  2. Redis 领导锁（多 EcoHub 实例共享 Redis 时仅 leader 轮询）
//  3. Conflict 退避（外部进程也在 poll 时拉长间隔并释放领导权）

const (
	// botPollerLockTTL 领导锁 TTL：必须大于单次循环的最坏耗时。
	// 锁只在每轮 getUpdates 之前续期一次，而 getUpdates 最长阻塞 40s，
	// 加上 Redis 请求（≤3s）与 webhook/命令注册等，单轮最坏约 60s；
	// TTL 过短会在长轮询中途过期，第二实例抢锁 → 双 getUpdates → Conflict 抖动。
	// 放宽后：领导实例硬崩溃时，备用实例最长等待 TTL 才接管（正常退出会经 defer 主动释放）。
	botPollerLockTTL      = 90 * time.Second
	botPollerStandbyWait  = 10 * time.Second
	botPollerConflictWait = 60 * time.Second
	botPollerMaxBackoff   = 30 * time.Second
)

// pollerGeneration 一代 Bot 轮询的生命周期句柄：cancel 停止该代，done 在该代协程退出后关闭。
// 以「按代句柄」取代共享 WaitGroup：Wait 与 Add 永不重叠，避免
// sync: WaitGroup misuse: Add called concurrently with Wait 竞态 panic。
type pollerGeneration struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var (
	pollerMu    sync.Mutex
	pollerGen   *pollerGeneration // 当前已注册的轮询代；nil 表示未注册
	pollerToken string            // 与 pollerGen 配套的 Bot Token
)

// EnsureBotPoller 按已保存 Bot Token 启停轮询。无 Token 停止；Token 变化则重启。
// cancel + Wait 在锁外执行，避免长轮询退出时阻塞其它 Ensure 调用方。
func EnsureBotPoller() {
	cfg := GetConfig()
	token := strings.TrimSpace(cfg.BotToken)
	ensureBotPoller(token, runBotPoller)
}

// ensureBotPoller 是 EnsureBotPoller 的纯逻辑版本，runner 可注入以便并发回归测试。
func ensureBotPoller(token string, runner func(ctx context.Context, token string)) {
	for {
		pollerMu.Lock()
		if token == "" {
			old := takeStopLocked()
			pollerMu.Unlock()
			waitStopped(old)
			return
		}
		if pollerGen != nil && pollerToken == token {
			pollerMu.Unlock()
			return
		}
		old := takeStopLocked()
		pollerMu.Unlock()
		waitStopped(old)

		pollerMu.Lock()
		// 等待期间其它调用可能已拉起同 token
		if pollerGen != nil && pollerToken == token {
			pollerMu.Unlock()
			return
		}
		// 他人已拉起不同 token：后写者胜 —— 必须停掉该代再注册自己，
		// 否则两代协程同时存活会双 getUpdates。单实例全局只有一个配置 token，
		// 出现不同 token 只可能是并发调用读到新旧配置的瞬时竞态。
		if pollerGen != nil {
			log.Printf("[Notify] 检测到其它 token 的轮询代，将停止并替换为当前配置 token（后写者胜）")
			pollerMu.Unlock()
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		gen := &pollerGeneration{cancel: cancel, done: make(chan struct{})}
		pollerGen = gen
		pollerToken = token
		pollerMu.Unlock()
		go func(tok string, gen *pollerGeneration) {
			defer close(gen.done)
			runner(ctx, tok)
		}(token, gen)
		log.Printf("[Notify] Telegram Bot 轮询已启动（/search + 列表翻页）")
		return
	}
}

// takeStopLocked 取出并清空当前轮询代（须持 pollerMu）；不等待退出。
func takeStopLocked() *pollerGeneration {
	g := pollerGen
	pollerGen = nil
	pollerToken = ""
	return g
}

// waitStopped 取消旧轮询并等待其协程退出（须在 pollerMu 外调用）。
// getUpdates 使用带父 ctx 的超时请求，cancel 后 HTTP 会中断。
// 等待的是该代协程关闭的 done channel：即便协程尚未启动，done 也一定会在
// 该代协程退出时关闭，因此不会漏等，也不会与「启动新代」产生竞态。
func waitStopped(gen *pollerGeneration) {
	if gen == nil {
		return
	}
	gen.cancel()
	<-gen.done
}

func runBotPoller(ctx context.Context, token string) {
	owner := newPollerOwnerID()
	defer releaseBotPollerLock(owner)

	var offset int64
	backoff := time.Second
	commandsOK := false
	isLeader := false
	loggedStandby := false
	webhookCleared := false

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !holdBotPollerLock(ctx, owner) {
			if isLeader {
				log.Printf("[Notify] Bot 轮询领导权已丢失，进入待命")
				isLeader = false
				commandsOK = false
			}
			if !loggedStandby {
				log.Printf("[Notify] 已有其它 EcoHub 实例负责 Telegram Bot 轮询，本实例待命")
				loggedStandby = true
			}
			if !sleepCtx(ctx, botPollerStandbyWait) {
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

		if !commandsOK {
			if registerBotCommands(ctx, token) {
				commandsOK = true
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		updates, err := client.getUpdates(reqCtx, token, offset, 25)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if isTelegramGetUpdatesConflict(err) {
				log.Printf("[Notify] getUpdates 冲突：同一 Bot Token 另有 getUpdates 在跑（其它进程/环境/未退出旧实例）。释放领导权并 %s 后重试", botPollerConflictWait)
				releaseBotPollerLock(owner)
				isLeader = false
				commandsOK = false
				if !sleepCtx(ctx, botPollerConflictWait) {
					return
				}
				backoff = time.Second
				continue
			}
			if isTelegramWebhookActiveError(err) {
				// webhook 被外部重新设置（或清除失败）：保持领导权，下一轮重新清除
				log.Printf("[Notify] getUpdates 被拒：webhook 仍处于激活状态，将重新清除后重试")
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
			}
			continue
		}
		backoff = time.Second

		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.CallbackQuery != nil {
				dispatchCallback(token, u.CallbackQuery)
			}
			if u.Message != nil {
				handleBotMessage(token, u.Message)
			}
		}
	}
}

func registerBotCommands(ctx context.Context, token string) bool {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := client.setMyCommands(cctx, token, []botCommand{
		{Command: "search", Description: "搜索影片 /search 关键词"},
		{Command: "s", Description: "搜索简写 /s 关键词"},
		{Command: "start", Description: "开始使用"},
	})
	if err != nil {
		syslog.Warnf("[Notify] setMyCommands 失败（将重试）: %v", err)
		return false
	}
	log.Printf("[Notify] 已注册 Bot 指令: /search /s /start")
	return true
}

func dispatchCallback(token string, cb *telegramCallback) {
	if cb == nil {
		return
	}
	// Message/Chat 缺失时拒绝，避免绕过白名单进入翻页/搜索处理
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
	switch {
	case strings.HasPrefix(data, callbackPrefix+":"):
		handleFilmPageCallback(token, cb)
	case strings.HasPrefix(data, searchCallbackPrefix+":"):
		handleSearchPageCallback(token, cb)
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = client.answerCallbackQuery(ctx, token, cb.ID, "未知操作", false)
		cancel()
	}
}

// isTelegramGetUpdatesConflict 识别 Telegram 多实例 getUpdates 冲突
// （另一实例/进程正在对该 Bot Token 长轮询）。仅匹配 Telegram 官方
// "terminated by other getUpdates request" 文案，避免误伤 webhook 类错误。
func isTelegramGetUpdatesConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "terminated by other getupdates")
}

// isTelegramWebhookActiveError 识别「webhook 未清除导致 getUpdates 被拒」。
// 与多实例冲突不同：应保持领导权并重新清除 webhook，而非释放领导权退避。
func isTelegramWebhookActiveError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "can't use getupdates method while webhook is active")
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

// holdBotPollerLock 尝试取得或续期领导锁。
// Redis 不可用时退化为本机单例轮询（进程内仍靠 pollerGen 单例句柄保证单消费者）；
// 多实例同时遇 Redis 故障时仍可能双 poll，依赖 Conflict 退避收敛。
func holdBotPollerLock(ctx context.Context, owner string) bool {
	if db.Rdb == nil {
		return true
	}
	key := config.NotifyBotPollerLockKey
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	ok, err := db.Rdb.SetNX(cctx, key, owner, botPollerLockTTL).Result()
	if err != nil {
		log.Printf("[Notify] Bot 轮询锁 SetNX 失败，退化为本机轮询（多实例时可能 Conflict）: %v", err)
		return true
	}
	if ok {
		return true
	}

	cur, err := db.Rdb.Get(cctx, key).Result()
	if err == redis.Nil {
		// 竞态空窗：再试一次
		ok2, err2 := db.Rdb.SetNX(cctx, key, owner, botPollerLockTTL).Result()
		if err2 != nil {
			log.Printf("[Notify] Bot 轮询锁重试 SetNX 失败，退化为本机轮询: %v", err2)
			return true
		}
		return ok2
	}
	if err != nil {
		log.Printf("[Notify] Bot 轮询锁 Get 失败，退化为本机轮询: %v", err)
		return true
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
	// 仅释放自己持有的锁
	const script = `
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0
`
	if err := db.Rdb.Eval(cctx, script, []string{config.NotifyBotPollerLockKey}, owner).Err(); err != nil {
		log.Printf("[Notify] 释放 Bot 轮询锁失败: %v", err)
	}
}
