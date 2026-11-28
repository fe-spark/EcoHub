package collect

import (
	"strings"
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

func TestDeletedSourceStaysOutOfRunningBatch(t *testing.T) {
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

	srcA := model.FilmSource{Id: "src-a", Name: "A"}
	srcB := model.FilmSource{Id: "src-b", Name: "B"}
	batch := newCollectBatchContext(model.CollectTriggerManual, "批量测试", []model.FilmSource{srcA, srcB}, time.Now()).withHour(24)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}
	batch.AddAffectedMIDs(&srcA, 24, []int64{1})
	batch.AddAffectedMIDs(&srcB, 24, []int64{2})
	batch.recordSourceSnapshot(model.CollectRecordSourceDetail{
		SourceID: srcA.Id, SourceName: srcA.Name, Status: progress.StatusDone,
		PageTotal: 3, SuccessCnt: 3, FilmsTotal: 1,
	})
	batch.recordSourceSnapshot(model.CollectRecordSourceDetail{
		SourceID: srcB.Id, SourceName: srcB.Name, Status: progress.StatusRunning,
		PageTotal: 4, SuccessCnt: 2, FilmsTotal: 1,
	})

	err = PauseActiveBatchRecords(srcA.Id, func() error {
		return db.Mdb.Model(&model.CollectRecord{}).Where("id = ?", batch.recordID).
			Updates(map[string]any{"source_count": 1}).Error
	})
	if err != nil {
		t.Fatal(err)
	}

	// 已摘除的站点再追加也不会计入部数。
	batch.AddAffectedMIDs(&srcA, 24, []int64{9})

	running, err := ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != model.CollectRecordStatusRunning || running.SourceCount != 1 {
		t.Fatalf("running status=%s sources=%d", running.Status, running.SourceCount)
	}

	persistCollectRecord(batch, []model.SourceNotifyResult{
		{SourceID: srcA.Id, SourceName: srcA.Name, Status: progress.StatusDone, PageTotal: 3, SuccessCnt: 3},
		{SourceID: srcB.Id, SourceName: srcB.Name, Status: progress.StatusDone, PageTotal: 4, SuccessCnt: 2},
	})

	settled, err := ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != model.CollectRecordStatusSuccess || settled.SourceCount != 1 {
		t.Fatalf("settled status=%s sources=%d", settled.Status, settled.SourceCount)
	}
	if strings.Contains(settled.DetailJson, srcA.Id) || settled.PageTotal != 4 || settled.PageSuccess != 2 || settled.FilmCount != 1 {
		t.Fatalf("settled pages=%d/%d films=%d detail=%s", settled.PageTotal, settled.PageSuccess, settled.FilmCount, settled.DetailJson)
	}
}
