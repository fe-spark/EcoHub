package spider

import (
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/spider/progress"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 旧批次已过派发前的占用复核、还没登记任务时，用户单站停止并马上重试：
// 旧批次不能顶替重试登记任务，也不能用重试的代次改进度、放掉重试的占用。
func TestSingleStopThenRetry_OldBatchDoesNotRegisterOverRetry(t *testing.T) {
	prev := db.Mdb
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.CategoryMapping{}, &model.Category{}, &model.SourceCategory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	src := model.FilmSource{Id: "register-after-retry", Name: "登记竞态", Uri: "http://127.0.0.1:1/api", State: true}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: src.Id, SourceTypeId: 1, CategoryId: 9}).Error; err != nil {
		t.Fatalf("seed mapping: %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	beforeRegisterCollectRunHook = func(id string) {
		if id != src.Id {
			return
		}
		close(entered)
		<-release
	}
	t.Cleanup(func() { beforeRegisterCollectRunHook = nil })

	claim, err := claimCollectSources([]model.FilmSource{src}, "Batch-Collect")
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("batch claim failed: %v", err)
	}
	batch := newCollectBatchContext(model.CollectTriggerManual, "Batch-Collect", claim.Sources, time.Now()).withClaim(claim)
	defer batch.close()

	ver := stopAllVersion.Load()
	done := make(chan error, 1)
	go func() { done <- handleCollectWithStopVersion(src.Id, 24, &ver, batch) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("old batch never reached task registration")
	}

	StopCollectSource(src.Id, "测试单站停止")
	retried, err := occupyAndMarkCollectSources([]model.FilmSource{src}, "Single-Collect")
	if err != nil || len(retried) != 1 {
		t.Fatalf("retry occupy failed: %v", err)
	}
	defer releaseCollectSourceIDs(src.Id)
	before, _ := progress.Snapshot(src.Id)

	close(release)
	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("old batch did not return")
	}
	if runErr == nil {
		t.Fatal("old batch must refuse to start once the retry owns the source")
	}

	registered := false
	progress.RangeTasks(func(id, _ string) bool {
		if id == src.Id {
			registered = true
		}
		return true
	})
	if registered {
		progress.RequestStop(src.Id)
		t.Fatal("old batch must not register a task over the retry")
	}
	after, ok := progress.Snapshot(src.Id)
	if !ok || after.Status != progress.StatusStarting || after.QueueId != before.QueueId {
		t.Fatalf("retry progress must stay untouched: status=%q queue=%q want starting/%q", after.Status, after.QueueId, before.QueueId)
	}
	if !isOccupiedCollectSource(src.Id) {
		t.Fatal("old batch must not release the retry occupy")
	}
	progress.MarkStopped(src.Id)
}
