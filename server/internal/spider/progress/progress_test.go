package progress

import (
	"testing"
	"time"

	"server/internal/model"
)

func TestIsAlreadyQueuedOrRunningRespectsActiveAndStale(t *testing.T) {
	const sourceID = "progress-stale-1"

	// 清理可能残留的状态
	store.Delete(sourceID)
	tasks.Delete(sourceID)

	// 1) 无进度 → 不阻挡
	if IsAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected not blocking when no progress")
	}

	// 2) 新鲜 running → 阻挡
	state := ensure(sourceID, "T")
	state.mu.Lock()
	state.data.Status = StatusRunning
	state.updated = time.Now()
	state.mu.Unlock()
	if !IsAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected blocking on fresh running")
	}

	// 3) 无 live 的 running 超时 → failed 且不阻挡
	state.mu.Lock()
	state.data.Status = StatusRunning
	state.updated = time.Now().Add(-staleDuration() - time.Second)
	state.mu.Unlock()
	if IsAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected not blocking after stale running without live task")
	}
	snap, ok := Snapshot(sourceID)
	if !ok || snap.Status != StatusFailed {
		t.Fatalf("expected status failed after stale running, got ok=%v status=%q", ok, snap.Status)
	}

	// 4) live tasks + running → 阻挡（即使 updated 很旧）
	tasks.Store(sourceID, task{cancel: func() {}, reqID: "req"})
	state.mu.Lock()
	state.data.Status = StatusRunning
	state.updated = time.Now().Add(-staleDuration() - time.Minute)
	state.mu.Unlock()
	if !IsAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected blocking while live tasks running")
	}
	tasks.Delete(sourceID)

	// 5) 占用中的 starting 即使很久仍是排队
	SetOccupyChecker(func(id string) bool { return id == sourceID })
	state.mu.Lock()
	state.data.Status = StatusStarting
	state.data.QueueId = "q-queued"
	state.updated = time.Now().Add(-staleDuration() - time.Minute)
	state.mu.Unlock()
	if !IsAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected blocking on occupied long starting")
	}
	snap, ok = Snapshot(sourceID)
	if !ok || snap.Status != StatusStarting {
		t.Fatalf("occupied starting must not stale-fail, got ok=%v status=%q", ok, snap.Status)
	}
	SetOccupyChecker(nil)

	// 6) 无占用、无 live 的 starting 超时 → failed，不阻挡
	state.mu.Lock()
	state.data.Status = StatusStarting
	state.updated = time.Now().Add(-staleDuration() - time.Minute)
	state.mu.Unlock()
	if IsAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("abandoned starting should not block after stale")
	}
	snap, ok = Snapshot(sourceID)
	if !ok || snap.Status != StatusFailed {
		t.Fatalf("abandoned starting should stale-fail, got ok=%v status=%q", ok, snap.Status)
	}
	store.Delete(sourceID)
}

func TestGetActiveTaskProgressPurgesTerminalPerSource(t *testing.T) {
	const (
		failedID = "progress-failed-early"
		activeID = "progress-active-still"
	)
	for _, id := range []string{failedID, activeID} {
		store.Delete(id)
		tasks.Delete(id)
	}
	t.Cleanup(func() {
		for _, id := range []string{failedID, activeID} {
			store.Delete(id)
			tasks.Delete(id)
		}
	})

	s1 := ensure(failedID, "FailedEarly")
	s1.mu.Lock()
	s1.data.Status = StatusFailed
	s1.data.Total = 10
	s1.data.Failed = 10
	s1.updated = time.Now().Add(-retainDuration() * 3)
	s1.mu.Unlock()

	s2 := ensure(activeID, "Active")
	s2.mu.Lock()
	s2.data.Status = StatusRunning
	s2.data.Total = 5
	s2.data.Success = 5
	s2.updated = time.Now()
	s2.mu.Unlock()

	list := GetActiveTaskProgress()
	byID := map[string]model.CollectProgress{}
	for _, p := range list {
		byID[p.Id] = p
	}
	if _, ok := byID[failedID]; ok {
		t.Fatal("expired terminal progress must not wait for other queues")
	}
	if _, ok := store.Load(failedID); ok {
		t.Fatal("expected early failed purged on its own retain window")
	}
	if p, ok := byID[activeID]; !ok || p.Status != StatusRunning {
		t.Fatalf("expected active running, got ok=%v status=%q", ok, p.Status)
	}
}

func TestMarkStoppedDoesNotClobberFailed(t *testing.T) {
	const sourceID = "progress-failed-not-stopped"
	store.Delete(sourceID)
	tasks.Delete(sourceID)
	t.Cleanup(func() {
		store.Delete(sourceID)
		tasks.Delete(sourceID)
	})

	state := ensure(sourceID, "FailedSource")
	state.mu.Lock()
	state.data.Status = StatusFailed
	state.data.Success = 672
	state.data.Failed = 10
	state.mu.Unlock()

	MarkStopped(sourceID)
	snap, ok := Snapshot(sourceID)
	if !ok || snap.Status != StatusFailed {
		t.Fatalf("连续失败终态不得被标成 stopped, got ok=%v status=%q", ok, snap.Status)
	}
}

