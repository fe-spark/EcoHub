package collect

import (
	"context"
	"errors"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/collect/progress"
)

// stubLateProbe 让探页卡住，直到测试放行；放行后返回给定结果（无视 ctx，模拟请求已在路上、晚一步才回来）。
func stubLateProbe(t *testing.T, pages int, err error) (entered <-chan struct{}, release chan<- struct{}) {
	t.Helper()
	in := make(chan struct{})
	rel := make(chan struct{})
	prev := fetchProbePageCount
	fetchProbePageCount = func(context.Context, *model.FilmSource, int) (int, error) {
		close(in)
		<-rel
		return pages, err
	}
	t.Cleanup(func() { fetchProbePageCount = prev })
	return in, rel
}

// 一键终止后马上重试同站，旧批次的探页这时才失败返回：不能把重试标成失败，也不能放掉重试的占用。
func TestStopAllThenRetry_LateProbeFailureDoesNotFailRetry(t *testing.T) {
	runLateProbeAfterStopAllRetry(t, "late-probe-fail", 0, errors.New("探页超时"))
}

// 同上，旧批次的探页晚一步成功返回：不能把旧结果写成重试的总页。
func TestStopAllThenRetry_LateProbeSuccessDoesNotSetRetryTotal(t *testing.T) {
	runLateProbeAfterStopAllRetry(t, "late-probe-ok", 7, nil)
}

func runLateProbeAfterStopAllRetry(t *testing.T, id string, pages int, probeErr error) {
	src := model.FilmSource{Id: id, Name: id}
	entered, release := stubLateProbe(t, pages, probeErr)

	claim, err := claimCollectSources([]model.FilmSource{src}, "Batch-Collect")
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("batch claim failed: %v", err)
	}
	batch := newCollectBatchContext(model.CollectTriggerManual, "Batch-Collect", claim.Sources, time.Now()).withClaim(claim)
	defer batch.close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		runSourcesGroupWithLimit(claim.Sources, 24, "Batch-Collect", 1, stopAllVersion.Load(), batch)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("old batch never started probing")
	}

	StopAllCollect("测试一键终止")
	retried, err := occupyAndMarkCollectSources([]model.FilmSource{src}, "Single-Collect")
	if err != nil || len(retried) != 1 {
		t.Fatalf("retry occupy failed: %v", err)
	}
	defer releaseCollectSourceIDs(src.Id)
	before, _ := progress.Snapshot(src.Id)

	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("old batch did not finish after probe returned")
	}

	after, ok := progress.Snapshot(src.Id)
	if !ok || after.Status != progress.StatusStarting || after.QueueId != before.QueueId {
		t.Fatalf("late probe must not touch the retry: status=%q queue=%q want starting/%q", after.Status, after.QueueId, before.QueueId)
	}
	if got := progress.PageTotal(src.Id); got != -1 {
		t.Fatalf("retry page total = %d, want unprobed (-1)", got)
	}
	if !isOccupiedCollectSource(src.Id) {
		t.Fatal("old batch must not release the retry occupy")
	}
	progress.MarkStopped(src.Id)
}

// PrepareBatchCollectStart 与批次真正建起来之间隔着一个协程。期间一键终止 + 重试时，
// 批次必须用占用那一刻的令牌和代次，不能把重试认成自己的。
func TestBatchUsesClaimTokens_NotRetryTakenAfterStopAll(t *testing.T) {
	src := model.FilmSource{Id: "claim-before-batch", Name: "claim"}
	claim, err := claimCollectSources([]model.FilmSource{src}, "批量采集")
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("claim failed: %v", err)
	}

	StopAllCollect("测试一键终止")
	retried, err := occupyAndMarkCollectSources([]model.FilmSource{src}, "Single-Collect")
	if err != nil || len(retried) != 1 {
		t.Fatalf("retry occupy failed: %v", err)
	}
	defer releaseCollectSourceIDs(src.Id)
	before, _ := progress.Snapshot(src.Id)

	// 协程这时才建批次。
	batch := newCollectBatchContext(model.CollectTriggerManual, "Batch-Collect", claim.Sources, time.Now()).withClaim(claim)
	defer batch.close()

	if batch.ownsOccupy(src.Id) {
		t.Fatal("old batch must not own the retry occupy")
	}
	if epoch, _ := batch.progressEpoch(src.Id); epoch == progress.Epoch(src.Id) {
		t.Fatal("old batch must keep the epoch from claim time, not the retry's")
	}
	abandonQueuedCollectSource(src, batch)
	after, _ := progress.Snapshot(src.Id)
	if after.Status != progress.StatusStarting || after.QueueId != before.QueueId {
		t.Fatalf("old batch must not stop the retry: %+v", after)
	}
	if !isOccupiedCollectSource(src.Id) {
		t.Fatal("old batch must not release the retry occupy")
	}
	progress.MarkStopped(src.Id)
}

