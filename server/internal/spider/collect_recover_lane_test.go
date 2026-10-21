package spider

import (
	"sync"
	"testing"
	"time"

	"server/internal/model"
)

func failureRecord(id uint, sourceID string) model.FailureRecord {
	fr := model.FailureRecord{OriginId: sourceID}
	fr.ID = id
	return fr
}

func isolateFailureRecoverLane(t *testing.T, sourceID string) model.FilmSource {
	t.Helper()
	old := failureRetries
	failureRetries = newFailureRetryHub()
	t.Cleanup(func() {
		failureRetries = old
		failureRecoverLanes.Delete(sourceID)
		releaseCollectSourceIDs(sourceID)
		collectLifecycle.endSource(sourceID)
	})
	return model.FilmSource{Id: sourceID, Name: "站", State: true}
}

func TestFailureRecoverOutcome_Message(t *testing.T) {
	cases := []struct {
		out  FailureRecoverOutcome
		want string
	}{
		{FailureRecoverOutcome{}, "没有待重试的记录"},
		{FailureRecoverOutcome{Already: 2}, "2 条已在重试，没有新的任务"},
		{FailureRecoverOutcome{Skipped: 3}, "3 条未开始：采集站正在采集、已停用或不存在"},
		{FailureRecoverOutcome{Queued: 4, Skipped: 1}, "已加入重试 4 条，跳过 1 条"},
	}
	for _, tc := range cases {
		if got := tc.out.Message(); got != tc.want {
			t.Fatalf("Message(%+v) = %q, want %q", tc.out, got, tc.want)
		}
	}
}

func TestFailureRecoverBlocked_LaneJoinsOwnQueue(t *testing.T) {
	sourceID := "recover-block-own"
	source := isolateFailureRecoverLane(t, sourceID)
	if len(occupyCollectSources([]model.FilmSource{source}, "own-lane")) != 1 {
		t.Fatal("预占用失败")
	}
	if !pushFailureRecover(sourceID, failureRecord(1, sourceID)) {
		t.Fatal("第一条应该负责启动")
	}
	if failureRecoverBlocked(sourceID) {
		t.Fatal("本站重试队列进行中时，后续记录应加入同一条队列")
	}
}

func TestFailureRecoverBlocked_OtherCollect(t *testing.T) {
	sourceID := "recover-block-other"
	source := isolateFailureRecoverLane(t, sourceID)
	if len(occupyCollectSources([]model.FilmSource{source}, "other-collect")) != 1 {
		t.Fatal("预占用失败")
	}
	if !failureRecoverBlocked(sourceID) {
		t.Fatal("别的采集占用站点时不能开始重试")
	}
}

