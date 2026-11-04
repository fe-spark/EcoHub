package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWriteLaneRoundRobinInterleavesSources(t *testing.T) {
	lane := newWriteLane("test-rr")
	lane.workers = 0 // 不自动 start workers，手动 nextJob

	// 站 A 页 1,2,3；站 B 页 101,102,103
	for _, page := range []int{1, 2, 3} {
		p := page
		if err := lane.submit(context.Background(), Job{
			SourceID:   "A",
			SourceName: "A",
			Page:       p,
			Write:      func() (Mids, error) { return Mids{}, nil },
			Complete:   func(Completion) {},
		}); err != nil {
			t.Fatalf("submit A page %d: %v", p, err)
		}
	}
	for _, page := range []int{101, 102, 103} {
		p := page
		if err := lane.submit(context.Background(), Job{
			SourceID:   "B",
			SourceName: "B",
			Page:       p,
			Write:      func() (Mids, error) { return Mids{}, nil },
			Complete:   func(Completion) {},
		}); err != nil {
			t.Fatalf("submit B page %d: %v", p, err)
		}
	}

	// 手动取 6 次，验证不会连续把 A 三页写完
	gotSources := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		job, _, finish := lane.nextJob()
		gotSources = append(gotSources, job.SourceID)
		_, _ = job.Write()
		job.Complete(Completion{Page: job.Page})
		finish()
	}

	// 理想 RR：A,B,A,B,A,B（order 中 A 先入队）
	wantPattern := []string{"A", "B", "A", "B", "A", "B"}
	for i := range wantPattern {
		if gotSources[i] != wantPattern[i] {
			t.Fatalf("round-robin order mismatch at %d: got %v want %v", i, gotSources, wantPattern)
		}
	}
}

