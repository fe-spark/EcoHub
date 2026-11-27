package collect

import (
	"encoding/json"
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

func TestEmitSingleFilmSnapshotPersistsDetail(t *testing.T) {
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

	src := model.FilmSource{Id: "jy", Name: "金鹰1(JY)"}
	emitSingle := func(filmID int64, changed []int64, errMsg string) {
		t.Helper()
		batch := newCollectBatchContext(
			model.CollectTriggerSingleUpdate,
			model.CollectTagSingleUpdate,
			[]model.FilmSource{src},
			time.Now().Add(-time.Second),
		).withReplayFilmIDs([]int64{filmID})
		defer batch.close()
		if len(changed) > 0 {
			batch.AddAffectedMIDs(&src, 1, changed)
		}
		if errMsg != "" {
			batch.RecordSourceError(src.Id, errMsg)
		}
		batch.recordSourceSnapshot(singleFilmSourceDetail(src, len(changed), errMsg))
		batch.emitSummary()
	}

	emitSingle(42, nil, "")
	emitSingle(43, []int64{43}, "")
	emitSingle(44, nil, "get movie detail failed")

	list, total, err := ledger.GetCollectRecordList(model.CollectRecordQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("expected 3 records, got total=%d len=%d", total, len(list))
	}
	recordIDs := make([]uint, 0, len(list))
	for _, rec := range list {
		recordIDs = append(recordIDs, rec.ID)
	}
	midsByRecord, err := ledger.ListCollectRecordMids(recordIDs)
	if err != nil {
		t.Fatal(err)
	}
	byFilm := map[int64]model.CollectRecord{}
	for _, rec := range list {
		mids := midsByRecord[rec.ID]
		if len(mids) != 1 {
			t.Fatalf("batch %s mids=%v", rec.BatchID, mids)
		}
		byFilm[mids[0]] = rec
	}

	unchanged := byFilm[42]
	if unchanged.Status != model.CollectRecordStatusSuccess {
		t.Fatalf("unchanged status %s", unchanged.Status)
	}
	if unchanged.Hour != 0 || unchanged.FilmCount != 0 {
		t.Fatalf("unchanged hour=%d films=%d", unchanged.Hour, unchanged.FilmCount)
	}
	if unchanged.PageTotal != 1 || unchanged.PageSuccess != 1 || unchanged.PageFailed != 0 {
		t.Fatalf("unchanged pages total=%d succ=%d fail=%d", unchanged.PageTotal, unchanged.PageSuccess, unchanged.PageFailed)
	}
	assertSourceDetail(t, unchanged.DetailJson, progress.StatusDone, 1, 1, 0, 0, "")

	changed := byFilm[43]
	if changed.Status != model.CollectRecordStatusSuccess || changed.FilmCount != 1 {
		t.Fatalf("changed status=%s films=%d", changed.Status, changed.FilmCount)
	}
	if changed.PageTotal != 1 || changed.PageSuccess != 1 || changed.PageFailed != 0 {
		t.Fatalf("changed pages total=%d succ=%d fail=%d", changed.PageTotal, changed.PageSuccess, changed.PageFailed)
	}
	assertSourceDetail(t, changed.DetailJson, progress.StatusDone, 1, 1, 1, 0, "")

	failed := byFilm[44]
	if failed.Status != model.CollectRecordStatusFailed || failed.FilmCount != 0 {
		t.Fatalf("failed status=%s films=%d", failed.Status, failed.FilmCount)
	}
	if failed.PageTotal != 1 || failed.PageSuccess != 0 || failed.PageFailed != 1 {
		t.Fatalf("failed pages total=%d succ=%d fail=%d", failed.PageTotal, failed.PageSuccess, failed.PageFailed)
	}
	if !failed.Ledger {
		t.Fatal("failed single-film update should keep the replay id")
	}
	assertSourceDetail(t, failed.DetailJson, progress.StatusFailed, 1, 0, 0, 1, "get movie detail failed")

	var filmRows []model.CollectRecordFilmItem
	if err := gdb.Order("mid ASC").Find(&filmRows).Error; err != nil {
		t.Fatal(err)
	}
	if len(filmRows) != 3 || filmRows[0].Mid != 42 || filmRows[1].Mid != 43 || filmRows[2].Mid != 44 {
		t.Fatalf("each single-film batch should keep its replay id, got %+v", filmRows)
	}
}

func TestReplayFilmIDsAreNotCopiedToIdleSources(t *testing.T) {
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

	srcA := model.FilmSource{Id: "a", Name: "A"}
	srcB := model.FilmSource{Id: "b", Name: "B"}
	srcC := model.FilmSource{Id: "c", Name: "C"}
	batch := newCollectBatchContext(
		model.CollectTriggerSingleUpdate,
		model.CollectTagSingleUpdate,
		[]model.FilmSource{srcA, srcB, srcC},
		time.Now().Add(-time.Second),
	).withReplayFilmIDs([]int64{43})
	defer batch.close()
	batch.AddAffectedMIDs(&srcA, 1, []int64{43})
	batch.recordSourceSnapshot(model.CollectRecordSourceDetail{
		SourceID: srcA.Id, SourceName: srcA.Name, Status: progress.StatusDone,
		PageTotal: 1, PageCurrent: 1, SuccessCnt: 1, FilmsTotal: 1,
	})
	batch.recordSourceSnapshot(model.CollectRecordSourceDetail{
		SourceID: srcB.Id, SourceName: srcB.Name, Status: progress.StatusDone,
		PageTotal: 1, PageCurrent: 1, SuccessCnt: 1, FilmsTotal: 0,
	})
	batch.recordSourceSnapshot(model.CollectRecordSourceDetail{
		SourceID: srcC.Id, SourceName: srcC.Name, Status: progress.StatusFailed,
		PageTotal: 1, PageCurrent: 0, FailedCnt: 1, FilmsTotal: 0, Error: "fail",
	})
	batch.emitSummary()

	var rows []model.CollectRecordFilmItem
	if err := gdb.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SourceID != srcA.Id || rows[0].Mid != 43 {
		t.Fatalf("idle and failed sources must not receive the replay film, got %+v", rows)
	}
}

func assertSourceDetail(t *testing.T, raw, status string, pageTotal, pageCurrent, films, failedCnt int, errMsg string) {
	t.Helper()
	var details []model.CollectRecordSourceDetail
	if err := json.Unmarshal([]byte(raw), &details); err != nil {
		t.Fatalf("detail json: %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("expected 1 source, got %d", len(details))
	}
	d := details[0]
	if d.SourceName != "金鹰1(JY)" || d.Status != status {
		t.Fatalf("source %s status %s", d.SourceName, d.Status)
	}
	if d.PageTotal != pageTotal || d.PageCurrent != pageCurrent || d.FilmsTotal != films || d.FailedCnt != failedCnt {
		t.Fatalf("detail pages=%d/%d films=%d failed=%d", d.PageCurrent, d.PageTotal, d.FilmsTotal, d.FailedCnt)
	}
	if d.Error != errMsg {
		t.Fatalf("detail error %q", d.Error)
	}
}
