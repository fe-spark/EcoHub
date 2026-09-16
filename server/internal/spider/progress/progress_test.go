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

	// 2) waiting_publish 新鲜 → 阻挡
	state := ensure(sourceID, "T")
	state.mu.Lock()
	state.data.Status = StatusWaitingPublish
	state.updated = time.Now()
	state.mu.Unlock()
	if !IsAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected blocking on fresh waiting_publish")
	}

	// 3) waiting_publish 即使很久仍阻挡，且不因超时标 failed（整批收尾等待）
	state.mu.Lock()
	state.data.Status = StatusWaitingPublish
	state.updated = time.Now().Add(-staleDuration() - time.Minute)
	state.mu.Unlock()
	if !IsAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected blocking on long waiting_publish (batch finalize wait)")
	}
	snap, ok := Snapshot(sourceID)
	if !ok || snap.Status != StatusWaitingPublish {
		t.Fatalf("waiting_publish must not stale-fail, got ok=%v status=%q", ok, snap.Status)
	}

	// 4) 无 live 的 running 超时 → failed 且不阻挡
	state.mu.Lock()
	state.data.Status = StatusRunning
	state.updated = time.Now().Add(-staleDuration() - time.Second)
	state.mu.Unlock()
	if IsAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected not blocking after stale running without live task")
	}
	snap, ok = Snapshot(sourceID)
	if !ok || snap.Status != StatusFailed {
		t.Fatalf("expected status failed after stale running, got ok=%v status=%q", ok, snap.Status)
	}

	// 5) live tasks + running → 阻挡（即使 updated 很旧）
	tasks.Store(sourceID, task{cancel: func() {}, reqID: "req"})
	state.mu.Lock()
	state.data.Status = StatusRunning
	state.updated = time.Now().Add(-staleDuration() - time.Minute)
	state.mu.Unlock()
	if !IsAlreadyQueuedOrRunning(sourceID) {
		t.Fatal("expected blocking while live tasks running")
	}
	tasks.Delete(sourceID)
	store.Delete(sourceID)
}

func TestGetActiveTaskProgressKeepsTerminalWhileAnyActive(t *testing.T) {
	const (
		failedID = "progress-failed-early"
		activeID = "progress-active-still"
	)
	for _, id := range []string{failedID, activeID} {
		store.Delete(id)
		tasks.Delete(id)
	}

	// 很早就失败的进度：有活跃采集时仍须保留（不能先消失）
	s1 := ensure(failedID, "FailedEarly")
	s1.mu.Lock()
	s1.data.Status = StatusFailed
	s1.data.Total = 10
	s1.data.Failed = 10
	s1.updated = time.Now().Add(-retainDuration() * 3)
	s1.mu.Unlock()

	s2 := ensure(activeID, "Active")
	s2.mu.Lock()
	s2.data.Status = StatusWaitingPublish
	s2.data.Total = 5
	s2.data.Success = 5
	s2.updated = time.Now()
	s2.mu.Unlock()

	list := GetActiveTaskProgress()
	byID := map[string]model.CollectProgress{}
	for _, p := range list {
		byID[p.Id] = p
	}
	if p, ok := byID[failedID]; !ok || p.Status != StatusFailed {
		t.Fatalf("expected early failed retained while active exists, got ok=%v status=%q", ok, p.Status)
	}
	if _, ok := store.Load(failedID); !ok {
		t.Fatal("expected early failed entry kept in map while active exists")
	}
	if p, ok := byID[activeID]; !ok || p.Status != StatusWaitingPublish {
		t.Fatalf("expected active waiting_publish, got ok=%v status=%q", ok, p.Status)
	}

	for _, id := range []string{failedID, activeID} {
		store.Delete(id)
	}
}

