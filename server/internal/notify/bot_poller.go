package notify

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Telegram long-polling：内联键盘回调 + /search 文本指令。

var (
	pollerMu     sync.Mutex
	pollerCancel context.CancelFunc
	pollerToken  string
)

// EnsureBotPoller 按已保存 Bot Token 启停轮询。无 Token 停止；Token 变化则重启。
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
	log.Printf("[Notify] Telegram Bot 轮询已启动（/search + 列表翻页）")
}

func stopBotPollerLocked() {
	if pollerCancel != nil {
		pollerCancel()
		pollerCancel = nil
		pollerToken = ""
	}
}

func runBotPoller(ctx context.Context, token string) {
	{
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if err := client.deleteWebhook(cctx, token); err != nil {
			log.Printf("[Notify] deleteWebhook: %v", err)
		}
		cancel()
	}

	var offset int64
	backoff := time.Second
	commandsOK := false
	for {
		select {
		case <-ctx.Done():
			return
		default:
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
		log.Printf("[Notify] setMyCommands 失败（将重试）: %v", err)
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
