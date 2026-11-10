package spider

import (
	"context"
	"errors"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/spider/progress"
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
