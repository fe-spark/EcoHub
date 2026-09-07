package notify

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/model"
)

func TestResolvePollerToken(t *testing.T) {
	cases := []struct {
		name     string
		cfg      model.NotifyConfig
		expected string
	}{
		{
			name: "disabled with token",
			cfg: model.NotifyConfig{
				Enabled:  false,
				BotToken: "123456:ABC-DEF",
			},
			expected: "",
		},
		{
			name: "enabled with empty token",
			cfg: model.NotifyConfig{
				Enabled:  true,
				BotToken: "   ",
			},
			expected: "",
		},
		{
			name: "enabled with valid token",
			cfg: model.NotifyConfig{
				Enabled:  true,
				BotToken: "  123456:ABC-DEF  ",
			},
			expected: "123456:ABC-DEF",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvePollerToken(tc.cfg)
			if got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestEnsureBotPollerLifecycle(t *testing.T) {
	// 确保测试前重置状态
	pollerMu.Lock()
	old := takeStopLocked()
	pollerMu.Unlock()
	waitStopped(old)

	defer func() {
		pollerMu.Lock()
		cur := takeStopLocked()
		pollerMu.Unlock()
		waitStopped(cur)
	}()

	var (
		startCount int32
		stopCount  int32
		lastToken  atomic.Value
	)

	runner := func(ctx context.Context, token string) {
		atomic.AddInt32(&startCount, 1)
		lastToken.Store(token)
		<-ctx.Done()
		atomic.AddInt32(&stopCount, 1)
	}

	// 1. 开关打开，有效 Token -> 启动
	ensureBotPoller("token-1", runner)
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&startCount) != 1 {
		t.Fatalf("expected startCount=1, got %d", atomic.LoadInt32(&startCount))
	}
	if lastToken.Load().(string) != "token-1" {
		t.Fatalf("expected token-1, got %v", lastToken.Load())
	}

	// 2. 重复调用相同 Token -> 不颠簸重启
	ensureBotPoller("token-1", runner)
	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&startCount) != 1 {
		t.Fatalf("expected startCount=1 (no-op), got %d", atomic.LoadInt32(&startCount))
	}

	// 3. Token 变更 -> 旧协程退出，新协程启动
	ensureBotPoller("token-2", runner)
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&startCount) != 2 {
		t.Fatalf("expected startCount=2, got %d", atomic.LoadInt32(&startCount))
	}
	if atomic.LoadInt32(&stopCount) != 1 {
		t.Fatalf("expected stopCount=1, got %d", atomic.LoadInt32(&stopCount))
	}
	if lastToken.Load().(string) != "token-2" {
		t.Fatalf("expected token-2, got %v", lastToken.Load())
	}

	// 4. 开关关闭（空 Token） -> 协程停止，重置句柄
	ensureBotPoller("", runner)
	if atomic.LoadInt32(&stopCount) != 2 {
		t.Fatalf("expected stopCount=2, got %d", atomic.LoadInt32(&stopCount))
	}

	pollerMu.Lock()
	isNil := pollerGen == nil && pollerToken == ""
	pollerMu.Unlock()
	if !isNil {
		t.Fatal("expected pollerGen and pollerToken to be cleared")
	}

	// 5. 协程自行退出时自动回收句柄，后续调用相同 Token 可正常重新拉起
	exitCh := make(chan struct{})
	selfExitRunner := func(ctx context.Context, token string) {
		atomic.AddInt32(&startCount, 1)
		// 模拟发生鉴权失败或检测到开关关闭，自行退出
		close(exitCh)
	}

	ensureBotPoller("token-3", selfExitRunner)
	<-exitCh
	time.Sleep(50 * time.Millisecond)

	pollerMu.Lock()
	cleaned := pollerGen == nil && pollerToken == ""
	pollerMu.Unlock()
	if !cleaned {
		t.Fatal("expected pollerGen to be auto-cleaned on self exit")
	}

	// 再次启动相同 Token，必须能够成功拉起
	restartedCh := make(chan struct{})
	ensureBotPoller("token-3", func(ctx context.Context, token string) {
		close(restartedCh)
		<-ctx.Done()
	})
	select {
	case <-restartedCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected poller to successfully restart after previous runner exited")
	}
}
