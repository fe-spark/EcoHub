package spider

import (
	"testing"
	"time"

	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider/progress"
)

func resetSearchReseal() {
	searchReseal.mu.Lock()
	searchReseal.pending = map[int64]struct{}{}
	searchReseal.sealing = map[string]struct{}{}
	searchReseal.running = false
	searchReseal.fullCollect = false
	searchReseal.awaitDone = false
	searchReseal.mu.Unlock()
	searchSealRunning.Store(false)
	unfreezeCollectQueues()
	for collectQueueActive() {
		endCollectQueue()
	}
	filmsnapshot.NotifySearchIndexProgress(0, 0, "idle")
}

func TestSearchResealRun_WaitsThenSealsOnce(t *testing.T) {
	prevWait := waitCollectIdle
	prevApply := sealSearchIndex
	t.Cleanup(func() {
		waitCollectIdle = prevWait
		sealSearchIndex = prevApply
		collectLifecycle.endSource("busy-seal")
		resetSearchReseal()
	})
	if err := collectLifecycle.beginSource("busy-seal"); err != nil {
		t.Fatal(err)
	}

	waitStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	waitCollectIdle = func() {
		select {
		case waitStarted <- struct{}{}:
		default:
		}
		<-release
	}
	sealed := make(chan int, 1)
	sealSearchIndex = func(mids []int64, incomplete bool) bool {
		if incomplete {
			t.Errorf("hourly mids marked incomplete")
		}
		sealed <- len(mids)
		return true
	}

	scheduleSearchReseal([]int64{1, 2, 1})
	scheduleSearchReseal([]int64{2, 3})

	if got := filmsnapshot.GetLatestSearchIndexProgress(); got.Status != "waiting" {
		t.Fatalf("status = %s, want waiting", got.Status)
	}

	select {
	case <-waitStarted:
	case <-time.After(time.Second):
		t.Fatal("seal did not wait")
	}
	select {
	case n := <-sealed:
		t.Fatalf("sealed while a source is collecting: %d", n)
	case <-time.After(40 * time.Millisecond):
	}

	collectLifecycle.endSource("busy-seal")
	close(release)
	select {
	case n := <-sealed:
		if n != 3 {
			t.Fatalf("sealed %d mids, want 3", n)
		}
	case <-time.After(time.Second):
		t.Fatal("did not seal after sources went idle")
	}
}

func TestSearchResealRun_FullCollectRebuildsAfterQueuesIdle(t *testing.T) {
	prevSeal := sealSearchIndex
	prevRebuild := requestSearchRebuild
	t.Cleanup(func() {
		sealSearchIndex = prevSeal
		requestSearchRebuild = prevRebuild
		resetSearchReseal()
	})

	rebuilt := make(chan struct{}, 1)
	sealSearchIndex = func(mids []int64, incomplete bool) bool {
		t.Errorf("full collect must not point-query mids=%v incomplete=%t", mids, incomplete)
		return false
	}
	requestSearchRebuild = func() { rebuilt <- struct{}{} }

	beginCollectQueue()
	beginCollectQueue()
	noteSearchResealFullCollect()
	scheduleSearchReseal([]int64{1, 2})

	select {
	case <-rebuilt:
		t.Fatal("rebuilt while a collect queue is still running")
	case <-time.After(40 * time.Millisecond):
	}

	endCollectQueue()
	select {
	case <-rebuilt:
		t.Fatal("rebuilt while another collect queue is still running")
	case <-time.After(40 * time.Millisecond):
	}

	endCollectQueue()
	select {
	case <-rebuilt:
	case <-time.After(time.Second):
		t.Fatal("did not rebuild after every collect queue finished")
	}
}

