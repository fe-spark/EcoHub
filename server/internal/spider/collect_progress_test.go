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

	// 3) waiting_publish 即使很久仍阻挡，且不因超时标 failed（整批收尾等待）
	state.mu.Lock()
	state.data.Status = progressStatusWaitingPublish
	state.updated = time.Now().Add(-progressStaleDuration() - time.Minute)
	state.mu.Unlock()
	if !isCollectAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected blocking on long waiting_publish (batch finalize wait)")
	}
	snap, ok := collectProgressSnapshot(sourceID)
	if !ok || snap.Status != progressStatusWaitingPublish {
		t.Fatalf("waiting_publish must not stale-fail, got ok=%v status=%q", ok, snap.Status)
	}

	// 4) 无 live 的 running 超时 → failed 且不阻挡
	state.mu.Lock()
	state.data.Status = progressStatusRunning
	state.updated = time.Now().Add(-progressStaleDuration() - time.Second)
	state.mu.Unlock()
	if isCollectAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected not blocking after stale running without live task")
	}
	snap, ok = collectProgressSnapshot(sourceID)
	if !ok || snap.Status != progressStatusFailed {
		t.Fatalf("expected status failed after stale running, got ok=%v status=%q", ok, snap.Status)
	}

	// 5) live activeTasks + running → 阻挡（即使 updated 很旧）
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

func TestGetActiveTaskProgressKeepsTerminalWhileAnyActive(t *testing.T) {
	const (
		failedID = "progress-failed-early"
		activeID = "progress-active-still"
	)
	for _, id := range []string{failedID, activeID} {
		collectProgress.Delete(id)
		activeTasks.Delete(id)
	}

	// 很早就失败的进度：有活跃采集时仍须保留（不能先消失）
	s1 := ensureCollectProgress(failedID, "FailedEarly")
	s1.mu.Lock()
	s1.data.Status = progressStatusFailed
	s1.data.Total = 10
	s1.data.Failed = 10
	s1.updated = time.Now().Add(-progressRetainDuration() * 3)
	s1.mu.Unlock()

	s2 := ensureCollectProgress(activeID, "Active")
	s2.mu.Lock()
	s2.data.Status = progressStatusWaitingPublish
	s2.data.Total = 5
	s2.data.Success = 5
	s2.updated = time.Now()
	s2.mu.Unlock()

	list := GetActiveTaskProgress()
	byID := map[string]model.CollectProgress{}
	for _, p := range list {
		byID[p.Id] = p
	}
	if p, ok := byID[failedID]; !ok || p.Status != progressStatusFailed {
		t.Fatalf("expected early failed retained while active exists, got ok=%v status=%q", ok, p.Status)
	}
	if _, ok := collectProgress.Load(failedID); !ok {
		t.Fatal("expected early failed entry kept in map while active exists")
	}
	if p, ok := byID[activeID]; !ok || p.Status != progressStatusWaitingPublish {
		t.Fatalf("expected active waiting_publish, got ok=%v status=%q", ok, p.Status)
	}

	for _, id := range []string{failedID, activeID} {
		collectProgress.Delete(id)
	}
}

func TestGetActiveTaskProgressClearsAllTerminalTogether(t *testing.T) {
	const (
		oldFailedID = "progress-old-failed"
		newDoneID   = "progress-new-done"
	)
	for _, id := range []string{oldFailedID, newDoneID} {
		collectProgress.Delete(id)
		activeTasks.Delete(id)
	}

	// 早失败 + 晚完成：在「最晚终态 + retain」之前两者都在；之后一起消失
	s1 := ensureCollectProgress(oldFailedID, "OldFailed")
	s1.mu.Lock()
	s1.data.Status = progressStatusFailed
	s1.updated = time.Now().Add(-progressRetainDuration() * 5)
	s1.mu.Unlock()

	s2 := ensureCollectProgress(newDoneID, "NewDone")
	s2.mu.Lock()
	s2.data.Status = progressStatusDone
	s2.updated = time.Now().Add(-progressRetainDuration() / 2)
	s2.mu.Unlock()

	list := GetActiveTaskProgress()
	byID := map[string]model.CollectProgress{}
	for _, p := range list {
		byID[p.Id] = p
	}
	if _, ok := byID[oldFailedID]; !ok {
		t.Fatal("expected old failed kept until batch retain based on latest terminal")
	}
	if _, ok := byID[newDoneID]; !ok {
		t.Fatal("expected new done kept within retain window")
	}

	// 把最晚终态也推过保留窗口 → 应统一清空
	s2.mu.Lock()
	s2.updated = time.Now().Add(-progressRetainDuration() - time.Second)
	s2.mu.Unlock()

	list = GetActiveTaskProgress()
	if len(list) != 0 {
		t.Fatalf("expected all terminal progress cleared together, got %d items", len(list))
	}
	if _, ok := collectProgress.Load(oldFailedID); ok {
		t.Fatal("expected old failed deleted in unified purge")
	}
	if _, ok := collectProgress.Load(newDoneID); ok {
		t.Fatal("expected new done deleted in unified purge")
	}
}

