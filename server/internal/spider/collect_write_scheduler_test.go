package spider

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/model"
)

func TestWriteLaneRoundRobinInterleavesSources(t *testing.T) {
	lane := newCollectWriteLane("test-rr")
	// 关闭限速影响：把 limiter 设得极高，只验证取页顺序。
	lane.limiter.SetLimit(1e6)
	lane.limiter.SetBurst(1000)
	lane.workers = 0 // 不自动 start workers，手动 nextJob

	// 站 A 页 1,2,3；站 B 页 101,102,103
	for _, page := range []int{1, 2, 3} {
		p := page
		if err := lane.submit(context.Background(), collectWriteJob{
			sourceID:   "A",
			sourceName: "A",
			grade:      model.SlaveCollect,
			page:       p,
			write:      func() ([]int64, error) { return nil, nil },
			complete:   func(collectWriteCompletion) {},
		}); err != nil {
			t.Fatalf("submit A page %d: %v", p, err)
		}
	}
	for _, page := range []int{101, 102, 103} {
		p := page
		if err := lane.submit(context.Background(), collectWriteJob{
			sourceID:   "B",
			sourceName: "B",
			grade:      model.SlaveCollect,
			page:       p,
			write:      func() ([]int64, error) { return nil, nil },
			complete:   func(collectWriteCompletion) {},
		}); err != nil {
			t.Fatalf("submit B page %d: %v", p, err)
		}
	}

	// 手动取 6 次，验证不会连续把 A 三页写完
	gotSources := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		job, _, finish := lane.nextJob()
		gotSources = append(gotSources, job.sourceID)
		_, _ = job.write()
		job.complete(collectWriteCompletion{page: job.page})
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
	lane := newCollectWriteLane("test-serial")
	lane.limiter.SetLimit(1e6)
	lane.limiter.SetBurst(1000)
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
		if err := lane.submit(context.Background(), collectWriteJob{
			sourceID:   "S1",
			sourceName: "S1",
			page:       p,
			write: func() ([]int64, error) {
				cur := inflight.Add(1)
				for {
					old := maxInflight.Load()
					if cur <= old || maxInflight.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				inflight.Add(-1)
				return nil, nil
			},
			complete: func(collectWriteCompletion) { wg.Done() },
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

func TestWriteLaneRateLimitApprox(t *testing.T) {
	lane := newCollectWriteLane("test-rate")
	// 约 10 页/秒，burst=1
	lane.limiter.SetLimit(10)
	lane.limiter.SetBurst(1)
	lane.workers = 1
	lane.start()

	const pages = 6
	var wg sync.WaitGroup
	wg.Add(pages)
	start := time.Now()
	for i := 1; i <= pages; i++ {
		p := i
		if err := lane.submit(context.Background(), collectWriteJob{
			sourceID:   "R1",
			sourceName: "R1",
			page:       p,
			write:      func() ([]int64, error) { return nil, nil },
			complete:   func(collectWriteCompletion) { wg.Done() },
		}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	lane.finishSource("R1")
	wg.Wait()
	elapsed := time.Since(start)

	// 6 页 @10/s 理论约 0.5s+，允许抖动；应明显大于无限制的瞬时完成。
	if elapsed < 400*time.Millisecond {
		t.Fatalf("rate limit too loose: finished %d pages in %s", pages, elapsed)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("rate limit too strict: finished %d pages in %s", pages, elapsed)
	}
}

func TestWriteLaneGlobalBackpressure(t *testing.T) {
	lane := newCollectWriteLane("test-global-bp")
	lane.limiter.SetLimit(1e6)
	lane.limiter.SetBurst(1000)
	lane.maxPerSource = 10
	lane.maxGlobal = 3
	lane.workers = 0

	// 填满全局水位 3
	for i := 1; i <= 3; i++ {
		p := i
		if err := lane.submit(context.Background(), collectWriteJob{
			sourceID:   "A",
			sourceName: "A",
			page:       p,
			write:      func() ([]int64, error) { return nil, nil },
			complete:   func(collectWriteCompletion) {},
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
		err := lane.submit(ctx, collectWriteJob{
			sourceID:   "B",
			sourceName: "B",
			page:       100,
			write:      func() ([]int64, error) { return nil, nil },
			complete:   func(collectWriteCompletion) {},
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
	_, _ = job.write()
	job.complete(collectWriteCompletion{page: job.page})
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
	lane := newCollectWriteLane("test-source-bp")
	lane.limiter.SetLimit(1e6)
	lane.limiter.SetBurst(1000)
	lane.maxPerSource = 2
	lane.maxGlobal = 100
	lane.workers = 0

	for i := 1; i <= 2; i++ {
		p := i
		if err := lane.submit(context.Background(), collectWriteJob{
			sourceID:   "S",
			sourceName: "S",
			page:       p,
			write:      func() ([]int64, error) { return nil, nil },
			complete:   func(collectWriteCompletion) {},
		}); err != nil {
			t.Fatalf("fill submit %d: %v", p, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	unblocked := make(chan struct{})
	go func() {
		err := lane.submit(ctx, collectWriteJob{
			sourceID:   "S",
			sourceName: "S",
			page:       3,
			write:      func() ([]int64, error) { return nil, nil },
			complete:   func(collectWriteCompletion) {},
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
	_, _ = job.write()
	job.complete(collectWriteCompletion{page: job.page})
	finish()

	select {
	case <-unblocked:
	case <-time.After(1 * time.Second):
		t.Fatal("submit should resume after source capacity frees")
	}
}

func TestWriteLanePanicReleasesWriting(t *testing.T) {
	lane := newCollectWriteLane("test-panic")
	lane.limiter.SetLimit(1e6)
	lane.limiter.SetBurst(1000)
	lane.workers = 1
	lane.start()

	var firstDone, secondDone sync.WaitGroup
	firstDone.Add(1)
	secondDone.Add(1)

	// 第一页 panic，必须释放 writing，否则第二页永远无法 take。
	if err := lane.submit(context.Background(), collectWriteJob{
		sourceID:   "P1",
		sourceName: "P1",
		page:       1,
		write: func() ([]int64, error) {
			panic("boom")
		},
		complete: func(c collectWriteCompletion) {
			if c.err == nil {
				t.Errorf("expected panic error completion")
			}
			firstDone.Done()
		},
	}); err != nil {
		t.Fatalf("submit panic job: %v", err)
	}
	if err := lane.submit(context.Background(), collectWriteJob{
		sourceID:   "P1",
		sourceName: "P1",
		page:       2,
		write:      func() ([]int64, error) { return nil, nil },
		complete:   func(collectWriteCompletion) { secondDone.Done() },
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

func TestWriteLaneReserveCancelDoesNotStarve(t *testing.T) {
	// 多 worker + 极少页：验证 take 失败路径会 Cancel reservation，
	// 不会因为空烧 token 把后续页拖到数秒级。
	lane := newCollectWriteLane("test-reserve-cancel")
	lane.limiter.SetLimit(20) // 20 页/秒
	lane.limiter.SetBurst(1)
	lane.workers = 4
	lane.start()

	const pages = 8
	var wg sync.WaitGroup
	wg.Add(pages)
	start := time.Now()
	for i := 1; i <= pages; i++ {
		p := i
		// 分散到多个站，放大 take 竞争。
		src := fmt.Sprintf("S%d", (i-1)%4)
		if err := lane.submit(context.Background(), collectWriteJob{
			sourceID:   src,
			sourceName: src,
			page:       p,
			write: func() ([]int64, error) {
				time.Sleep(2 * time.Millisecond)
				return nil, nil
			},
			complete: func(collectWriteCompletion) { wg.Done() },
		}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	for _, id := range []string{"S0", "S1", "S2", "S3"} {
		lane.finishSource(id)
	}
	wg.Wait()
	elapsed := time.Since(start)

	// 8 页 @20/s 理论 ~0.35s+；若空烧严重会远超 2s。
	if elapsed > 2*time.Second {
		t.Fatalf("reserve cancel likely broken: 8 pages took %s", elapsed)
	}
}
