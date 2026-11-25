package collect

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/collect/progress"
	"server/internal/infra/db"
	"server/internal/model"
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

func TestStopAndAlreadyRunningDoNotNotifySourceFailed(t *testing.T) {
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

	src := model.FilmSource{Id: "notify-skip-stop", Name: "不告警", Uri: "http://127.0.0.1:1/api", State: true}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: src.Id, SourceTypeId: 1, CategoryId: 9}).Error; err != nil {
		t.Fatalf("seed mapping: %v", err)
	}

	prevNotify := getNotifyDispatcher()
	var notified atomic.Int32
	RegisterNotifyDispatcher(NotifyDispatcher{
		OnSourceFailed: func(string, string, string) { notified.Add(1) },
	})
	t.Cleanup(func() {
		RegisterNotifyDispatcher(prevNotify)
		beforeRegisterCollectRunHook = nil
		if progress.UnregisterTask(src.Id, "holder") {
			progress.RequestStop(src.Id)
		}
	})

	t.Run("stop during register", func(t *testing.T) {
		notified.Store(0)
		beforeRegisterCollectRunHook = func(id string) {
			if id == src.Id {
				stopAllVersion.Add(1)
			}
		}
		ver := stopAllVersion.Load()
		err := handleCollectWithStopVersion(src.Id, 24, &ver, nil)
		if !errors.Is(err, progress.ErrDispatchStopped) {
			t.Fatalf("got %v", err)
		}
		if notified.Load() != 0 {
			t.Fatalf("stop must not notify, count=%d", notified.Load())
		}
	})

	t.Run("already running", func(t *testing.T) {
		notified.Store(0)
		beforeRegisterCollectRunHook = nil
		if _, err := progress.TryRegisterTask(src.Id, "holder", nil); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			progress.UnregisterTask(src.Id, "holder")
			progress.MarkStopped(src.Id)
		})
		err := handleCollectWithStopVersion(src.Id, 24, nil, nil)
		if !errors.Is(err, progress.ErrTaskExists) {
			t.Fatalf("got %v", err)
		}
		if notified.Load() != 0 {
			t.Fatalf("skip must not notify, count=%d", notified.Load())
		}
	})
}