func TestStampPageRunningSkipsTerminal(t *testing.T) {
	failed := model.CollectProgress{Status: StatusFailed, Current: 10}
	StampPageRunning(&failed, 20)
	if failed.Status != StatusFailed {
		t.Fatalf("failed 不可被写回 running, got %q", failed.Status)
	}
	if failed.Current != 10 {
		t.Fatalf("终态页码不应被推进, got %d", failed.Current)
	}

	stopped := model.CollectProgress{Status: StatusStopped, Current: 5}
	StampPageRunning(&stopped, 8)
	if stopped.Status != StatusStopped {
		t.Fatalf("stopped 不可被写回 running, got %q", stopped.Status)
	}

	done := model.CollectProgress{Status: StatusDone, Current: 5}
	StampPageRunning(&done, 9)
	if done.Status != StatusDone {
		t.Fatalf("done 不可被写回 running, got %q", done.Status)
	}

	running := model.CollectProgress{Status: StatusStarting, Current: 1}
	StampPageRunning(&running, 3)
	if running.Status != StatusRunning {
		t.Fatalf("starting 应进入 running, got %q", running.Status)
	}
	if running.Current != 3 {
		t.Fatalf("running 页码应为 3, got %d", running.Current)
	}
}

func TestMarkSourceDone(t *testing.T) {
	const sourceID = "mark-source-done"
	store.Delete(sourceID)
	t.Cleanup(func() { store.Delete(sourceID) })

	ensure(sourceID, "DoneSource")
	Update(sourceID, func(p *model.CollectProgress) {
		p.Status = StatusRunning
		p.Total = 10
		p.Success = 10
		p.Current = 10
	})
	MarkSourceDone(sourceID)
	snap, ok := Snapshot(sourceID)
	if !ok || snap.Status != StatusDone {
		t.Fatalf("MarkSourceDone 应变 done, got ok=%v status=%q", ok, snap.Status)
	}
}

func TestGetActiveTaskProgressClearsTerminalPerSourceRetain(t *testing.T) {
	const (
		oldFailedID = "progress-old-failed"
		newDoneID   = "progress-new-done"
	)
	for _, id := range []string{oldFailedID, newDoneID} {
		store.Delete(id)
		tasks.Delete(id)
	}
	t.Cleanup(func() {
		for _, id := range []string{oldFailedID, newDoneID} {
			store.Delete(id)
			tasks.Delete(id)
		}
	})

	s1 := ensure(oldFailedID, "OldFailed")
	s1.mu.Lock()
	s1.data.Status = StatusFailed
	s1.updated = time.Now().Add(-retainDuration() * 5)
	s1.mu.Unlock()

	s2 := ensure(newDoneID, "NewDone")
	s2.mu.Lock()
	s2.data.Status = StatusDone
	s2.updated = time.Now().Add(-retainDuration() / 2)
	s2.mu.Unlock()

	list := GetActiveTaskProgress()
	byID := map[string]model.CollectProgress{}
	for _, p := range list {
		byID[p.Id] = p
	}
	if _, ok := byID[oldFailedID]; ok {
		t.Fatal("old failed should already be purged on its own retain window")
	}
	if _, ok := byID[newDoneID]; !ok {
		t.Fatal("expected new done kept within retain window")
	}

	s2.mu.Lock()
	s2.updated = time.Now().Add(-retainDuration() - time.Second)
	s2.mu.Unlock()

	list = GetActiveTaskProgress()
	if len(list) != 0 {
		t.Fatalf("expected expired terminal progress cleared, got %d items", len(list))
	}
	if _, ok := store.Load(newDoneID); ok {
		t.Fatal("expected new done deleted after its retain window")
	}
}

func TestGetActiveTaskProgressMarksStaleRunningWithoutLive(t *testing.T) {
	const sourceID = "progress-stale-running"
	store.Delete(sourceID)
	tasks.Delete(sourceID)

	state := ensure(sourceID, "StaleRun")
	state.mu.Lock()
	state.data.Status = StatusRunning
	state.updated = time.Now().Add(-staleDuration() - time.Second)
	state.mu.Unlock()

	list := GetActiveTaskProgress()
	found := false
	for _, p := range list {
		if p.Id == sourceID {
			found = true
			if p.Status != StatusFailed {
				t.Fatalf("expected failed after stale running, got %q", p.Status)
			}
		}
	}
	if !found {
		t.Fatal("expected stale running to appear as failed within retain window")
	}

	snap, ok := Snapshot(sourceID)
	if !ok || snap.Status != StatusFailed {
		t.Fatalf("map status should be failed, ok=%v status=%q", ok, snap.Status)
	}
	store.Delete(sourceID)
}