func TestStopAllCancelsInflightProbeImmediately(t *testing.T) {
	src := model.FilmSource{Id: "probe-cancel-fast", Name: "fast"}
	entered := make(chan struct{})
	canceled := make(chan struct{})
	prev := fetchProbePageCount
	fetchProbePageCount = func(ctx context.Context, _ *model.FilmSource, _ int) (int, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		return 0, ctx.Err()
	}
	t.Cleanup(func() { fetchProbePageCount = prev })

	claim, err := claimCollectSources([]model.FilmSource{src}, "Batch-Collect")
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("claim failed: %v", err)
	}
	batch := newCollectBatchContext(model.CollectTriggerManual, "Batch-Collect", claim.Sources, time.Now()).withClaim(claim)
	defer batch.close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		runSourcesGroupWithLimit(claim.Sources, 24, "Batch-Collect", 1, stopAllVersion.Load(), batch)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not start")
	}

	StopAllCollect("测试立即取消探页")
	select {
	case <-canceled:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("StopAllCollect should cancel inflight probe ctx without polling delay")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("batch dispatch did not finish after probe cancel")
	}
}

// 旧批次失权退出 handleCollectWithStopVersion 时，绝不能抄走新重试的进度或写成失败。
func TestDisplacedBatchDoesNotCopyRetrySnapshotOnEarlyExit(t *testing.T) {
	resetSearchReseal()
	resetStreamPublishBufferForTest()
	prevHook := beforeRegisterCollectRunHook
	t.Cleanup(func() {
		beforeRegisterCollectRunHook = prevHook
		resetStreamPublishBufferForTest()
		resetSearchReseal()
	})

	gdb := setupAuditSpiderDB(t)
	src := model.FilmSource{Id: "displaced-s1", Name: "被重开站", Uri: "http://127.0.0.1/disp", State: true}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Category{Id: 10, Name: "电影"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: src.Id, SourceTypeId: 1, CategoryId: 10}).Error; err != nil {
		t.Fatal(err)
	}

	claim1, err := claimCollectSources([]model.FilmSource{src}, model.CollectTagBatchCollect)
	if err != nil {
		t.Fatal(err)
	}
	oldBatch := newCollectBatchContext(model.CollectTriggerManual, model.CollectTagBatchCollect, claim1.Sources, time.Now()).withClaim(claim1)
	defer oldBatch.close()

	beforeRegisterCollectRunHook = func(id string) {
		if id != src.Id {
			return
		}
		// 模拟旧批次停站并立刻由新重试接手且新重试已写高页数。
		StopCollectSource(src.Id, "停旧开新")
		retried, rErr := occupyAndMarkCollectSources([]model.FilmSource{src}, model.CollectTagSingleCollect)
		if rErr != nil || len(retried) != 1 {
			t.Fatalf("retry occupy failed: %v", rErr)
		}
		progress.SetPageTotal(src.Id, 99)
		progress.Update(src.Id, func(p *model.CollectProgress) {
			p.Status = progress.StatusRunning
			p.Success = 12
		})
	}

	ver := stopAllVersion.Load()
	err = handleCollectWithStopVersion(src.Id, 24, &ver, oldBatch)
	releaseCollectSourceIDs(src.Id)
	progress.MarkStopped(src.Id)
	if err == nil {
		t.Fatal("失权旧批次应返回中止错误")
	}

	oldBatch.mu.Lock()
	snap := oldBatch.sourceSnapshots[src.Id]
	oldBatch.mu.Unlock()
	if snap.Status != progress.StatusStopped || snap.PageTotal != 0 || snap.SuccessCnt != 0 || snap.Error != "" {
		t.Fatalf("失权旧批次应保留纯停站状态且不得混入新重试页数，got=%+v", snap)
	}
}