func TestServeFailureRecoverLane_RetriesSelectedTogether(t *testing.T) {
	sourceID := "recover-lane-together"
	source := isolateFailureRecoverLane(t, sourceID)
	origPref := preferredSourceIDFn
	preferredSourceIDFn = func() string { return "" }
	t.Cleanup(func() { preferredSourceIDFn = origPref })

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var batches [][]uint
	occupiedDuringRun := false

	run := func(src model.FilmSource, records []model.FailureRecord, batchCtx *collectBatchContext) {
		if src.Id != sourceID || batchCtx == nil {
			t.Errorf("run ctx = %s batchNil=%v", src.Id, batchCtx == nil)
		}
		if err := collectLifecycle.beginSource(sourceID); err == nil {
			t.Error("重试执行期间站点必须停留在生命周期里")
			collectLifecycle.endSource(sourceID)
		}
		if !isOccupiedCollectSource(sourceID) {
			t.Error("重试执行期间必须占着站点")
		}
		ids := make([]uint, len(records))
		for i := range records {
			ids[i] = records[i].ID
		}
		mu.Lock()
		batches = append(batches, ids)
		occupiedDuringRun = isOccupiedCollectSource(sourceID)
		mu.Unlock()
		once.Do(func() { close(started) })
		<-release
	}

	for _, id := range []uint{11, 12, 13} {
		start := pushFailureRecover(sourceID, failureRecord(id, sourceID))
		if (id == 11) != start {
			t.Fatalf("id %d start = %v, want %v", id, start, id == 11)
		}
	}

	done := make(chan struct{})
	go func() {
		serveFailureRecoverLane(sourceID, source, run)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker 没有开始执行")
	}
	if pushFailureRecover(sourceID, failureRecord(14, sourceID)) {
		t.Fatal("执行中追加的记录不应该再启动一个 worker")
	}
	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker 没有结束")
	}

	mu.Lock()
	defer mu.Unlock()
	if !occupiedDuringRun {
		t.Fatal("合并重试时没有占用站点")
	}
	if len(batches) != 2 || len(batches[0]) != 3 || batches[0][0] != 11 || batches[0][2] != 13 || len(batches[1]) != 1 || batches[1][0] != 14 {
		t.Fatalf("batches = %v, want [[11 12 13] [14]]", batches)
	}
	if isOccupiedCollectSource(sourceID) {
		t.Fatal("重试结束后仍占着站点")
	}
	if err := collectLifecycle.beginSource(sourceID); err != nil {
		t.Fatalf("生命周期没有释放: %v", err)
	}
	collectLifecycle.endSource(sourceID)
}

func TestServeFailureRecoverLane_BusySourceAbandonsAll(t *testing.T) {
	sourceID := "recover-lane-busy"
	source := isolateFailureRecoverLane(t, sourceID)
	if len(occupyCollectSources([]model.FilmSource{source}, "other-collect")) != 1 {
		t.Fatal("预占用失败")
	}

	for _, id := range []uint{21, 22, 23} {
		if !TryBeginFailureRetry(id) {
			t.Fatalf("入队 %d 失败", id)
		}
		if id == 21 {
			if !pushFailureRecover(sourceID, failureRecord(id, sourceID)) {
				t.Fatal("第一条应该负责启动")
			}
			continue
		}
		if pushFailureRecover(sourceID, failureRecord(id, sourceID)) {
			t.Fatal("后续记录不应该各自启动")
		}
	}

	called := false
	serveFailureRecoverLane(sourceID, source, func(_ model.FilmSource, _ []model.FailureRecord, _ *collectBatchContext) {
		called = true
	})
	if called {
		t.Fatal("站点已被占用时不应该执行重试")
	}
	if len(FailureRetrySnapshot([]uint{21, 22, 23})) != 0 {
		t.Fatal("站点忙时选中的记录必须全部离开重试队列")
	}
	if !isOccupiedCollectSource(sourceID) {
		t.Fatal("放弃重试时不能放掉别人的站点占用")
	}
}

func TestServeFailureRecoverLane_BeginRejectedAbandonsAll(t *testing.T) {
	sourceID := "recover-lane-begin"
	source := isolateFailureRecoverLane(t, sourceID)
	if err := collectLifecycle.beginSource(sourceID); err != nil {
		t.Fatal(err)
	}

	for _, id := range []uint{31, 32} {
		if !TryBeginFailureRetry(id) {
			t.Fatalf("入队 %d 失败", id)
		}
		pushFailureRecover(sourceID, failureRecord(id, sourceID))
	}

	called := false
	serveFailureRecoverLane(sourceID, source, func(_ model.FilmSource, _ []model.FailureRecord, _ *collectBatchContext) {
		called = true
	})
	if called {
		t.Fatal("生命周期拒绝后不应该执行重试")
	}
	if len(FailureRetrySnapshot([]uint{31, 32})) != 0 {
		t.Fatal("启动失败时选中的记录必须全部离开重试队列")
	}
	if isOccupiedCollectSource(sourceID) {
		t.Fatal("启动失败后仍占着站点")
	}
}