func TestWriteLaneSameSourceSerial(t *testing.T) {
	lane := newWriteLane("test-serial")
	// 多 worker 下同站仍必须串行（writing 互斥）。
	lane.workers = 3
	lane.start()

	var inflight atomic.Int32
	var maxInflight atomic.Int32
	var wg sync.WaitGroup
	const pages = 8
	wg.Add(pages)

	for i := 1; i <= pages; i++ {
		p := i
		if err := lane.submit(context.Background(), Job{
			SourceID:   "S1",
			SourceName: "S1",
			Page:       p,
			Write: func() (Mids, error) {
				cur := inflight.Add(1)
				for {
					old := maxInflight.Load()
					if cur <= old || maxInflight.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				inflight.Add(-1)
				return Mids{}, nil
			},
			Complete: func(Completion) { wg.Done() },
		}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	lane.finishSource("S1")
	wg.Wait()

	if maxInflight.Load() != 1 {
		t.Fatalf("same source should be serial, max inflight=%d", maxInflight.Load())
	}
}

func TestWriteLaneGlobalBackpressure(t *testing.T) {
	lane := newWriteLane("test-global-bp")
	lane.maxPerSource = 10
	lane.maxGlobal = 3
	lane.workers = 0

	// 填满全局水位 3
	for i := 1; i <= 3; i++ {
		p := i
		if err := lane.submit(context.Background(), Job{
			SourceID:   "A",
			SourceName: "A",
			Page:       p,
			Write:      func() (Mids, error) { return Mids{}, nil },
			Complete:   func(Completion) {},
		}); err != nil {
			t.Fatalf("fill submit %d: %v", p, err)
		}
	}
	snap := lane.snapshot()
	if snap.PendingTotal != 3 {
		t.Fatalf("expected global pending 3, got %d", snap.PendingTotal)
	}

	// 再提交应被全局水位阻塞，直到 take 一页
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	unblocked := make(chan struct{})
	go func() {
		err := lane.submit(ctx, Job{
			SourceID:   "B",
			SourceName: "B",
			Page:       100,
			Write:      func() (Mids, error) { return Mids{}, nil },
			Complete:   func(Completion) {},
		})
		if err != nil {
			t.Errorf("blocked submit failed: %v", err)
			return
		}
		close(unblocked)
	}()

	// 确认一段时间内仍阻塞
	select {
	case <-unblocked:
		t.Fatal("submit should block when global buffer is full")
	case <-time.After(80 * time.Millisecond):
	}

	// 消费 1 页，应唤醒被反压的 submit
	job, _, finish := lane.nextJob()
	_, _ = job.Write()
	job.Complete(Completion{Page: job.Page})
	finish()

	select {
	case <-unblocked:
	case <-time.After(1 * time.Second):
		t.Fatal("submit should resume after global capacity frees")
	}

	snap = lane.snapshot()
	// take 1 后 pending=2，再 submit 1 → pending=3
	if snap.PendingTotal != 3 {
		t.Fatalf("expected pending back to 3, got %d", snap.PendingTotal)
	}
}

func TestWriteLaneSourceBackpressure(t *testing.T) {
	lane := newWriteLane("test-source-bp")
	lane.maxPerSource = 2
	lane.maxGlobal = 100
	lane.workers = 0

	for i := 1; i <= 2; i++ {
		p := i
		if err := lane.submit(context.Background(), Job{
			SourceID:   "S",
			SourceName: "S",
			Page:       p,
			Write:      func() (Mids, error) { return Mids{}, nil },
			Complete:   func(Completion) {},
		}); err != nil {
			t.Fatalf("fill submit %d: %v", p, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	unblocked := make(chan struct{})
	go func() {
		err := lane.submit(ctx, Job{
			SourceID:   "S",
			SourceName: "S",
			Page:       3,
			Write:      func() (Mids, error) { return Mids{}, nil },
			Complete:   func(Completion) {},
		})
		if err != nil {
			t.Errorf("source bp submit failed: %v", err)
			return
		}
		close(unblocked)
	}()

	select {
	case <-unblocked:
		t.Fatal("submit should block when source buffer is full")
	case <-time.After(80 * time.Millisecond):
	}

	job, _, finish := lane.nextJob()
	_, _ = job.Write()
	job.Complete(Completion{Page: job.Page})
	finish()

	select {
	case <-unblocked:
	case <-time.After(1 * time.Second):
		t.Fatal("submit should resume after source capacity frees")
	}
}

func TestWriteLanePanicReleasesWriting(t *testing.T) {
	lane := newWriteLane("test-panic")
	lane.workers = 1
	lane.start()

	var firstDone, secondDone sync.WaitGroup
	firstDone.Add(1)
	secondDone.Add(1)

	// 第一页 panic，必须释放 writing，否则第二页永远无法 take。
	if err := lane.submit(context.Background(), Job{
		SourceID:   "P1",
		SourceName: "P1",
		Page:       1,
		Write: func() (Mids, error) {
			panic("boom")
		},
		Complete: func(c Completion) {
			if c.Err == nil {
				t.Errorf("expected panic error completion")
			}
			firstDone.Done()
		},
	}); err != nil {
		t.Fatalf("submit panic job: %v", err)
	}
	if err := lane.submit(context.Background(), Job{
		SourceID:   "P1",
		SourceName: "P1",
		Page:       2,
		Write:      func() (Mids, error) { return Mids{}, nil },
		Complete:   func(Completion) { secondDone.Done() },
	}); err != nil {
		t.Fatalf("submit follow-up job: %v", err)
	}
	lane.finishSource("P1")

	done := make(chan struct{})
	go func() {
		firstDone.Wait()
		secondDone.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writing lock not released after panic; follow-up page stuck")
	}
}

func TestWriteLaneWaitPending(t *testing.T) {
	lane := newWriteLane("test-wait-pending")
	lane.workers = 2
	lane.start()

	var executed atomic.Int32
	const total = 5
	for i := 1; i <= total; i++ {
		p := i
		if err := lane.submit(context.Background(), Job{
			SourceID:   "src_wait",
			SourceName: "src_wait",
			Page:       p,
			Write: func() (Mids, error) {
				time.Sleep(10 * time.Millisecond)
				executed.Add(1)
				return Mids{}, nil
			},
			Complete: func(Completion) {},
		}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	lane.finishSource("src_wait")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := lane.waitPending(ctx); err != nil {
		t.Fatalf("waitPending failed: %v", err)
	}

	if executed.Load() != total {
		t.Fatalf("expected all %d jobs to be executed before waitPending returns, got %d", total, executed.Load())
	}
}

func TestWaitPendingWrites_Idle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := WaitPendingWrites(ctx); err != nil {
		t.Fatalf("WaitPendingWrites on idle scheduler failed: %v", err)
	}
}

func TestWriteLaneWaitPending_Timeout(t *testing.T) {
	lane := newWriteLane("test_timeout")
	lane.start()

	// 提交一个耗时任务
	if err := lane.submit(context.Background(), Job{
		SourceID:   "src_slow",
		SourceName: "src_slow",
		Page:       1,
		Write: func() (Mids, error) {
			time.Sleep(200 * time.Millisecond)
			return Mids{}, nil
		},
		Complete: func(Completion) {},
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// 等待超时时间极短（20ms），应返回 context.DeadlineExceeded
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := lane.waitPending(ctx)
	if err != context.DeadlineExceeded {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
}

func TestWriteLaneWaitPending_WorkerPanic(t *testing.T) {
	lane := newWriteLane("test_panic")
	lane.start()

	// 提交一个会 panic 的任务
	if err := lane.submit(context.Background(), Job{
		SourceID:   "src_panic",
		SourceName: "src_panic",
		Page:       1,
		Write: func() (Mids, error) {
			panic("simulated panic in write")
		},
		Complete: func(Completion) {},
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	lane.finishSource("src_panic")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// panic 被 recover 后 finishWriting 仍应被正常执行，waitPending 不能被卡死
	if err := lane.waitPending(ctx); err != nil {
		t.Fatalf("waitPending failed despite panic recovery: %v", err)
	}
}

func TestWriteLaneCancelSourceCompletesPending(t *testing.T) {
	lane := newWriteLane("test-cancel-source")
	lane.workers = 0

	var completed atomic.Int32
	for i := 1; i <= 3; i++ {
		p := i
		if err := lane.submit(context.Background(), Job{
			SourceID:   "S",
			SourceName: "S",
			Page:       p,
			Write: func() (Mids, error) {
				t.Fatal("canceled pending jobs must not run write")
				return Mids{}, nil
			},
			Complete: func(c Completion) {
				if c.Err != context.Canceled {
					t.Errorf("page %d: expected canceled, got %v", p, c.Err)
				}
				completed.Add(1)
			},
		}); err != nil {
			t.Fatalf("submit %d: %v", p, err)
		}
	}

	lane.cancelSource("S")
	if got := completed.Load(); got != 3 {
		t.Fatalf("expected 3 canceled completions, got %d", got)
	}
	if snap := lane.snapshot(); snap.PendingTotal != 0 || snap.PendingSources != 0 {
		t.Fatalf("queue should be gone after cancel, got pending=%d sources=%d", snap.PendingTotal, snap.PendingSources)
	}
}
