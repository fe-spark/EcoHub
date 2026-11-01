package fetcher

import (
	"context"
	"testing"
	"time"

	"server/internal/config"
)

func TestGetSourcePageConcurrencySharesHostBudget(t *testing.T) {
	prevWorkers := config.CollectPageWorkers
	prevSolo := config.CollectPageWorkersSolo
	prevBudget := config.CollectFetchInFlight
	prevDeps := deps
	t.Cleanup(func() {
		config.CollectPageWorkers = prevWorkers
		config.CollectPageWorkersSolo = prevSolo
		config.CollectFetchInFlight = prevBudget
		deps = prevDeps
	})
	config.CollectPageWorkers = 16
	config.CollectPageWorkersSolo = 24
	config.CollectFetchInFlight = 48

	deps.LiveTaskCount = func() int { return 1 }
	if got := GetSourcePageConcurrency(nil); got != 24 {
		t.Fatalf("solo=%d", got)
	}
	deps.LiveTaskCount = func() int { return 12 }
	if got := GetSourcePageConcurrency(nil); got != 4 {
		t.Fatalf("12 stations=%d", got)
	}
	deps.LiveTaskCount = func() int { return 2 }
	if got := GetSourcePageConcurrency(nil); got != 16 {
		t.Fatalf("2 stations=%d", got)
	}

	if gotMax := GetSourcePageConcurrencyMax(nil); gotMax != 24 {
		t.Fatalf("max solo=%d", gotMax)
	}
}

func TestDynamicLimiterScale(t *testing.T) {
	currentLimit := 1
	limiter := newDynamicLimiter(func() int {
		return currentLimit
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := limiter.acquire(ctx); err != nil {
		t.Fatalf("首次获取应该成功: %v", err)
	}

	// 第二次获取在 limit=1 时应该阻塞
	blockedCtx, blockedCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer blockedCancel()
	if err := limiter.acquire(blockedCtx); err == nil {
		t.Fatal("在达到 limit=1 时第二次获取应该阻塞并超时")
	}

	// 动态放开 limit 到 2
	currentLimit = 2
	limiter.cond.Broadcast()

	fastCtx, fastCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer fastCancel()
	if err := limiter.acquire(fastCtx); err != nil {
		t.Fatalf("动态扩容到 limit=2 后应该成功获取: %v", err)
	}

	// release 两次
	limiter.release()
	limiter.release()

	if limiter.running != 0 {
		t.Fatalf("全部释放后 running 应该为 0, got %d", limiter.running)
	}
}