func TestGetActiveTaskProgressDoesNotStaleQueuedStarting(t *testing.T) {
	const sourceID = "progress-queued-starting"
	const queueID = "q-batch-12"
	store.Delete(sourceID)
	tasks.Delete(sourceID)
	SetOccupyChecker(func(id string) bool { return id == sourceID })
	t.Cleanup(func() {
		SetOccupyChecker(nil)
		store.Delete(sourceID)
		tasks.Delete(sourceID)
	})

	state := ensure(sourceID, "HD(BF)")
	state.mu.Lock()
	state.data.Status = StatusStarting
	state.data.QueueId = queueID
	state.updated = time.Now().Add(-staleDuration() - time.Minute)
	state.mu.Unlock()

	list := GetActiveTaskProgress()
	found := false
	for _, p := range list {
		if p.Id == sourceID {
			found = true
			if p.Status != StatusStarting {
				t.Fatalf("queued starting must stay starting, got %q", p.Status)
			}
			if p.QueueId != queueID {
				t.Fatalf("queued starting must keep queueId, got %q", p.QueueId)
			}
		}
	}
	if !found {
		t.Fatal("expected long queued starting still listed")
	}

	snap, ok := Snapshot(sourceID)
	if !ok || snap.Status != StatusStarting {
		t.Fatalf("map status should remain starting, ok=%v status=%q", ok, snap.Status)
	}
	if snap.QueueId != queueID {
		t.Fatalf("map queueId should remain %q, got %q", queueID, snap.QueueId)
	}

	pruneStale()
	snap, ok = Snapshot(sourceID)
	if !ok || snap.Status != StatusStarting || snap.QueueId != queueID {
		t.Fatalf("housekeeping must not stale-fail queued starting, ok=%v status=%q queueId=%q",
			ok, snap.Status, snap.QueueId)
	}
}

func TestGetActiveTaskProgressMarksStaleAbandonedStarting(t *testing.T) {
	const sourceID = "progress-abandoned-starting"
	store.Delete(sourceID)
	tasks.Delete(sourceID)
	t.Cleanup(func() {
		store.Delete(sourceID)
		tasks.Delete(sourceID)
	})

	state := ensure(sourceID, "Gone")
	state.mu.Lock()
	state.data.Status = StatusStarting
	state.updated = time.Now().Add(-staleDuration() - time.Second)
	state.mu.Unlock()

	list := GetActiveTaskProgress()
	found := false
	for _, p := range list {
		if p.Id == sourceID {
			found = true
			if p.Status != StatusFailed {
				t.Fatalf("abandoned starting should stale-fail, got %q", p.Status)
			}
		}
	}
	if !found {
		t.Fatal("expected abandoned starting to appear as failed within retain window")
	}
}

func TestMarkSourcesCollectStartingSetsQueueId(t *testing.T) {
	const (
		sourceA = "queue-mark-a"
		sourceB = "queue-mark-b"
		queueID = "q-test-queue"
	)
	for _, id := range []string{sourceA, sourceB} {
		store.Delete(id)
		tasks.Delete(id)
	}
	t.Cleanup(func() {
		for _, id := range []string{sourceA, sourceB} {
			store.Delete(id)
			tasks.Delete(id)
		}
	})

	MarkSourcesCollectStarting([]model.FilmSource{
		{Id: sourceA, Name: "A"},
		{Id: sourceB, Name: "B"},
	}, queueID)

	snapA, okA := Snapshot(sourceA)
	snapB, okB := Snapshot(sourceB)
	if !okA || !okB {
		t.Fatal("expected starting progress for both sources")
	}
	if snapA.QueueId != queueID || snapB.QueueId != queueID {
		t.Fatalf("queueId = %q / %q; want %q", snapA.QueueId, snapB.QueueId, queueID)
	}
	if snapA.Status != StatusStarting || snapB.Status != StatusStarting {
		t.Fatalf("status = %q / %q; want starting", snapA.Status, snapB.Status)
	}
}

func TestMarkSourcesCollectStarting_RetryOverwritesQueueId(t *testing.T) {
	const sourceID = "queue-retry-ik"
	store.Delete(sourceID)
	tasks.Delete(sourceID)
	t.Cleanup(func() {
		store.Delete(sourceID)
		tasks.Delete(sourceID)
	})

	MarkSourcesCollectStarting([]model.FilmSource{{Id: sourceID, Name: "HD(IK)"}}, "q-batch-12")
	Update(sourceID, func(p *model.CollectProgress) {
		p.Status = StatusFailed
	})
	MarkSourcesCollectStarting([]model.FilmSource{{Id: sourceID, Name: "HD(IK)"}}, "q-single-retry")

	snap, ok := Snapshot(sourceID)
	if !ok {
		t.Fatal("expected progress after retry mark")
	}
	if snap.QueueId != "q-single-retry" {
		t.Fatalf("retry queueId = %q, want q-single-retry", snap.QueueId)
	}
	if snap.Status != StatusStarting {
		t.Fatalf("retry status = %q, want starting", snap.Status)
	}
}
