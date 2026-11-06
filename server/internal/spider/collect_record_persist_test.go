package spider

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/progress"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPersistCollectRecord(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CollectRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	src1 := model.FilmSource{Id: "persist-s1", Name: "源1"}
	src2 := model.FilmSource{Id: "persist-s2", Name: "源2"}
	startedAt := time.Now().Add(-2 * time.Second)

	batch := newCollectBatchContext(model.CollectTriggerManual, "批量测试", []model.FilmSource{src1, src2}, startedAt).withHour(24)
	defer batch.close()

	// 等待初始异步写入完成
	time.Sleep(50 * time.Millisecond)

	results := []model.SourceNotifyResult{
		{
			SourceID:    src1.Id,
			SourceName:  src1.Name,
			Status:      progress.StatusDone,
			PageTotal:   10,
			PageCurrent: 10,
			SuccessCnt:  10,
			FailedCnt:   0,
			FilmsTotal:  25,
		},
		{
			SourceID:    src2.Id,
			SourceName:  src2.Name,
			Status:      progress.StatusFailed,
			PageTotal:   5,
			PageCurrent: 2,
			SuccessCnt:  1,
			FailedCnt:   1,
			FailedPages: []int{3, 4},
			FilmsTotal:  3,
			Error:       "连接超时",
		},
	}
	persistCollectRecord(batch, results)

	// 等待异步完成写入
	time.Sleep(100 * time.Millisecond)

	list, total, err := repository.GetCollectRecordList(model.CollectRecordQuery{
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("query record: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("expected 1 record, got %d", total)
	}
	rec := list[0]
	if rec.Status != model.CollectRecordStatusPartial {
		t.Fatalf("expected status %s, got %s", model.CollectRecordStatusPartial, rec.Status)
	}
	if rec.Hour != 24 {
		t.Fatalf("expected hour 24, got %d", rec.Hour)
	}
	if rec.FilmCount != 28 {
		t.Fatalf("expected film count 28, got %d", rec.FilmCount)
	}
	if rec.PageTotal != 15 || rec.PageSuccess != 11 || rec.PageFailed != 1 {
		t.Fatalf("unexpected pages: total=%d, succ=%d, fail=%d", rec.PageTotal, rec.PageSuccess, rec.PageFailed)
	}
	if !strings.Contains(rec.DetailJson, `"failedPages":[3,4]`) {
		t.Fatalf("detail dropped failed pages: %s", rec.DetailJson)
	}
}

func TestEmitSingleFilmSnapshotPersistsDetail(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
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
			batch.AddNotifyMIDs(src.Id, changed)
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

	list, total, err := repository.GetCollectRecordList(model.CollectRecordQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("expected 3 records, got total=%d len=%d", total, len(list))
	}
	byFilm := map[string]model.CollectRecord{}
	for _, rec := range list {
		byFilm[rec.FilmIDs] = rec
	}

	unchanged := byFilm["42"]
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

	changed := byFilm["43"]
	if changed.Status != model.CollectRecordStatusSuccess || changed.FilmCount != 1 {
		t.Fatalf("changed status=%s films=%d", changed.Status, changed.FilmCount)
	}
	if changed.PageTotal != 1 || changed.PageSuccess != 1 || changed.PageFailed != 0 {
		t.Fatalf("changed pages total=%d succ=%d fail=%d", changed.PageTotal, changed.PageSuccess, changed.PageFailed)
	}
	assertSourceDetail(t, changed.DetailJson, progress.StatusDone, 1, 1, 1, 0, "")

	failed := byFilm["44"]
	if failed.Status != model.CollectRecordStatusFailed || failed.FilmCount != 0 {
		t.Fatalf("failed status=%s films=%d", failed.Status, failed.FilmCount)
	}
	if failed.PageTotal != 1 || failed.PageSuccess != 0 || failed.PageFailed != 1 {
		t.Fatalf("failed pages total=%d succ=%d fail=%d", failed.PageTotal, failed.PageSuccess, failed.PageFailed)
	}
	if failed.FilmIDs != "44" {
		t.Fatalf("film ids wiped: %q", failed.FilmIDs)
	}
	assertSourceDetail(t, failed.DetailJson, progress.StatusFailed, 1, 0, 0, 1, "get movie detail failed")

	var filmRows []model.CollectRecordFilmItem
	if err := gdb.Order("mid ASC").Find(&filmRows).Error; err != nil {
		t.Fatal(err)
	}
	if len(filmRows) != 1 || filmRows[0].Mid != 43 || filmRows[0].SourceID != "jy" {
		t.Fatalf("only the stored mid should be kept, got %+v", filmRows)
	}
}

func TestReplayFilmIDsAreNotCopiedToIdleSources(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
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
	batch.AddNotifyMIDs(srcA.Id, []int64{43})
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

func TestPersistCollectRecord_StopAfterDoneStaysStopped(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CollectRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	src1 := model.FilmSource{Id: "stop-s1", Name: "已完成"}
	src2 := model.FilmSource{Id: "stop-s2", Name: "已停止"}
	batch := newCollectBatchContext(model.CollectTriggerManual, "停止测试", []model.FilmSource{src1, src2}, time.Now().Add(-time.Second)).withHour(24)
	defer batch.close()

	persistCollectRecord(batch, []model.SourceNotifyResult{
		{SourceID: src1.Id, SourceName: src1.Name, Status: progress.StatusDone, PageTotal: 2, SuccessCnt: 2},
		{SourceID: src2.Id, SourceName: src2.Name, Status: progress.StatusStopped, PageTotal: 2, PageCurrent: 1},
	})

	list, _, err := repository.GetCollectRecordList(model.CollectRecordQuery{Page: 1, PageSize: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("query: %v len=%d", err, len(list))
	}
	if list[0].Status != model.CollectRecordStatusStopped {
		t.Fatalf("expected stopped, got %s", list[0].Status)
	}
	if list[0].FailedSources != 0 || list[0].SuccessSources != 1 {
		t.Fatalf("sources success=%d failed=%d", list[0].SuccessSources, list[0].FailedSources)
	}
}

func TestStartBatchCollectRetry_RollbackOccupyOnRecordStartError(t *testing.T) {
	prev := db.Mdb
	db.Mdb = nil // 模拟数据库未连接导致 recordStart 失败
	t.Cleanup(func() { db.Mdb = prev })

	src := model.FilmSource{Id: "retry-rollback-s1", Name: "重试回滚源"}
	occupied, err := occupyAndMarkCollectSources([]model.FilmSource{src}, "q-test-retry")
	if err != nil || len(occupied) != 1 {
		t.Fatalf("occupy failed: %v", err)
	}
	if !isOccupiedCollectSource(src.Id) {
		t.Fatal("source should be occupied initially")
	}

	_, err = StartBatchCollectRetry("parent-b-1", 24, []model.FilmSource{src})
	if err == nil {
		t.Fatal("expected error when db is nil")
	}

	// 验证失败后已立即回滚占用，站点不卡在采集中
	if isOccupiedCollectSource(src.Id) {
		t.Fatal("source should NOT be occupied after StartBatchCollectRetry failure")
	}
	snap, ok := progress.Snapshot(src.Id)
	if !ok || snap.Status != progress.StatusStopped {
		t.Fatalf("expected progress status stopped, got ok=%v, status=%s", ok, snap.Status)
	}
}

func TestSingleFilmBatch_RecordStartCreatesRunning(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CollectRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	src := model.FilmSource{Id: "single-record-s1", Name: "单片记账源"}
	batch := newCollectBatchContext(
		model.CollectTriggerSingleUpdate,
		model.CollectTagSingleUpdate,
		[]model.FilmSource{src},
		time.Now(),
	).withReplayFilmIDs([]int64{42})
	defer batch.close()

	if err := batch.recordStart(); err != nil {
		t.Fatalf("recordStart: %v", err)
	}

	list, total, err := repository.GetCollectRecordList(model.CollectRecordQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("expected 1 running record, got total=%d len=%d", total, len(list))
	}
	rec := list[0]
	if rec.Status != model.CollectRecordStatusRunning {
		t.Fatalf("expected running, got %s", rec.Status)
	}
	if rec.Trigger != model.CollectTriggerSingleUpdate {
		t.Fatalf("trigger %s", rec.Trigger)
	}
	if rec.FilmIDs != "42" {
		t.Fatalf("film ids %q", rec.FilmIDs)
	}
	if rec.SourceCount != 1 {
		t.Fatalf("source count %d", rec.SourceCount)
	}
}

func TestSingleFilmBatch_RecordStartErrorReleasesOccupy(t *testing.T) {
	prev := db.Mdb
	db.Mdb = nil // 模拟数据库未连接导致 recordStart 失败
	t.Cleanup(func() { db.Mdb = prev })

	src := model.FilmSource{Id: "single-rollback-s1", Name: "单片回滚源"}
	occupied, err := occupyAndMarkCollectSources([]model.FilmSource{src}, "单片更新")
	if err != nil || len(occupied) != 1 {
		t.Fatalf("occupy failed: %v", err)
	}
	if !isOccupiedCollectSource(src.Id) {
		t.Fatal("source should be occupied initially")
	}

	batch := newCollectBatchContext(
		model.CollectTriggerSingleUpdate,
		model.CollectTagSingleUpdate,
		occupied,
		time.Now(),
	).withReplayFilmIDs([]int64{99})
	err = batch.recordStart()
	if err == nil {
		t.Fatal("expected error when db is nil")
	}
	// 模拟 CollectSingleFilm 在 recordStart 失败后 return，由 defer close 释放占用
	batch.close()

	if isOccupiedCollectSource(src.Id) {
		t.Fatal("source should NOT be occupied after recordStart failure + close")
	}
}

func TestCheckpointKeepsWrittenFilmsAfterInterrupt(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
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

	src := model.FilmSource{Id: "film-src", Name: "入库站"}
	batch := newCollectBatchContext(model.CollectTriggerCron, model.CollectTagCronCollect, []model.FilmSource{src}, time.Now()).withHour(24)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}
	batch.noteSourceHour(src.Id, 48)
	batch.AddAffectedMIDs(&src, 48, []int64{11, 12})
	batch.AddNotifyMIDs(src.Id, []int64{11})
	batch.flushRecordSync()

	var films int64
	if err := gdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", batch.batchID).Count(&films).Error; err != nil {
		t.Fatal(err)
	}
	if films != 2 {
		t.Fatalf("written films = %d, want both affected mids", films)
	}
	rec, err := repository.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusRunning || rec.FilmCount != 2 || rec.Hour != 24 {
		t.Fatalf("running record status=%s films=%d hour=%d", rec.Status, rec.FilmCount, rec.Hour)
	}
	if !strings.Contains(rec.DetailJson, `"filmsTotal":2`) || !strings.Contains(rec.DetailJson, `"hour":48`) {
		t.Fatalf("detail = %s", rec.DetailJson)
	}

	if err := repository.MarkInterruptedCollectRecords(); err != nil {
		t.Fatal(err)
	}
	rec, err = repository.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusInterrupted || rec.FilmCount != 2 || rec.Hour != 24 {
		t.Fatalf("interrupted record status=%s films=%d hour=%d", rec.Status, rec.FilmCount, rec.Hour)
	}
	if !strings.Contains(rec.DetailJson, `"hour":48`) {
		t.Fatalf("interrupt dropped source hour: %s", rec.DetailJson)
	}
}
