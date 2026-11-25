package collect

import (
	"testing"
	"time"

	"server/internal/model"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/collect/progress"
)

func TestSearchResealRetryWaitBacksOffAndCaps(t *testing.T) {
	prevWait := sealRetryWait
	sealRetryWait = 2 * time.Second
	t.Cleanup(func() { sealRetryWait = prevWait })

	q := &searchResealQueue{}
	want := []time.Duration{
		2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, time.Minute, time.Minute,
	}
	for _, expected := range want {
		if got := q.nextSealRetryWait(); got != expected {
			t.Fatalf("nextSealRetryWait() = %s, want %s", got, expected)
		}
	}
	q.resetSealRetryWait()
	if got := q.nextSealRetryWait(); got != 2*time.Second {
		t.Fatalf("after reset nextSealRetryWait() = %s, want 2s", got)
	}
}

func TestSealFrozenRound_RestoresMidsWhenSealFails(t *testing.T) {
	resetSearchReseal()
	prevSeal := sealSearchIndex
	prevWait := sealRetryWait
	t.Cleanup(func() {
		sealSearchIndex = prevSeal
		sealRetryWait = prevWait
		resetSearchReseal()
	})
	sealRetryWait = 0
	progress.Ensure("fail-seal", "失败站")
	progress.Update("fail-seal", func(p *model.CollectProgress) {
		p.Status = progress.StatusSealing
	})

	attempts := 0
	sealSearchIndex = func(mids []int64) bool {
		attempts++
		if len(mids) != 2 {
			t.Errorf("mids=%d", len(mids))
		}
		return attempts > 1
	}

	searchReseal.mu.Lock()
	searchReseal.pending[11] = struct{}{}
	searchReseal.pending[12] = struct{}{}
	searchReseal.sealing["fail-seal"] = struct{}{}
	searchReseal.running = true
	searchReseal.mu.Unlock()

	if searchReseal.sealFrozenRound() {
		t.Fatal("封口失败应留下这一轮")
	}
	searchReseal.mu.Lock()
	pending := len(searchReseal.pending)
	_, sealing := searchReseal.sealing["fail-seal"]
	searchReseal.mu.Unlock()
	if pending != 2 || !sealing {
		t.Fatalf("pending=%d sealing=%v", pending, sealing)
	}
	if snap, ok := progress.Snapshot("fail-seal"); !ok || snap.Status != progress.StatusSealing {
		t.Fatalf("status=%s ok=%v", snap.Status, ok)
	}

	if searchReseal.sealFrozenRound() {
		t.Fatal("封口成功后应回到循环")
	}
	searchReseal.mu.Lock()
	pending = len(searchReseal.pending)
	_, sealing = searchReseal.sealing["fail-seal"]
	searchReseal.mu.Unlock()
	if pending != 0 || sealing {
		t.Fatalf("pending=%d sealing=%v", pending, sealing)
	}
	if snap, ok := progress.Snapshot("fail-seal"); !ok || snap.Status != progress.StatusDone {
		t.Fatalf("status=%s ok=%v", snap.Status, ok)
	}
}

func TestSealFrozenRound_RestoresMidsEvenIfProgressChangesWithoutRebuild(t *testing.T) {
	resetSearchReseal()
	prevSeal := sealSearchIndex
	prevWait := sealRetryWait
	t.Cleanup(func() {
		sealSearchIndex = prevSeal
		sealRetryWait = prevWait
		resetSearchReseal()
	})
	sealRetryWait = 0

	sealSearchIndex = func(mids []int64) bool {
		// 模拟封口失败（未触发重建），期间另一路触发进度变更为 waiting
		filmsnapshot.NotifySearchIndexProgress(0, 0, "waiting")
		return false
	}

	searchReseal.mu.Lock()
	searchReseal.pending[21] = struct{}{}
	searchReseal.pending[22] = struct{}{}
	searchReseal.running = true
	searchReseal.mu.Unlock()

	if searchReseal.sealFrozenRound() {
		t.Fatal("未触发全量重建的封口失败应放回编号重试")
	}
	searchReseal.mu.Lock()
	_, has21 := searchReseal.pending[21]
	_, has22 := searchReseal.pending[22]
	searchReseal.mu.Unlock()
	if !has21 || !has22 {
		t.Fatalf("progress token 变更但不涉及重建时不应丢失 mids, has21=%v has22=%v", has21, has22)
	}
}

func TestSearchResealRetryAfterIdle_CompletesOnRebuildDone(t *testing.T) {
	setupAuditSpiderDB(t)
	resetSearchReseal()
	t.Cleanup(func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) && (filmsnapshot.SearchRebuildScheduled() || filmsnapshot.IsSearchIndexBuilding()) {
			time.Sleep(10 * time.Millisecond)
		}
		resetSearchReseal()
	})

	progress.Ensure("retry-idle-r1", "RetryIdleR1")
	progress.Update("retry-idle-r1", func(p *model.CollectProgress) {
		p.Status = progress.StatusSealing
	})
	searchReseal.mu.Lock()
	searchReseal.sealing["retry-idle-r1"] = struct{}{}
	searchReseal.mu.Unlock()

	searchReseal.retryAfterIdle()
	// 在 500ms 防抖+空表重建结束后，应由 awaitDone 捕获 done 终态并释放 sealing，同时等待重建任务彻底结束。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snap, ok := progress.Snapshot("retry-idle-r1")
		if ok && snap.Status == progress.StatusDone && !filmsnapshot.SearchRebuildScheduled() && !filmsnapshot.IsSearchIndexBuilding() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap, _ := progress.Snapshot("retry-idle-r1")
	t.Fatalf("retryAfterIdle 重建完成后应释放 StatusSealing 且重建变为空闲，status=%q scheduled=%t building=%t",
		snap.Status, filmsnapshot.SearchRebuildScheduled(), filmsnapshot.IsSearchIndexBuilding())
}

func TestSearchResealRetryAfterIdle_RequeuesWhenNotScheduled(t *testing.T) {
	resetSearchReseal()
	prevWait := sealRetryWait
	sealRetryWait = 5 * time.Millisecond
	t.Cleanup(func() {
		sealRetryWait = prevWait
		resetSearchReseal()
	})

	searchReseal.mu.Lock()
	searchReseal.sealing["requeue-r1"] = struct{}{}
	searchReseal.awaitDone = true
	searchReseal.mu.Unlock()

	searchReseal.reconcileRetrySchedule(false)

	searchReseal.mu.Lock()
	awaiting := searchReseal.awaitDone
	retries := searchReseal.rebuildRetries
	timerSet := searchReseal.retryTimer != nil
	searchReseal.mu.Unlock()
	if awaiting || retries != 1 || !timerSet {
		t.Fatalf("未调度时应复位 awaitDone 并重排重试，awaiting=%t retries=%d timerSet=%t", awaiting, retries, timerSet)
	}
}
