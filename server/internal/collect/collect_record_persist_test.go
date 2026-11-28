package collect

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"server/internal/collect/ledger"
	"server/internal/collect/progress"
	"server/internal/film/catalog"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPersistCollectRecord(t *testing.T) {
	dsn := testutil.MemDSN(t)
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

	list, total, err := ledger.GetCollectRecordList(model.CollectRecordQuery{
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

func TestPersistCollectRecord_StopAfterDoneStaysStopped(t *testing.T) {
	dsn := testutil.MemDSN(t)
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

	list, _, err := ledger.GetCollectRecordList(model.CollectRecordQuery{Page: 1, PageSize: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("query: %v len=%d", err, len(list))
	}
	if list[0].Status != model.CollectRecordStatusPartialStopped {
		t.Fatalf("expected partial_stopped, got %s", list[0].Status)
	}
	if list[0].FailedSources != 0 || list[0].SuccessSources != 1 {
		t.Fatalf("sources success=%d failed=%d", list[0].SuccessSources, list[0].FailedSources)
	}
}

func TestStartBatchCollectRetry_RollbackOccupyOnRecordStartError(t *testing.T) {
	prev := db.Mdb
	db.Mdb = nil // 模拟数据库未连接导致 recordStart 失败
	t.Cleanup(func() { db.Mdb = prev })
	// Cleanup 按后进先出执行，必须先等后台重试（含结算与回调）退出再恢复 db.Mdb。
	t.Cleanup(func() {
		if !WaitBackgroundCollects(10 * time.Second) {
			t.Error("background retry collect did not exit before cleanup")
		}
	})

	src := model.FilmSource{Id: "retry-rollback-s1", Name: "重试回滚源"}
	claim, err := claimCollectSources([]model.FilmSource{src}, "q-test-retry")
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("occupy failed: %v", err)
	}
	if !isOccupiedCollectSource(src.Id) {
		t.Fatal("source should be occupied initially")
	}

	batchID, err := StartBatchCollectRetry("parent-b-1", 24, claim)
	if err != nil || batchID == "" {
		t.Fatalf("record write failure should not cancel retry: id=%s err=%v", batchID, err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for isOccupiedCollectSource(src.Id) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if isOccupiedCollectSource(src.Id) {
		t.Fatal("source stayed occupied after the retry run failed")
	}
	// 等待后台采集批次（含队列释放与 emitSummary 落库尝试）完全退出，避免污染后续测试或与 Cleanup 竞态。
	if !WaitBackgroundCollects(10 * time.Second) {
		t.Fatal("background retry collect did not exit before deadline")
	}
}

func TestStartBatchCollectRetry_ReleasesOccupyWhenQueueRejected(t *testing.T) {
	t.Cleanup(func() {
		collectLifecycle.endSource("retry-seal-s1")
		resetSearchReseal()
	})
	searchReseal.mu.Lock()
	searchReseal.running = true
	searchReseal.sealing["retry-gap"] = struct{}{}
	searchReseal.mu.Unlock()

	src := model.FilmSource{Id: "retry-seal-s1", Name: "封口拒绝"}
	claim, err := claimCollectSources([]model.FilmSource{src}, "q-test-retry-seal")
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("occupy failed: %v", err)
	}
	if !isOccupiedCollectSource(src.Id) {
		t.Fatal("source should be occupied initially")
	}

	_, err = StartBatchCollectRetry("parent-seal", 24, claim)
	if err != errSearchSealPending {
		t.Fatalf("err = %v", err)
	}
	if isOccupiedCollectSource(src.Id) {
		t.Fatal("source should NOT be occupied after queue admission is rejected")
	}
	snap, ok := progress.Snapshot(src.Id)
	if !ok || snap.Status != progress.StatusStopped {
		t.Fatalf("expected progress status stopped, got ok=%v, status=%s", ok, snap.Status)
	}
}

func TestSingleFilmBatch_RecordStartCreatesRunning(t *testing.T) {
	dsn := testutil.MemDSN(t)
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

	list, total, err := ledger.GetCollectRecordList(model.CollectRecordQuery{Page: 1, PageSize: 10})
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
	if rec.TargetMids != "[42]" {
		t.Fatalf("expected target_mids=[42], got %q", rec.TargetMids)
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
	t.Cleanup(func() { progress.MarkStopped(src.Id) })
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

func TestRunningRecordStaysEmptyUntilInterrupt(t *testing.T) {
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

	src := model.FilmSource{Id: "film-src", Name: "入库站"}
	batch := newCollectBatchContext(model.CollectTriggerCron, model.CollectTagCronCollect, []model.FilmSource{src}, time.Now()).withHour(24)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}
	batch.noteSourceHour(src.Id, 48)
	batch.AddAffectedMIDs(&src, 48, []int64{11, 12})

	rec, err := ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusRunning || rec.FilmCount != 0 || !strings.Contains(rec.DetailJson, `"status":"running"`) {
		t.Fatalf("running record status=%s films=%d detail=%s", rec.Status, rec.FilmCount, rec.DetailJson)
	}

	if err := ledger.MarkInterruptedCollectRecords(); err != nil {
		t.Fatal(err)
	}
	rec, err = ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusInterrupted || rec.Hour != 24 {
		t.Fatalf("interrupted record status=%s hour=%d", rec.Status, rec.Hour)
	}
}

func TestCheckpointKeepsSourceRunningUntilPersist(t *testing.T) {
	dsn := testutil.MemDSN(t)
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

	src := model.FilmSource{Id: "seal-src", Name: "封口站"}
	batch := newCollectBatchContext(model.CollectTriggerManual, model.CollectTagBatchCollect, []model.FilmSource{src}, time.Now())
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}
	batch.recordSourceSnapshot(model.CollectRecordSourceDetail{
		SourceID:   src.Id,
		SourceName: src.Name,
		Status:     progress.StatusSealing,
		PageTotal:  10,
		SuccessCnt: 8,
		FailedCnt:  1,
		FilmsTotal: 3,
		Error:      "限流",
	})

	rec, err := ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusRunning || rec.PageTotal != 0 || !strings.Contains(rec.DetailJson, `"status":"running"`) || strings.Contains(rec.DetailJson, "限流") {
		t.Fatalf("running record was updated early: status=%s pages=%d detail=%s", rec.Status, rec.PageTotal, rec.DetailJson)
	}

	persistCollectRecord(batch, []model.SourceNotifyResult{{
		SourceID:   src.Id,
		SourceName: src.Name,
		Status:     progress.StatusSealing,
		PageTotal:  10,
		SuccessCnt: 8,
		FailedCnt:  1,
		FilmsTotal: 3,
		Error:      "限流",
	}})
	rec, err = ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusPartial || !strings.Contains(rec.DetailJson, `"status":"partial"`) || !strings.Contains(rec.DetailJson, "限流") {
		t.Fatalf("settled status=%s detail=%s", rec.Status, rec.DetailJson)
	}
}

func TestCollectMarksFilmIndexLastRecordID(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	primarySrc := model.FilmSource{Id: "primary-src", Name: "主站", Uri: "http://primary.example/api", State: true, Sort: 0}
	backupSrc := model.FilmSource{Id: "backup-src", Name: "副站", Uri: "http://backup.example/api", State: true, Sort: 1}
	_ = gdb.Create(&[]model.FilmSource{primarySrc, backupSrc}).Error
	_ = gdb.Create(&model.PrimaryState{Id: 1, PrimarySourceId: primarySrc.Id}).Error
	_ = gdb.Create(&model.Category{Id: 1, Name: "电影", StableKey: "movie", Show: true}).Error
	_ = gdb.Create(&model.SourceCategory{SourceId: primarySrc.Id, SourceTypeId: 1, RawName: "动作片"}).Error
	_ = gdb.Create(&model.CategoryMapping{SourceId: primarySrc.Id, SourceTypeId: 1, CategoryId: 1}).Error
	_ = gdb.Create(&model.SourceCategory{SourceId: backupSrc.Id, SourceTypeId: 1, RawName: "动作片"}).Error
	_ = gdb.Create(&model.CategoryMapping{SourceId: backupSrc.Id, SourceTypeId: 1, CategoryId: 1}).Error
	catalog.ClearCategoryCache()
	taxonomy.RefreshCategoryCache()
	_ = sourcecatalog.RefreshCollectSourceCatalog()
	stubStreamPublishForTest(t, func([]int64) error { return nil }, func() string { return primarySrc.Id })

	batch := newCollectBatchContext(model.CollectTriggerManual, model.CollectTagBatchCollect, []model.FilmSource{primarySrc, backupSrc}, time.Now()).withHour(-1)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}

	makeDetail := func(id int64, name string) model.MovieDetail {
		return model.MovieDetail{
			Id:         id,
			Name:       name,
			Cid:        1,
			UpdateTime: "2026-11-28 10:00:00",
			PlayList:   [][]model.MovieUrlInfo{{{Episode: "第1集", Link: "https://example.com/1.m3u8"}}},
			PlayFrom:   []string{"m3u8"},
		}
	}

	writtenPrimary, err := saveCollectedFilmForCollect(batch.WriteContext(context.Background(), &primarySrc), &primarySrc, 1, []model.MovieDetail{
		makeDetail(101, "主站新片1"),
		makeDetail(102, "主站新片2"),
	})
	if err != nil {
		t.Fatalf("save primary failed: %v", err)
	}
	batch.AddAffectedMIDs(&primarySrc, -1, writtenPrimary.Affected)

	writtenBackup, err := saveCollectedFilmForCollect(batch.WriteContext(context.Background(), &backupSrc), &backupSrc, 1, []model.MovieDetail{
		makeDetail(201, "副站新片1"),
	})
	if err != nil {
		t.Fatalf("save backup failed: %v", err)
	}
	batch.AddAffectedMIDs(&backupSrc, -1, writtenBackup.Affected)

	persistCollectRecord(batch, []model.SourceNotifyResult{
		{SourceID: primarySrc.Id, SourceName: primarySrc.Name, Status: progress.StatusDone, PageTotal: 1, SuccessCnt: 1},
		{SourceID: backupSrc.Id, SourceName: backupSrc.Name, Status: progress.StatusDone, PageTotal: 1, SuccessCnt: 1},
	})

	rec, err := ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusSuccess || rec.FilmCount != 3 {
		t.Fatalf("status=%s films=%d", rec.Status, rec.FilmCount)
	}
	var markedPrimary int64
	if err := gdb.Model(&model.FilmIndex{}).Where("first_source_id = ? AND last_record_id = ?", primarySrc.Id, rec.ID).Count(&markedPrimary).Error; err != nil {
		t.Fatal(err)
	}
	if markedPrimary != 2 {
		t.Fatalf("expected 2 primary films marked with last_record_id=%d, got %d", rec.ID, markedPrimary)
	}
	var markedBackup int64
	if err := gdb.Model(&model.FilmIndex{}).Where("first_source_id = ? AND last_record_id = ?", backupSrc.Id, rec.ID).Count(&markedBackup).Error; err != nil {
		t.Fatal(err)
	}
	if markedBackup != 0 {
		t.Fatalf("expected backup films not marked with last_record_id=%d, got %d", rec.ID, markedBackup)
	}
}

func TestPersistCollectRecord_ReleasesSourceFilmSetAfterSourceFinish(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
	stubStreamPublishForTest(t, func([]int64) error { return nil }, func() string { return "src-a" })

	srcA := model.FilmSource{Id: "src-a", Name: "站A", Uri: "http://a.example/api", State: true, Sort: 0}
	srcB := model.FilmSource{Id: "src-b", Name: "站B", Uri: "http://b.example/api", State: true, Sort: 1}
	batch := newCollectBatchContext(model.CollectTriggerManual, model.CollectTagBatchCollect, []model.FilmSource{srcA, srcB}, time.Now()).withHour(-1)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}

	// 站A跨页存在重复 mid，先累计再去重释放集合，计数应保留
	batch.AddAffectedMIDs(&srcA, -1, []int64{101, 102})
	batch.AddAffectedMIDs(&srcA, -1, []int64{102, 103})
	batch.releaseSourceFilmSet(srcA.Id)

	batch.mu.Lock()
	_, setStillExists := batch.sourceFilmSets[srcA.Id]
	batch.mu.Unlock()
	if setStillExists {
		t.Fatal("expected sourceFilmSets for finished source src-a to be released")
	}

	batch.AddAffectedMIDs(&srcB, -1, []int64{201, 202, 202})
	batch.releaseSourceFilmSet(srcB.Id)

	persistCollectRecord(batch, []model.SourceNotifyResult{
		{SourceID: srcA.Id, SourceName: srcA.Name, Status: progress.StatusDone, PageTotal: 2, SuccessCnt: 2},
		{SourceID: srcB.Id, SourceName: srcB.Name, Status: progress.StatusDone, PageTotal: 1, SuccessCnt: 1},
	})

	rec, err := ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.FilmCount != 5 {
		t.Fatalf("expected deduplicated FilmCount=5 (3+2), got %d", rec.FilmCount)
	}
	var details []model.CollectRecordSourceDetail
	if err := json.Unmarshal([]byte(rec.DetailJson), &details); err != nil {
		t.Fatal(err)
	}
	gotBySrc := map[string]int{}
	for _, d := range details {
		gotBySrc[d.SourceID] = d.FilmsTotal
	}
	if gotBySrc[srcA.Id] != 3 || gotBySrc[srcB.Id] != 2 {
		t.Fatalf("expected FilmsTotal {src-a:3, src-b:2}, got %+v", gotBySrc)
	}
}
