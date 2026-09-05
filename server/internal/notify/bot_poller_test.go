package notify

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/model"
)

// stopAllPollers 清空并停止当前已注册的轮询代，供测试收尾。
func stopAllPollers() {
	pollerMu.Lock()
	old := takeStopLocked()
	pollerMu.Unlock()
	waitStopped(old)
}

// waitCond 轮询等待条件成立，超时报错。
func waitCond(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// TestEnsureBotPollerLifecycle 验证启停/幂等/token 切换的基本语义。
func TestEnsureBotPollerLifecycle(t *testing.T) {
	var started atomic.Int32
	var stopped atomic.Int32
	runner := func(ctx context.Context, token string) {
		started.Add(1)
		<-ctx.Done()
		stopped.Add(1)
	}
	defer stopAllPollers()

	ensureBotPoller("token-a", runner)
	waitCond(t, func() bool { return started.Load() == 1 })
	// 同 token 幂等：不应重复启动（给潜在的新协程留出启动窗口后复查）
	ensureBotPoller("token-a", runner)
	time.Sleep(100 * time.Millisecond)
	if started.Load() != 1 {
		t.Fatalf("same token should not restart, got %d", started.Load())
	}
	// 切换 token：旧 runner 被取消退出，新 runner 启动
	ensureBotPoller("token-b", runner)
	waitCond(t, func() bool { return started.Load() == 2 && stopped.Load() == 1 })
	// 空 token：停止全部
	ensureBotPoller("", runner)
	waitCond(t, func() bool { return stopped.Load() == 2 })
}

// TestEnsureBotPollerConcurrentStress 并发压测启停/抢占。
// 曾用共享 WaitGroup 实现「取消+等待退出」，并发下 Add 与 Wait 可重叠，
// 触发 sync: WaitGroup misuse panic；现改为按代 done channel 后此测试须稳定通过
// （配合 go test -race 运行）。
func TestEnsureBotPollerConcurrentStress(t *testing.T) {
	// runner 模拟长轮询：阻塞至 ctx 取消，尽量拉长停止窗口
	runner := func(ctx context.Context, token string) {
		<-ctx.Done()
	}
	defer stopAllPollers()

	const workers = 8
	const rounds = 60
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				// 少量 token 轮换，制造停止/重启/互相抢占的并发交错
				ensureBotPoller(fmt.Sprintf("token-%d", (w+i)%3), runner)
			}
		}(w)
	}
	wg.Wait()
}

func TestResolvePollerToken(t *testing.T) {
	cases := []struct {
		name string
		cfg  model.NotifyConfig
		want string
	}{
		{
			name: "通知已启用且有Token",
			cfg: model.NotifyConfig{
				Enabled:  true,
				BotToken: " 123456:ABCDEF ",
			},
			want: "123456:ABCDEF",
		},
		{
			name: "通知未启用即便有Token也应返回空",
			cfg: model.NotifyConfig{
				Enabled:  false,
				BotToken: "123456:ABCDEF",
			},
			want: "",
		},
		{
			name: "通知已启用但Token为空",
			cfg: model.NotifyConfig{
				Enabled:  true,
				BotToken: "   ",
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePollerToken(tc.cfg); got != tc.want {
				t.Fatalf("resolvePollerToken() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStopBotPoller_CleanShutdown(t *testing.T) {
	var stopped atomic.Int32
	runner := func(ctx context.Context, token string) {
		<-ctx.Done()
		stopped.Add(1)
	}
	defer stopAllPollers()

	ensureBotPoller("test-stop-token", runner)
	pollerMu.Lock()
	if pollerGen == nil || pollerToken != "test-stop-token" {
		pollerMu.Unlock()
		t.Fatalf("poller should be running before StopBotPoller")
	}
	pollerMu.Unlock()

	StopBotPoller()

	pollerMu.Lock()
	genAfter := pollerGen
	tokAfter := pollerToken
	pollerMu.Unlock()

	if genAfter != nil || tokAfter != "" {
		t.Fatalf("pollerGen and pollerToken should be cleared after StopBotPoller, got gen=%v tok=%q", genAfter, tokAfter)
	}
	if stopped.Load() != 1 {
		t.Fatalf("runner should have received stop signal, got %d", stopped.Load())
	}
}

func TestBotPoller_SelfExitCleansPollerGen(t *testing.T) {
	// 验证 runner 自发退出（如检测到开关关闭或致命错误）时，pollerGen 能够被 defer 自动清空
	runner := func(ctx context.Context, token string) {
		// 模拟直接 return 退出
		return
	}
	defer stopAllPollers()

	ensureBotPoller("self-exit-token", runner)
	// 等待 runner 协程退出并执行 defer 清理
	waitCond(t, func() bool {
		pollerMu.Lock()
		defer pollerMu.Unlock()
		return pollerGen == nil && pollerToken == ""
	})
}

func TestBotPoller_FatalAuthError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"unauthorized lower", fmt.Errorf("telegram api: unauthorized"), true},
		{"unauthorized upper", fmt.Errorf("HTTP 401 Unauthorized"), true},
		{"not found", fmt.Errorf("telegram api: not found"), true},
		{"http 404", fmt.Errorf("telegram http 404: Not Found"), true},
		{"network timeout", fmt.Errorf("dial tcp: i/o timeout"), false},
		{"rate limit", fmt.Errorf("telegram rate limited: retry later"), false},
		{"conflict", fmt.Errorf("terminated by other getUpdates request"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTelegramFatalAuthError(tc.err); got != tc.expected {
				t.Fatalf("isTelegramFatalAuthError(%v) = %v, want %v", tc.err, got, tc.expected)
			}
		})
	}
}
