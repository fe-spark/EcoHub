package collect

import (
	"sync"
	"testing"
	"time"

	"server/internal/model"
)

func TestSearchResealOnTerminalDone_NoABBADeadlockAndClearsSealing(t *testing.T) {
	origQ := searchReseal
	q := &searchResealQueue{
		pending:   map[int64]struct{}{},
		sealing:   map[string]struct{}{},
		awaitDone: true,
	}
	searchReseal = q
	defer func() { searchReseal = origQ }()

	// 模拟调用方已持有外层重建锁（如 searchRebuild.mu），同时采集生命周期锁内反向查询 SearchWorkBusyReason。
	var fakeRebuildMu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				fakeRebuildMu.Lock()
				q.mu.Lock()
				q.sealing["src_deadlock"] = struct{}{}
				q.awaitDone = true
				q.mu.Unlock()
				q.onSearchIndexTerminal("done")
				fakeRebuildMu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				collectLifecycle.mu.Lock()
				_ = SearchWorkBusyReason()
				collectLifecycle.mu.Unlock()
			}
		}()
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("检测到搜索封口终态回调与 collectLifecycle.mu 之间的死锁")
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.sealing) != 0 {
		t.Fatalf("done 收口后 sealing 应为空，实际=%v", q.sealing)
	}
}

func TestReleaseSourceOccupy_DoesNotReleaseOtherBatchToken(t *testing.T) {
	const srcID = "src_occupy_double_release"
	defer releaseCollectSourceIDs(srcID)

	src := model.FilmSource{Id: srcID, Name: "占坑回归站", State: true}
	occA, tokA := occupyCollectSourcesWithTokens([]model.FilmSource{src}, "testA")
	if len(occA) != 1 {
		t.Fatal("batchA 占坑应成功")
	}
	batchA := newCollectBatchContext("manual", "testA", occA, time.Now()).withClaim(CollectClaim{tokens: tokA})
	defer unregisterActiveBatch(batchA)
	// batchA 第一次正常释放
	batchA.releaseSourceOccupy(srcID)

	// batchB 随后合法占坑同一站点
	occB, tokB := occupyCollectSourcesWithTokens([]model.FilmSource{src}, "testB")
	if len(occB) != 1 {
		t.Fatal("batchB 占坑应成功")
	}
	batchB := newCollectBatchContext("manual", "testB", occB, time.Now()).withClaim(CollectClaim{tokens: tokB})
	defer unregisterActiveBatch(batchB)
	defer batchB.releaseSourceOccupy(srcID)

	// batchA 重复释放（如并发收口路径），不得误删 batchB 的占坑
	batchA.releaseSourceOccupy(srcID)

	occC, tokC := occupyCollectSourcesWithTokens([]model.FilmSource{src}, "testC")
	if len(occC) > 0 {
		releaseCollectTokens(tokC)
		t.Fatal("batchA 重复释放误删了 batchB 的占坑，导致 batchC 并发重入")
	}
}
