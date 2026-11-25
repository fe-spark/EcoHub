package collect

import (
	"context"
	"testing"
	"time"

	"server/internal/collect/fetcher"
	"server/internal/collect/progress"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSettleSourceStatus(t *testing.T) {
	status, errMsg := settleSourceStatus(progress.StatusStopping, "context canceled", 0)
	if status != progress.StatusStopped || errMsg != "" {
		t.Fatalf("stopping should settle stopped without error, got %s %q", status, errMsg)
	}

	status, errMsg = settleSourceStatus(progress.StatusStopped, "用户停止", 0)
	if status != progress.StatusStopped || errMsg != "" {
		t.Fatalf("stopped should clear error, got %s %q", status, errMsg)
	}

	status, errMsg = settleSourceStatus(progress.StatusDone, "连接超时", 0)
	if status != progress.StatusFailed || errMsg != "连接超时" {
		t.Fatalf("error should settle failed, got %s %q", status, errMsg)
	}

	status, errMsg = settleSourceStatus(progress.StatusDone, "连接超时", 3)
	if status != model.CollectRecordStatusPartial || errMsg != "连接超时" {
		t.Fatalf("page errors with success should settle partial, got %s %q", status, errMsg)
	}

	status, errMsg = settleSourceStatus(progress.StatusDone, "", 0)
	if status != progress.StatusDone || errMsg != "" {
		t.Fatalf("done without error should stay done, got %s %q", status, errMsg)
	}

	msg := formatCategorySkip(2)
	status, errMsg = settleSourceStatus(progress.StatusDone, msg, 4)
	if status != model.CollectRecordStatusPartial || errMsg != "分类未映射，跳过 2 部" {
		t.Fatalf("category skip should settle partial, got %s %q", status, errMsg)
	}
}

func TestBatchCollectStatus(t *testing.T) {
	// 1. 全部成功：所有站点完成，无失败页
	if s := model.CollectBatchStatus(1, 0, 0, 0, 0, 1, 22, 0); s != model.CollectRecordStatusSuccess {
		t.Fatalf("expected success, got %s", s)
	}
	if s := model.CollectBatchStatus(4, 0, 0, 0, 0, 4, 100, 0); s != model.CollectRecordStatusSuccess {
		t.Fatalf("expected success, got %s", s)
	}

	// 2. 单站部分成功（22页成21页，失败1页）：必须判定为 partial，不能是 failed
	if s := model.CollectBatchStatus(0, 1, 0, 0, 0, 1, 21, 1); s != model.CollectRecordStatusPartial {
		t.Fatalf("expected partial for single source with partial success, got %s", s)
	}

	// 3. 多站部分成功：有站点全成，有站点失败
	if s := model.CollectBatchStatus(3, 0, 1, 0, 0, 4, 86, 1); s != model.CollectRecordStatusPartial {
		t.Fatalf("expected partial for multi source with partial failure, got %s", s)
	}

	// 4. 彻底失败：零页成功，零站成功
	if s := model.CollectBatchStatus(0, 0, 1, 0, 0, 1, 0, 1); s != model.CollectRecordStatusFailed {
		t.Fatalf("expected failed for zero success, got %s", s)
	}
	if s := model.CollectBatchStatus(0, 0, 2, 0, 0, 2, 0, 5); s != model.CollectRecordStatusFailed {
		t.Fatalf("expected failed for zero success multi-source, got %s", s)
	}

	// 5. 全部站点被用户停止：无完成站点，无失败站点
	if s := model.CollectBatchStatus(0, 0, 0, 2, 0, 2, 10, 0); s != model.CollectRecordStatusStopped {
		t.Fatalf("expected stopped, got %s", s)
	}

	// 6. 部分停止：部分站点已完成，部分被停止，无失败站点
	if s := model.CollectBatchStatus(1, 0, 0, 1, 0, 2, 10, 0); s != model.CollectRecordStatusPartialStopped {
		t.Fatalf("expected partial_stopped, got %s", s)
	}

	// 7. 部分停止但停止前已有失败页：仍是 partial_stopped，不能被失败页翻成 partial
	if s := model.CollectBatchStatus(1, 0, 0, 1, 0, 2, 10, 2); s != model.CollectRecordStatusPartialStopped {
		t.Fatalf("expected partial_stopped even with failed pages before stop, got %s", s)
	}

	// 8. 停止与失败站点并存：有成功产出，定为 partial
	if s := model.CollectBatchStatus(1, 0, 1, 1, 0, 3, 10, 2); s != model.CollectRecordStatusPartial {
		t.Fatalf("expected partial when stopped alongside a failed source, got %s", s)
	}

	if s := model.CollectBatchStatus(1, 0, 0, 0, 1, 2, 4, 0); s != model.CollectRecordStatusInterrupted {
		t.Fatalf("expected interrupted, got %s", s)
	}
}

func setupStopTestDB(t *testing.T, source model.FilmSource) *gorm.DB {
	t.Helper()
	prev := db.Mdb
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.CategoryMapping{}, &model.Category{}, &model.SourceCategory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := gdb.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: source.Id, SourceTypeId: 1, CategoryId: 9}).Error; err != nil {
		t.Fatalf("seed mapping: %v", err)
	}
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
	return gdb
}