func TestSearchResealRun_CompletesEmptyCycle(t *testing.T) {
	prevWait := waitCollectIdle
	prevApply := sealSearchIndex
	t.Cleanup(func() {
		waitCollectIdle = prevWait
		sealSearchIndex = prevApply
		collectLifecycle.endSource("empty-seal")
		resetSearchReseal()
	})
	progress.Ensure("empty-seal", "EmptySeal")
	progress.Update("empty-seal", func(p *model.CollectProgress) {
		p.Status = progress.StatusSealing
	})
	if err := collectLifecycle.beginSource("empty-seal"); err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	waitCollectIdle = func() { <-release }
	sealSearchIndex = func([]int64, bool) bool { return true }

	searchReseal.addSealing("empty-seal")
	time.Sleep(30 * time.Millisecond)
	if snap, _ := progress.Snapshot("empty-seal"); snap.Status != progress.StatusSealing {
		t.Fatalf("抓页未结束时应保持 sealing, got %q", snap.Status)
	}

	collectLifecycle.endSource("empty-seal")
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if snap, _ := progress.Snapshot("empty-seal"); snap.Status == progress.StatusDone {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap, _ := progress.Snapshot("empty-seal")
	t.Fatalf("空封口周期也应收成 done, got %q", snap.Status)
}

func TestBeginSource_RejectsWhileSearchIndexBuilding(t *testing.T) {
	filmsnapshot.NotifySearchIndexProgress(1, 0, "indexing")
	t.Cleanup(func() {
		filmsnapshot.NotifySearchIndexProgress(0, 0, "idle")
	})
	err := collectLifecycle.beginSource("during-index")
	if err != errSearchIndexUpdating {
		t.Fatalf("err = %v", err)
	}
}

func TestBeginSource_RejectsWhileSealQueueWaiting(t *testing.T) {
	t.Cleanup(resetSearchReseal)
	searchReseal.mu.Lock()
	searchReseal.running = true
	searchReseal.sealing["wait-block"] = struct{}{}
	searchReseal.mu.Unlock()
	if !searchIndexBlocksCollect() {
		t.Fatal("waiting seal queue should block collect")
	}
	err := collectLifecycle.beginSource("during-waiting")
	if err != errSearchSealPending {
		t.Fatalf("err = %v", err)
	}
}

func TestBeginSource_JoinsWhileSealRoundHasLiveQueue(t *testing.T) {
	t.Cleanup(func() {
		collectLifecycle.endSource("sibling-join")
		resetSearchReseal()
	})
	beginCollectQueue()
	searchReseal.mu.Lock()
	searchReseal.running = true
	searchReseal.sealing["live"] = struct{}{}
	searchReseal.mu.Unlock()

	if searchIndexBlocksCollect() {
		t.Fatal("seal round with a live queue should still accept collects")
	}
	if err := collectLifecycle.beginSource("sibling-join"); err != nil {
		t.Fatal(err)
	}
	ok, err := tryBeginCollectQueue()
	if !ok || err != nil {
		t.Fatalf("tryBegin = %v, %v", ok, err)
	}
	collectQueues.mu.Lock()
	got := collectQueues.n
	collectQueues.mu.Unlock()
	if got != 2 {
		t.Fatalf("queues = %d, want 2", got)
	}
}

func TestTryBeginCollectQueue_FreezesIdleSealRound(t *testing.T) {
	t.Cleanup(resetSearchReseal)
	searchReseal.mu.Lock()
	searchReseal.running = true
	searchReseal.sealing["gap"] = struct{}{}
	searchReseal.mu.Unlock()

	ok, err := tryBeginCollectQueue()
	if ok || err != errSearchSealPending {
		t.Fatalf("tryBegin = %v, %v", ok, err)
	}
	collectQueues.mu.Lock()
	frozen := collectQueues.frozen
	n := collectQueues.n
	collectQueues.mu.Unlock()
	if !frozen || n != 0 {
		t.Fatalf("frozen=%v n=%d, want frozen and no queue", frozen, n)
	}
	if !freezeCollectQueuesIfIdle() {
		t.Fatal("idle frozen round should be ready to seal")
	}
	if ok, err := tryBeginCollectQueue(); ok || err != errSearchSealPending {
		t.Fatalf("second tryBegin = %v, %v", ok, err)
	}
	collectQueues.mu.Lock()
	n = collectQueues.n
	collectQueues.mu.Unlock()
	if n != 0 {
		t.Fatalf("frozen round accepted a queue, n=%d", n)
	}
}

func TestResealQueue_IdleDoesNotCompleteSealing(t *testing.T) {
	prevWait := waitCollectIdle
	prevApply := sealSearchIndex
	t.Cleanup(func() {
		waitCollectIdle = prevWait
		sealSearchIndex = prevApply
		collectLifecycle.endSource("idle-seal")
		resetSearchReseal()
	})
	if err := collectLifecycle.beginSource("idle-seal"); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	waitCollectIdle = func() { <-release }
	sealSearchIndex = func([]int64, bool) bool {
		filmsnapshot.NotifySearchIndexProgress(10, 0, "indexing")
		return false
	}

	progress.Ensure("idle-seal", "IdleSeal")
	progress.Update("idle-seal", func(p *model.CollectProgress) {
		p.Status = progress.StatusSealing
	})
	searchReseal.addSealing("idle-seal")
	scheduleSearchReseal([]int64{999})
	collectLifecycle.endSource("idle-seal")
	close(release)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		searchReseal.mu.Lock()
		awaiting := searchReseal.awaitDone
		searchReseal.mu.Unlock()
		if awaiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	searchReseal.mu.Lock()
	awaiting := searchReseal.awaitDone
	searchReseal.mu.Unlock()
	if !awaiting {
		t.Fatal("expected awaitDone while rebuild in progress")
	}

	// 无关/失败 idle 不得收口 sealing。
	filmsnapshot.NotifySearchIndexProgress(0, 0, "idle")
	if snap, _ := progress.Snapshot("idle-seal"); snap.Status != progress.StatusSealing {
		t.Fatalf("idle 后应保持 sealing, got %q", snap.Status)
	}
	searchReseal.mu.Lock()
	awaiting = searchReseal.awaitDone
	searchReseal.mu.Unlock()
	if awaiting {
		t.Fatal("idle 应清掉 awaitDone，避免后续无关 done 误收口")
	}
}

func TestResealQueue_UnrelatedDoneDoesNotCompleteWithoutAwait(t *testing.T) {
	t.Cleanup(resetSearchReseal)
	progress.Ensure("no-await", "NoAwait")
	progress.Update("no-await", func(p *model.CollectProgress) {
		p.Status = progress.StatusSealing
	})
	searchReseal.mu.Lock()
	searchReseal.sealing["no-await"] = struct{}{}
	searchReseal.awaitDone = false
	searchReseal.mu.Unlock()

	filmsnapshot.NotifySearchIndexProgress(1, 1, "done")
	if snap, _ := progress.Snapshot("no-await"); snap.Status != progress.StatusSealing {
		t.Fatalf("未停等重建时 done 不应收口, got %q", snap.Status)
	}
}

func TestResealQueue_StaysSealingUntilRebuildDone(t *testing.T) {
	prevWait := waitCollectIdle
	prevApply := sealSearchIndex
	t.Cleanup(func() {
		waitCollectIdle = prevWait
		sealSearchIndex = prevApply
		collectLifecycle.endSource("rebuild-seal")
		resetSearchReseal()
	})
	if err := collectLifecycle.beginSource("rebuild-seal"); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	waitCollectIdle = func() { <-release }
	sealSearchIndex = func([]int64, bool) bool {
		filmsnapshot.NotifySearchIndexProgress(10, 0, "indexing")
		return false
	}

	progress.Ensure("rebuild-seal", "RebuildSealSource")
	progress.Update("rebuild-seal", func(p *model.CollectProgress) {
		p.Status = progress.StatusSealing
	})
	searchReseal.addSealing("rebuild-seal")
	scheduleSearchReseal([]int64{999})
	collectLifecycle.endSource("rebuild-seal")
	close(release)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		searchReseal.mu.Lock()
		running := searchReseal.running
		searchReseal.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap, _ := progress.Snapshot("rebuild-seal"); snap.Status != progress.StatusSealing {
		t.Fatalf("重建未结束时应保持待检索, got %q", snap.Status)
	}

	filmsnapshot.NotifySearchIndexProgress(10, 10, "done")
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if snap, _ := progress.Snapshot("rebuild-seal"); snap.Status == progress.StatusDone {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap, _ := progress.Snapshot("rebuild-seal")
	t.Fatalf("重建结束后应收入完成, got %q", snap.Status)
}

func TestResealQueue_SealsMidsArrivedDuringFailedSeal(t *testing.T) {
	prevWait := waitCollectIdle
	prevApply := sealSearchIndex
	t.Cleanup(func() {
		waitCollectIdle = prevWait
		sealSearchIndex = prevApply
		collectLifecycle.endSource("rebuild-more")
		resetSearchReseal()
	})
	if err := collectLifecycle.beginSource("rebuild-more"); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	waitCollectIdle = func() { <-release }

	second := make(chan []int64, 1)
	sealSearchIndex = func(mids []int64, _ bool) bool {
		if len(mids) == 1 && mids[0] == 999 {
			filmsnapshot.NotifySearchIndexProgress(10, 0, "indexing")
			scheduleSearchReseal([]int64{1000})
			return false
		}
		filmsnapshot.NotifySearchIndexProgress(1, 1, "done")
		second <- append([]int64(nil), mids...)
		return true
	}

	progress.Ensure("rebuild-more", "RebuildMore")
	progress.Update("rebuild-more", func(p *model.CollectProgress) {
		p.Status = progress.StatusSealing
	})
	searchReseal.addSealing("rebuild-more")
	scheduleSearchReseal([]int64{999})
	collectLifecycle.endSource("rebuild-more")
	close(release)

	select {
	case mids := <-second:
		if len(mids) != 1 || mids[0] != 1000 {
			t.Fatalf("second seal = %v", mids)
		}
	case <-time.After(time.Second):
		t.Fatal("failed seal did not seal mids that arrived during rebuild")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if snap, _ := progress.Snapshot("rebuild-more"); snap.Status == progress.StatusDone {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap, _ := progress.Snapshot("rebuild-more")
	t.Fatalf("后续编号封完后应收入完成, got %q", snap.Status)
}
