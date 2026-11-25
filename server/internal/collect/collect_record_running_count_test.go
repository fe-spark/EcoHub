package collect

import (
	"testing"
	"time"

	"server/internal/collect/progress"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"server/internal/collect/ledger"
)

func TestRunningRecordWaitsUntilFinishToStoreFilms(t *testing.T) {
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

	src := model.FilmSource{Id: "run-src", Name: "进行中站"}
	batch := newCollectBatchContext(model.CollectTriggerCron, model.CollectTagCronCollect, []model.FilmSource{src}, time.Now()).withHour(24)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}

	rec, err := ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusRunning || rec.Ledger || rec.FilmCount != 0 {
		t.Fatalf("after start status=%s ledger=%v films=%d", rec.Status, rec.Ledger, rec.FilmCount)
	}

	batch.AddAffectedMIDs(&src, 24, []int64{11, 12})
	batch.recordSourceSnapshot(model.CollectRecordSourceDetail{
		SourceID: src.Id, SourceName: src.Name, Status: progress.StatusDone, FilmsTotal: 2,
	})
	rec, err = ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusRunning || rec.Ledger || rec.FilmCount != 0 {
		t.Fatalf("while running status=%s ledger=%v films=%d", rec.Status, rec.Ledger, rec.FilmCount)
	}
	var rows int64
	if err := gdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", batch.batchID).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("running record stored %d film ids", rows)
	}

	persistCollectRecord(batch, []model.SourceNotifyResult{{
		SourceID: src.Id, SourceName: src.Name, Status: progress.StatusDone,
		PageTotal: 1, SuccessCnt: 1, FilmsTotal: 2,
	}})
	rec, err = ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusSuccess || !rec.Ledger || rec.FilmCount != 2 {
		t.Fatalf("after finish status=%s ledger=%v films=%d", rec.Status, rec.Ledger, rec.FilmCount)
	}
}
