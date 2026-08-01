package spider

import (
	"testing"
	"time"

	"server/internal/model"
)

func TestIsCollectAlreadyQueuedOrRunningRespectsActiveAndStale(t *testing.T) {
	const sourceID = "progress-stale-1"

	// 清理可能残留的状态
	collectProgress.Delete(sourceID)
	activeTasks.Delete(sourceID)

	// 1) 无进度 → 不阻挡
	if isCollectAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected not blocking when no progress")
	}

	// 2) waiting_publish 新鲜 → 阻挡
	state := ensureCollectProgress(sourceID, "T")
	state.mu.Lock()
	state.data.Status = progressStatusWaitingPublish
	state.updated = time.Now()
	state.mu.Unlock()
	if !isCollectAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected blocking on fresh waiting_publish")
	}

	// 3) waiting_publish 超时 → 标 failed 且不阻挡
	state.mu.Lock()
	state.data.Status = progressStatusWaitingPublish
	state.updated = time.Now().Add(-progressStaleDuration() - time.Second)
	state.mu.Unlock()
	if isCollectAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected not blocking after stale waiting_publish")
	}
	snap, ok := collectProgressSnapshot(sourceID)
	if !ok || snap.Status != progressStatusFailed {
		t.Fatalf("expected status failed after stale prune, got ok=%v status=%q", ok, snap.Status)
	}

	// 4) live activeTasks + running → 阻挡（即使 updated 很旧）
	activeTasks.Store(sourceID, collectTask{cancel: func() {}, reqId: "req"})
	state.mu.Lock()
	state.data.Status = progressStatusRunning
	state.updated = time.Now().Add(-progressStaleDuration() - time.Minute)
	state.mu.Unlock()
	if !isCollectAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected blocking while live activeTasks running")
	}
	activeTasks.Delete(sourceID)
	collectProgress.Delete(sourceID)
}

func TestGetActiveTaskProgressRetainsDoneAndDropsExpired(t *testing.T) {
	const (
		doneID    = "progress-done-1"
		expiredID = "progress-expired-1"
		activeID  = "progress-active-1"
	)
	for _, id := range []string{doneID, expiredID, activeID} {
		collectProgress.Delete(id)
		activeTasks.Delete(id)
	}

	// 新鲜 done：应出现在列表
	s1 := ensureCollectProgress(doneID, "Done")
	s1.mu.Lock()
	s1.data.Status = progressStatusDone
	s1.data.Total = 10
	s1.data.Success = 10
	s1.updated = time.Now()
	s1.mu.Unlock()

	// 过期 done：应被删除且不出现
	s2 := ensureCollectProgress(expiredID, "Expired")
	s2.mu.Lock()
	s2.data.Status = progressStatusDone
	s2.updated = time.Now().Add(-progressRetainDuration() - time.Second)
	s2.mu.Unlock()

	// 活跃 waiting_publish：应出现
	s3 := ensureCollectProgress(activeID, "Active")
	s3.mu.Lock()
	s3.data.Status = progressStatusWaitingPublish
	s3.data.Total = 5
	s3.data.Success = 5
	s3.updated = time.Now()
	s3.mu.Unlock()

	list := GetActiveTaskProgress()
	byID := map[string]model.CollectProgress{}
	for _, p := range list {
		byID[p.Id] = p
	}

	if p, ok := byID[doneID]; !ok || p.Status != progressStatusDone {
		t.Fatalf("expected retained done progress, got ok=%v status=%q", ok, p.Status)
	}
	if _, ok := byID[expiredID]; ok {
		t.Fatal("expected expired done progress to be omitted")
	}
	if _, ok := collectProgress.Load(expiredID); ok {
		t.Fatal("expected expired progress entry deleted from map")
	}
	if p, ok := byID[activeID]; !ok || p.Status != progressStatusWaitingPublish {
		t.Fatalf("expected active waiting_publish, got ok=%v status=%q", ok, p.Status)
	}

	for _, id := range []string{doneID, expiredID, activeID} {
		collectProgress.Delete(id)
	}
}

func TestGetActiveTaskProgressMarksStaleWaitingPublishFailed(t *testing.T) {
	const sourceID = "progress-stale-list"
	collectProgress.Delete(sourceID)
	activeTasks.Delete(sourceID)

	state := ensureCollectProgress(sourceID, "Stale")
	state.mu.Lock()
	state.data.Status = progressStatusWaitingPublish
	state.updated = time.Now().Add(-progressStaleDuration() - time.Second)
	state.mu.Unlock()

	list := GetActiveTaskProgress()
	found := false
	for _, p := range list {
		if p.Id == sourceID {
			found = true
			if p.Status != progressStatusFailed {
				t.Fatalf("expected failed after stale, got %q", p.Status)
			}
		}
	}
	if !found {
		// 刚变 failed 且 retain 窗口内应仍可见
		t.Fatal("expected stale progress to appear as failed within retain window")
	}

	snap, ok := collectProgressSnapshot(sourceID)
	if !ok || snap.Status != progressStatusFailed {
		t.Fatalf("map status should be failed, ok=%v status=%q", ok, snap.Status)
	}
	collectProgress.Delete(sourceID)
}

func TestMarkSourcePagesFinishedStatuses(t *testing.T) {
	const singleID = "pages-finished-single"
	const batchID = "pages-finished-batch"
	for _, id := range []string{singleID, batchID} {
		collectProgress.Delete(id)
	}

	ensureCollectProgress(singleID, "S")
	updateCollectProgress(singleID, func(p *model.CollectProgress) {
		p.Status = progressStatusRunning
		p.Total = 3
		p.Success = 3
	})
	markSourcePagesFinished(singleID, true)
	if snap, _ := collectProgressSnapshot(singleID); snap.Status != progressStatusPageDone {
		t.Fatalf("single flushAtEnd want page_done, got %q", snap.Status)
	}

	ensureCollectProgress(batchID, "B")
	updateCollectProgress(batchID, func(p *model.CollectProgress) {
		p.Status = progressStatusRunning
		p.Total = 3
		p.Success = 3
	})
	markSourcePagesFinished(batchID, false)
	if snap, _ := collectProgressSnapshot(batchID); snap.Status != progressStatusWaitingPublish {
		t.Fatalf("batch want waiting_publish, got %q", snap.Status)
	}

	for _, id := range []string{singleID, batchID} {
		collectProgress.Delete(id)
	}
}
