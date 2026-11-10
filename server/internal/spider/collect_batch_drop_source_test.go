package spider

import (
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/progress"
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
	if err := gdb.AutoMigrate(&model.CollectRecord{}, &model.CollectRecordFilmItem{}); err != nil {
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
	if missed := batch.flushRecordSync(); len(missed) > 0 {
		t.Fatalf("flush missed %+v", missed)
	}

	err = PauseActiveBatchRecords(srcA.Id, func() error {
		if err := db.Mdb.Where("batch_id = ? AND source_id = ?", batch.batchID, srcA.Id).
			Delete(&model.CollectRecordFilmItem{}).Error; err != nil {
			return err
		}
		return db.Mdb.Model(&model.CollectRecord{}).Where("batch_id = ?", batch.batchID).
			Updates(map[string]any{"source_count": 1}).Error
	})
	if err != nil {
		t.Fatal(err)
	}

	if missed := batch.writeCheckpoint(map[string][]int64{srcA.Id: {9}, srcB.Id: {2}}); len(missed) > 0 {
		t.Fatalf("checkpoint missed %+v", missed)
	}
	running, err := repository.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != model.CollectRecordStatusRunning || running.SourceCount != 1 {
		t.Fatalf("running status=%s sources=%d", running.Status, running.SourceCount)
	}
	if strings.Contains(running.DetailJson, srcA.Id) || !strings.Contains(running.DetailJson, srcB.Id) {
		t.Fatalf("checkpoint detail=%s", running.DetailJson)
	}
	if _, total, err := repository.GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batch.batchID, SourceID: srcA.Id, Page: 1, PageSize: 10,
	}); err != nil || total != 0 {
		t.Fatalf("deleted source films total=%d err=%v", total, err)
	}

	persistCollectRecord(batch, []model.SourceNotifyResult{
		{SourceID: srcA.Id, SourceName: srcA.Name, Status: progress.StatusDone, PageTotal: 3, SuccessCnt: 3, FilmsTotal: 1},
		{SourceID: srcB.Id, SourceName: srcB.Name, Status: progress.StatusDone, PageTotal: 4, SuccessCnt: 2, FilmsTotal: 1},
	})

	settled, err := repository.GetCollectRecordByBatchID(batch.batchID)
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