func TestStoppedSourceSkipsPublishMuBeforeSealing(t *testing.T) {
	resetSearchReseal()
	resetStreamPublishBufferForTest()
	origCollectPages := collectPagesFn
	origPub := publishStreamWindowFn
	origPref := preferredSourceIDFn
	origSeal := sealSearchIndex
	publishStreamWindowFn = func([]int64) error { return nil }
	preferredSourceIDFn = func() string { return "" }
	sealSearchIndex = func([]int64) bool { return true }
	t.Cleanup(func() {
		collectPagesFn = origCollectPages
		publishStreamWindowFn = origPub
		preferredSourceIDFn = origPref
		sealSearchIndex = origSeal
		resetStreamPublishBufferForTest()
		resetSearchReseal()
	})

	src := model.FilmSource{Id: "stop-skip-publish-mu", Name: "跳过发布锁", Uri: "http://127.0.0.1:1/api", State: true}
	setupStopTestDB(t, src)

	beginCollectQueue()
	defer endCollectQueue()

	collectPagesFn = func(ctx context.Context, _ int, _ []int, source *model.FilmSource, hours int, batch fetcher.Batch) (bool, int, int, []int, error) {
		batch.AddAffectedMIDs(source, hours, []int64{42})
		StopCollectSource(source.Id, "测试停止跳过publishMu")
		<-ctx.Done()
		return true, 1, 0, nil, ctx.Err()
	}

	claim, err := claimCollectSources([]model.FilmSource{src}, "Batch-Collect")
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("claim failed: %v", err)
	}
	progress.SetPageTotal(src.Id, 2)
	batch := newCollectBatchContext(model.CollectTriggerManual, "Batch-Collect", claim.Sources, time.Now()).withClaim(claim)
	defer batch.close()

	// 先占住全局 publishMu，验证已停站点不会被 sealFinishedSource 卡在「停止中」。
	publishMu.Lock()
	locked := true
	defer func() {
		if locked {
			publishMu.Unlock()
		}
	}()

	ver := stopAllVersion.Load()
	done := make(chan error, 1)
	go func() {
		done <- handleCollectWithStopVersion(src.Id, 24, &ver, batch)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stopped source blocked on publishMu instead of entering sealing")
	}
	if snap, ok := progress.Snapshot(src.Id); !ok || snap.Status != progress.StatusSealing {
		t.Fatalf("stopped source with writes should enter sealing, got ok=%v status=%q", ok, snap.Status)
	}

	publishMu.Unlock()
	locked = false
	if err := batch.flushAndFinalize(); err != nil {
		t.Fatalf("flushAndFinalize failed: %v", err)
	}
	searchReseal.mu.Lock()
	_, hasMid := searchReseal.pending[42]
	searchReseal.mu.Unlock()
	if !hasMid {
		t.Fatal("expected affected MID 42 enqueued into searchReseal by flushAndFinalize")
	}
}

func TestStoppedDuringInternalProbe_ZeroWritesDoesNotEnterSealing(t *testing.T) {
	resetSearchReseal()
	origFetchProbe := fetchProbePageCount
	t.Cleanup(func() {
		fetchProbePageCount = origFetchProbe
		resetSearchReseal()
	})

	src := model.FilmSource{Id: "stop-internal-probe", Name: "内探停", Uri: "http://127.0.0.1:1/api", State: true}
	setupStopTestDB(t, src)

	entered := make(chan struct{})
	fetchProbePageCount = func(ctx context.Context, _ *model.FilmSource, _ int) (int, error) {
		close(entered)
		<-ctx.Done()
		return 0, ctx.Err()
	}

	claim, err := claimCollectSources([]model.FilmSource{src}, "Single-Collect")
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("claim failed: %v", err)
	}
	batch := newCollectBatchContext(model.CollectTriggerManual, "Single-Collect", claim.Sources, time.Now()).withClaim(claim)
	defer batch.close()

	ver := stopAllVersion.Load()
	done := make(chan error, 1)
	go func() {
		done <- handleCollectWithStopVersion(src.Id, 24, &ver, batch)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not start")
	}

	StopCollectSource(src.Id, "探页阶段停止")
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleCollectWithStopVersion did not return after probe stop")
	}
	if snap, ok := progress.Snapshot(src.Id); !ok || snap.Status != progress.StatusStopped {
		t.Fatalf("0-write probe stop must settle stopped instead of sealing, got ok=%v status=%q", ok, snap.Status)
	}
}
