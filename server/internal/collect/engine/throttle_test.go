package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestThrottleStore_NoFixedIntervalByDefault(t *testing.T) {
	ts := NewThrottleStore(func() int { return 16 }, func() int { return 16 })
	start := time.Now()
	for i := 0; i < 8; i++ {
		rel, err := ts.Acquire(context.Background(), "fast_src")
		if err != nil {
			t.Fatalf("Acquire failed: %v", err)
		}
		rel(nil)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("无固定采集间隔下连续请求耗时异常过长: %s", elapsed)
	}
}

func TestThrottleStore_AIMDAdjustsOn429AndRecovery(t *testing.T) {
	now := time.Now()
	ts := NewThrottleStore(func() int { return 16 }, func() int { return 16 })
	ts.nowFn = func() time.Time { return now }

	if got := ts.WindowLimit("s1"); got != defaultThrottleWindow {
		t.Fatalf("initial window=%d, want %d", got, defaultThrottleWindow)
	}

	rel, err := ts.Acquire(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	rel(errors.New("Too Many Requests, status=429"))

	if got := ts.WindowLimit("s1"); got != defaultThrottleWindow/2 {
		t.Fatalf("window after 429=%d, want %d", got, defaultThrottleWindow/2)
	}

	// 推进时钟跳过冷却，连续成功 10 次后窗口 +1。
	now = now.Add(2 * time.Second)
	for i := 0; i < aimdIncreaseStep; i++ {
		rel, err := ts.Acquire(context.Background(), "s1")
		if err != nil {
			t.Fatal(err)
		}
		rel(nil)
	}
	if got := ts.WindowLimit("s1"); got != defaultThrottleWindow/2+1 {
		t.Fatalf("window after recovery=%d, want %d", got, defaultThrottleWindow/2+1)
	}

	// 验证站点间隔离与 Reset 恢复默认值。
	if got := ts.WindowLimit("s2"); got != defaultThrottleWindow {
		t.Fatalf("other source window affected: %d", got)
	}
	ts.Reset("s1")
	if got := ts.WindowLimit("s1"); got != defaultThrottleWindow {
		t.Fatalf("window after Reset=%d, want %d", got, defaultThrottleWindow)
	}
}

func TestThrottleStore_TwoStageDynamicIntervalAndRecovery(t *testing.T) {
	now := time.Now()
	ts := NewThrottleStore(func() int { return 16 }, func() int { return 16 })
	ts.nowFn = func() time.Time { return now }

	trigger429 := func(errMsg string) {
		rel, err := ts.Acquire(context.Background(), "strict")
		if err != nil {
			t.Fatalf("Acquire failed: %v", err)
		}
		rel(errors.New(errMsg))
	}

	// 一级退避：窗口 8 -> 4 -> 2 -> 1，期间动态间隔保持 0。
	for _, wantWin := range []int{4, 2, 1} {
		trigger429("Too Many Requests, status=429")
		if got := ts.WindowLimit("strict"); got != wantWin {
			t.Fatalf("window=%d, want %d", got, wantWin)
		}
		if got := ts.DynamicInterval("strict"); got != 0 {
			t.Fatalf("dynamic interval should stay 0 while window>1, got %s", got)
		}
		now = now.Add(2 * time.Second)
	}

	// 二级退避：窗口已为 1 后再遇 429，动态间隔 250ms -> 500ms -> 1s -> 2s -> 4s -> 5s 封顶。
	for _, wantInt := range []time.Duration{
		250 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		5 * time.Second,
		5 * time.Second,
	} {
		trigger429("Too Many Requests, status=429")
		if got := ts.DynamicInterval("strict"); got != wantInt {
			t.Fatalf("dynamic interval=%s, want %s", got, wantInt)
		}
		now = now.Add(6 * time.Second)
	}

	// 渐进恢复：连续成功优先把动态间隔减半直至归零，期间并发窗口保持 1；归零后再放大并发窗口。
	runSuccessRound := func() {
		for i := 0; i < aimdIncreaseStep; i++ {
			rel, err := ts.Acquire(context.Background(), "strict")
			if err != nil {
				t.Fatalf("Acquire failed: %v", err)
			}
			rel(nil)
			now = now.Add(6 * time.Second)
		}
	}

	for ts.DynamicInterval("strict") > 0 {
		prevInt := ts.DynamicInterval("strict")
		runSuccessRound()
		if ts.DynamicInterval("strict") >= prevInt {
			t.Fatalf("dynamic interval did not shrink: prev=%s cur=%s", prevInt, ts.DynamicInterval("strict"))
		}
		if ts.WindowLimit("strict") != 1 {
			t.Fatalf("window should remain 1 while dynamic interval recovering, got %d", ts.WindowLimit("strict"))
		}
	}

	// 动态间隔归零后，再连续成功一轮，并发窗口开始从 1 -> 2 恢复。
	runSuccessRound()
	if got := ts.WindowLimit("strict"); got != 2 {
		t.Fatalf("window after interval cleared=%d, want 2", got)
	}
}

func TestThrottleStore_RespectsRetryAfterCooldown(t *testing.T) {
	now := time.Now()
	ts := NewThrottleStore(func() int { return 16 }, func() int { return 16 })
	ts.nowFn = func() time.Time { return now }

	rel, err := ts.Acquire(context.Background(), "retry_after_src")
	if err != nil {
		t.Fatal(err)
	}
	rel(errors.New("Too Many Requests, status=429, retry_after=5s, url=http://example.com"))

	w := ts.window("retry_after_src")
	w.mu.Lock()
	gotCooldown := w.cooldownUntil.Sub(now)
	w.mu.Unlock()
	if gotCooldown != 5*time.Second {
		t.Fatalf("cooldown=%s, want 5s from Retry-After", gotCooldown)
	}

	// 短冷却的迟到 429 不能覆盖已有的更长 Retry-After 冷却截止时间。
	w.release(0, errors.New("Too Many Requests, status=429"))
	w.mu.Lock()
	afterSecond := w.cooldownUntil.Sub(now)
	w.mu.Unlock()
	if afterSecond != 5*time.Second {
		t.Fatalf("shorter 429 overwrote longer Retry-After cooldown: got %s, want 5s", afterSecond)
	}
}

func TestThrottleStore_ConcurrentBurst429BacksOffOncePerEpoch(t *testing.T) {
	now := time.Now()
	ts := NewThrottleStore(func() int { return 16 }, func() int { return 16 })
	ts.nowFn = func() time.Time { return now }

	// 同并发波次内同时放行 8 个在途请求，随后全部返回 429。
	releases := make([]func(error), 0, defaultThrottleWindow)
	for i := 0; i < defaultThrottleWindow; i++ {
		rel, err := ts.Acquire(context.Background(), "burst")
		if err != nil {
			t.Fatalf("Acquire %d failed: %v", i, err)
		}
		releases = append(releases, rel)
	}
	for _, rel := range releases {
		rel(errors.New("Too Many Requests, status=429"))
	}

	if got := ts.WindowLimit("burst"); got != defaultThrottleWindow/2 {
		t.Fatalf("burst of 8 429s in one epoch should halve window once to %d, got %d", defaultThrottleWindow/2, got)
	}
	if got := ts.DynamicInterval("burst"); got != 0 {
		t.Fatalf("burst in one epoch must not avalanche into dynamicInterval=%s", got)
	}
}

func TestThrottleStore_DynamicIntervalPacesLaunches(t *testing.T) {
	ts := NewThrottleStore(func() int { return 8 }, func() int { return 8 })
	w := ts.window("paced")
	w.mu.Lock()
	w.limit = 1
	w.dynamicInterval = 30 * time.Millisecond
	w.mu.Unlock()

	start := time.Now()
	for i := 0; i < 3; i++ {
		rel, err := ts.Acquire(context.Background(), "paced")
		if err != nil {
			t.Fatalf("Acquire %d failed: %v", i, err)
		}
		rel(nil)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("动态间隔未生效，3 次请求仅耗时 %s", elapsed)
	}
}