func TestCanEnterFinalizingSkipsStopped(t *testing.T) {
	if canEnterFinalizing(StatusStopped) {
		t.Fatal("用户停止后不应进入 finalizing，否则单站收尾会变成采集完成")
	}
	if canEnterFinalizing(StatusFailed) {
		t.Fatal("failed 不应进入 finalizing")
	}
	for _, status := range []string{
		StatusPageDone,
		StatusWaitingPublish,
		StatusRunning,
		StatusStarting,
	} {
		if !canEnterFinalizing(status) {
			t.Fatalf("%s 应能进入 finalizing", status)
		}
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

	running := model.CollectProgress{Status: StatusStarting, Current: 1}
	StampPageRunning(&running, 3)
	if running.Status != StatusRunning {
		t.Fatalf("starting 应进入 running, got %q", running.Status)
	}
	if running.Current != 3 {
		t.Fatalf("running 页码应为 3, got %d", running.Current)
	}

	wrapping := model.CollectProgress{Status: StatusWaitingPublish, Current: 5}
	StampPageRunning(&wrapping, 9)
	if wrapping.Status != StatusWaitingPublish {
		t.Fatalf("收尾态不可被写回 running, got %q", wrapping.Status)
	}
}

func TestMarkSourcePagesFinishedAfterAbortEntersWrapUp(t *testing.T) {
	const sourceID = "abort-wrap-up"
	store.Delete(sourceID)
	t.Cleanup(func() { store.Delete(sourceID) })

	ensure(sourceID, "AbortWrap")
	Update(sourceID, func(p *model.CollectProgress) {
		p.Status = StatusRunning
		p.Total = 698
		p.Success = 672
		p.Failed = 10
		p.Current = 682
	})
	MarkSourcePagesFinished(sourceID, false)
	snap, ok := Snapshot(sourceID)
	if !ok || snap.Status != StatusWaitingPublish {
		t.Fatalf("连续失败停抓且有成功入库应变 waiting_publish, got ok=%v status=%q", ok, snap.Status)
	}
	if !isActiveStatus(snap.Status) {
		t.Fatal("收尾态必须仍在采集生命周期内")
	}
}

func TestGetActiveTaskProgressClearsAllTerminalTogether(t *testing.T) {
	const (
		oldFailedID = "progress-old-failed"
		newDoneID   = "progress-new-done"
	)
	for _, id := range []string{oldFailedID, newDoneID} {
		store.Delete(id)
		tasks.Delete(id)
	}

	// 早失败 + 晚完成：在「最晚终态 + retain」之前两者都在；之后一起消失
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
	if _, ok := byID[oldFailedID]; !ok {
		t.Fatal("expected old failed kept until batch retain based on latest terminal")
	}
	if _, ok := byID[newDoneID]; !ok {
		t.Fatal("expected new done kept within retain window")
	}

	// 把最晚终态也推过保留窗口 → 应统一清空
	s2.mu.Lock()
	s2.updated = time.Now().Add(-retainDuration() - time.Second)
	s2.mu.Unlock()

	list = GetActiveTaskProgress()
	if len(list) != 0 {
		t.Fatalf("expected all terminal progress cleared together, got %d items", len(list))
	}
	if _, ok := store.Load(oldFailedID); ok {
		t.Fatal("expected old failed deleted in unified purge")
	}
	if _, ok := store.Load(newDoneID); ok {
		t.Fatal("expected new done deleted in unified purge")
	}
}

func TestGetActiveTaskProgressDoesNotStaleWaitingPublish(t *testing.T) {
	const sourceID = "progress-wait-publish-long"
	store.Delete(sourceID)
	tasks.Delete(sourceID)

	state := ensure(sourceID, "WaitPublish")
	state.mu.Lock()
	state.data.Status = StatusWaitingPublish
	state.updated = time.Now().Add(-staleDuration() - time.Minute)
	state.mu.Unlock()

	list := GetActiveTaskProgress()
	found := false
	for _, p := range list {
		if p.Id == sourceID {
			found = true
			if p.Status != StatusWaitingPublish {
				t.Fatalf("waiting_publish must stay, got %q", p.Status)
			}
		}
	}
	if !found {
		t.Fatal("expected long waiting_publish still listed")
	}

	snap, ok := Snapshot(sourceID)
	if !ok || snap.Status != StatusWaitingPublish {
		t.Fatalf("map status should remain waiting_publish, ok=%v status=%q", ok, snap.Status)
	}
	store.Delete(sourceID)
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

func TestMarkSourcePagesFinishedStatuses(t *testing.T) {
	const singleID = "pages-finished-single"
	const batchID = "pages-finished-batch"
	for _, id := range []string{singleID, batchID} {
		store.Delete(id)
	}

	ensure(singleID, "S")
	Update(singleID, func(p *model.CollectProgress) {
		p.Status = StatusRunning
		p.Total = 3
		p.Success = 3
	})
	MarkSourcePagesFinished(singleID, true)
	if snap, _ := Snapshot(singleID); snap.Status != StatusPageDone {
		t.Fatalf("single flushAtEnd want page_done, got %q", snap.Status)
	}

	ensure(batchID, "B")
	Update(batchID, func(p *model.CollectProgress) {
		p.Status = StatusRunning
		p.Total = 3
		p.Success = 3
	})
	MarkSourcePagesFinished(batchID, false)
	if snap, _ := Snapshot(batchID); snap.Status != StatusWaitingPublish {
		t.Fatalf("batch want waiting_publish, got %q", snap.Status)
	}

	for _, id := range []string{singleID, batchID} {
		store.Delete(id)
	}
}

// 0 页时列表状态须与生命周期一致：批量 → waiting_publish，单站 → page_done（再由 defer 收尾）。
func TestZeroPageProgressStatusMatchesLifecycle(t *testing.T) {
	const (
		batchID  = "zero-page-batch"
		singleID = "zero-page-single"
	)
	for _, id := range []string{batchID, singleID} {
		store.Delete(id)
	}

	// 模拟 handleCollect 0 页分支（与生产代码同一套状态赋值）
	markZeroPageProgress := func(id string, flushAtEnd bool) {
		ensure(id, id)
		Update(id, func(p *model.CollectProgress) {
			p.Total = 0
			p.Current = 0
			p.Success = 0
			p.Failed = 0
			if flushAtEnd {
				p.Status = StatusPageDone
			} else {
				p.Status = StatusWaitingPublish
			}
		})
	}

	markZeroPageProgress(batchID, false)
	if snap, _ := Snapshot(batchID); snap.Status != StatusWaitingPublish {
		t.Fatalf("batch zero-page want waiting_publish, got %q", snap.Status)
	}
	if !isActiveStatus(StatusWaitingPublish) {
		t.Fatal("waiting_publish must stay active so list shows 等待收尾")
	}

	markZeroPageProgress(singleID, true)
	if snap, _ := Snapshot(singleID); snap.Status != StatusPageDone {
		t.Fatalf("single zero-page want page_done, got %q", snap.Status)
	}

	for _, id := range []string{batchID, singleID} {
		store.Delete(id)
	}
}
