package notify

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// Telegram long-polling：处理内联键盘回调（上一页/下一页）。
// 不依赖公网 webhook，适合内网/Docker 部署。

var (
	pollerMu     sync.Mutex
	pollerCancel context.CancelFunc
	pollerToken  string
)

// EnsureBotPoller 按当前已保存 Bot Token 启停轮询。
// 无 Token 时停止；Token 变化时重启。
func EnsureBotPoller() {
	cfg := GetConfig()
	token := strings.TrimSpace(cfg.BotToken)
	pollerMu.Lock()
	defer pollerMu.Unlock()
	if token == "" {
		stopBotPollerLocked()
		return
	}
	if pollerCancel != nil && pollerToken == token {
		return
	}
	stopBotPollerLocked()
	ctx, cancel := context.WithCancel(context.Background())
	pollerCancel = cancel
	pollerToken = token
	go runBotPoller(ctx, token)
	log.Printf("[Notify] Telegram 回调查询已启动（内联键盘分页）")
}

func stopBotPollerLocked() {
	if pollerCancel != nil {
		pollerCancel()
		pollerCancel = nil
		pollerToken = ""
	}
}

func runBotPoller(ctx context.Context, token string) {
	// 清除 webhook，避免与 getUpdates 冲突
	{
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := client.deleteWebhook(cctx, token); err != nil {
			log.Printf("[Notify] deleteWebhook: %v", err)
		}
		cancel()
	}

	var offset int64
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// long poll：timeout 25s，外层 context 略长
		reqCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		updates, err := client.getUpdates(reqCtx, token, offset, 25)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[Notify] getUpdates 失败: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
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
				handleFilmPageCallback(token, u.CallbackQuery)
			}
		}
	}
}
