package notify

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