func TestGetActiveTaskProgressDoesNotStaleWaitingPublish(t *testing.T) {
	const sourceID = "progress-wait-publish-long"
	collectProgress.Delete(sourceID)
	activeTasks.Delete(sourceID)

	state := ensureCollectProgress(sourceID, "WaitPublish")
	state.mu.Lock()
	state.data.Status = progressStatusWaitingPublish
	state.updated = time.Now().Add(-progressStaleDuration() - time.Minute)
	state.mu.Unlock()

	list := GetActiveTaskProgress()
	found := false
	for _, p := range list {
		if p.Id == sourceID {
			found = true
			if p.Status != progressStatusWaitingPublish {
				t.Fatalf("waiting_publish must stay, got %q", p.Status)
			}
		}
	}
	if !found {
		t.Fatal("expected long waiting_publish still listed")
	}

	snap, ok := collectProgressSnapshot(sourceID)
	if !ok || snap.Status != progressStatusWaitingPublish {
		t.Fatalf("map status should remain waiting_publish, ok=%v status=%q", ok, snap.Status)
	}
	collectProgress.Delete(sourceID)
}

func TestPrioritizeCollectSourcesMasterFirst(t *testing.T) {
	sources := []model.FilmSource{
		{Id: "s1", Name: "A", Grade: model.SlaveCollect},
		{Id: "m1", Name: "M", Grade: model.MasterCollect},
		{Id: "s2", Name: "B", Grade: model.SlaveCollect},
	}
	out := prioritizeCollectSources(sources)
	if len(out) != 3 || out[0].Id != "m1" {
		t.Fatalf("master should be first, got %+v", out)
	}
	if out[1].Id != "s1" || out[2].Id != "s2" {
		t.Fatalf("slave order should be stable, got %+v", out)
	}
}

func TestGetActiveTaskProgressMarksStaleRunningWithoutLive(t *testing.T) {
	const sourceID = "progress-stale-running"
	collectProgress.Delete(sourceID)
	activeTasks.Delete(sourceID)

	state := ensureCollectProgress(sourceID, "StaleRun")
	state.mu.Lock()
	state.data.Status = progressStatusRunning
	state.updated = time.Now().Add(-progressStaleDuration() - time.Second)
	state.mu.Unlock()

	list := GetActiveTaskProgress()
	found := false
	for _, p := range list {
		if p.Id == sourceID {
			found = true
			if p.Status != progressStatusFailed {
				t.Fatalf("expected failed after stale running, got %q", p.Status)
			}
		}
	}
	if !found {
		t.Fatal("expected stale running to appear as failed within retain window")
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

// 0 页时列表状态须与生命周期一致：批量 → waiting_publish，单站 → page_done（再由 defer 收尾）。
func TestZeroPageProgressStatusMatchesLifecycle(t *testing.T) {
	const (
		batchID  = "zero-page-batch"
		singleID = "zero-page-single"
	)
	for _, id := range []string{batchID, singleID} {
		collectProgress.Delete(id)
	}

	// 模拟 handleCollect 0 页分支（与生产代码同一套状态赋值）
	markZeroPageProgress := func(id string, flushAtEnd bool) {
		ensureCollectProgress(id, id)
		updateCollectProgress(id, func(p *model.CollectProgress) {
			p.Total = 0
			p.Current = 0
			p.Success = 0
			p.Failed = 0
			if flushAtEnd {
				p.Status = progressStatusPageDone
			} else {
				p.Status = progressStatusWaitingPublish
			}
		})
	}

	markZeroPageProgress(batchID, false)
	if snap, _ := collectProgressSnapshot(batchID); snap.Status != progressStatusWaitingPublish {
		t.Fatalf("batch zero-page want waiting_publish, got %q", snap.Status)
	}
	if !isActiveCollectStatus(progressStatusWaitingPublish) {
		t.Fatal("waiting_publish must stay active so list shows 等待收尾")
	}

	markZeroPageProgress(singleID, true)
	if snap, _ := collectProgressSnapshot(singleID); snap.Status != progressStatusPageDone {
		t.Fatalf("single zero-page want page_done, got %q", snap.Status)
	}

	for _, id := range []string{batchID, singleID} {
		collectProgress.Delete(id)
	}
}
