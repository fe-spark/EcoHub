package collect

import (
	"encoding/json"
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

func TestEmitSingleFilmSnapshotPersistsDetail(t *testing.T) {
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
	byFilm := map[int64]model.CollectRecord{}
	for _, rec := range list {
		var mids []int64
		if err := json.Unmarshal([]byte(rec.TargetMids), &mids); err != nil || len(mids) != 1 {
			t.Fatalf("batch %s target_mids=%q err=%v", rec.BatchID, rec.TargetMids, err)
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
	assertSourceDetail(t, unchanged.DetailJson, progress.StatusDone, 1, 1, 0, 0, "")

	changed := byFilm[43]
	if changed.Status != model.CollectRecordStatusSuccess || changed.FilmCount != 1 {
		t.Fatalf("changed status=%s films=%d", changed.Status, changed.FilmCount)
	}
	assertSourceDetail(t, changed.DetailJson, progress.StatusDone, 1, 1, 1, 0, "")

	failed := byFilm[44]
	if failed.Status != model.CollectRecordStatusFailed || failed.FilmCount != 0 {
		t.Fatalf("failed status=%s films=%d", failed.Status, failed.FilmCount)
	}
	if failed.TargetMids != "[44]" {
		t.Fatalf("failed single-film update should keep target_mids=[44], got %q", failed.TargetMids)
	}
	assertSourceDetail(t, failed.DetailJson, progress.StatusFailed, 1, 0, 0, 1, "get movie detail failed")
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
