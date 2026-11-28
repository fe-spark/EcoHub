package collect

import (
	"testing"
	"time"

	"server/internal/collect/ledger"
	"server/internal/collect/progress"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRunningRecordCountsInMemoryAndDeduplicatesAcrossCalls(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CollectRecord{}, &model.FilmIndex{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	src := model.FilmSource{Id: "run-src", Name: "进行中站"}
	batch := newCollectBatchContext(model.CollectTriggerCron, model.CollectTagCronCollect, []model.FilmSource{src}, time.Now()).withHour(24)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}

	// 第一次传入含单次内部重复的 mid
	batch.AddAffectedMIDs(&src, 24, []int64{11, 12, 12})

	rec, err := ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusRunning || rec.FilmCount != 0 {
		t.Fatalf("while running status=%s films=%d", rec.Status, rec.FilmCount)
	}

	// 第二次传入跨页重叠的 mid=12 与新 mid=13，应去重为 3 部而非 4 部
	batch.AddAffectedMIDs(&src, 24, []int64{12, 13})
	_, settledID := persistCollectRecord(batch, []model.SourceNotifyResult{{
		SourceID: src.Id, SourceName: src.Name, Status: progress.StatusDone,
		PageTotal: 2, SuccessCnt: 2,
	}})
	if settledID != rec.ID {
		t.Fatalf("expected settledID=%d, got %d", rec.ID, settledID)
	}
	rec, err = ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusSuccess || rec.FilmCount != 3 {
		t.Fatalf("after finish status=%s films=%d want=3", rec.Status, rec.FilmCount)
	}
}

func TestNonPrimarySourceSkipsPendingSealMIDs(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CollectRecord{}, &model.FilmIndex{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	stubStreamPublishForTest(t, func(mids []int64) error { return nil }, func() string { return "primary-only" })

	src := model.FilmSource{Id: "other-src", Name: "非主站"}
	batch := newCollectBatchContext(model.CollectTriggerManual, model.CollectTagBatchCollect, []model.FilmSource{src}, time.Now()).withHour(24)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}

	batch.AddAffectedMIDs(&src, 24, []int64{1, 2})
	if seal := batch.takeAffectedMIDs(); len(seal) != 0 {
		t.Fatalf("non-primary source should not accumulate pendingSealMIDs, got %v", seal)
	}

	persistCollectRecord(batch, []model.SourceNotifyResult{{
		SourceID: src.Id, SourceName: src.Name, Status: progress.StatusDone,
		PageTotal: 1, SuccessCnt: 1,
	}})
	rec, err := ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusSuccess || rec.FilmCount != 2 {
		t.Fatalf("record should settle to success with filmCount=2, got status=%s films=%d", rec.Status, rec.FilmCount)
	}
}
