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
	// 等待后台采集队列完全退出，避免污染后续依赖 collectQueues.n == 0 的测试
	for time.Now().Before(deadline) {
		collectQueues.mu.Lock()
		n := collectQueues.n
		collectQueues.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
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
	if rec.Ledger {
		t.Fatal("running record should not have a film ledger yet")
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

func TestRunningRecordStaysEmptyUntilInterrupt(t *testing.T) {
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

	src := model.FilmSource{Id: "film-src", Name: "入库站"}
	batch := newCollectBatchContext(model.CollectTriggerCron, model.CollectTagCronCollect, []model.FilmSource{src}, time.Now()).withHour(24)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}
	batch.noteSourceHour(src.Id, 48)
	batch.AddAffectedMIDs(&src, 48, []int64{11, 12})
	batch.AddNotifyMIDs(src.Id, []int64{11})

	var films int64
	if err := gdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", batch.batchID).Count(&films).Error; err != nil {
		t.Fatal(err)
	}
	if films != 0 {
		t.Fatalf("running batch wrote %d film rows", films)
	}
	rec, err := repository.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusRunning || rec.FilmCount != 0 || !strings.Contains(rec.DetailJson, `"status":"running"`) || strings.Contains(rec.DetailJson, `"filmsTotal":2`) {
		t.Fatalf("running record status=%s films=%d detail=%s", rec.Status, rec.FilmCount, rec.DetailJson)
	}

	if err := repository.MarkInterruptedCollectRecords(); err != nil {
		t.Fatal(err)
	}
	rec, err = repository.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusInterrupted || rec.FilmCount != 0 || rec.Hour != 24 {
		t.Fatalf("interrupted record status=%s films=%d hour=%d", rec.Status, rec.FilmCount, rec.Hour)
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
	if err := gdb.AutoMigrate(&model.CollectRecord{}, &model.CollectRecordFilmItem{}); err != nil {
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

	rec, err := repository.GetCollectRecordByBatchID(batch.batchID)
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
	rec, err = repository.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusPartial || !strings.Contains(rec.DetailJson, `"status":"partial"`) || !strings.Contains(rec.DetailJson, "限流") {
		t.Fatalf("settled status=%s detail=%s", rec.Status, rec.DetailJson)
	}
}

func TestFullCollectStoresFilmLedger(t *testing.T) {
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

	src := model.FilmSource{Id: "full-src", Name: "全量站"}
	batch := newCollectBatchContext(model.CollectTriggerManual, model.CollectTagBatchCollect, []model.FilmSource{src}, time.Now()).withHour(-1)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}
	batch.AddAffectedMIDs(&src, -1, []int64{1, 2, 3})
	persistCollectRecord(batch, []model.SourceNotifyResult{{
		SourceID: src.Id, SourceName: src.Name, Status: progress.StatusDone,
		PageTotal: 1, SuccessCnt: 1, FilmsTotal: 3,
	}})

	rec, err := repository.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusSuccess || rec.FilmCount != 3 || !rec.Ledger {
		t.Fatalf("status=%s films=%d ledger=%v", rec.Status, rec.FilmCount, rec.Ledger)
	}
	var rows int64
	if err := gdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", batch.batchID).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 3 {
		t.Fatalf("full collect stored %d film ids", rows)
	}
}

func TestIncrementalCollectStoresSmallFilmLedger(t *testing.T) {
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

	src := model.FilmSource{Id: "inc-src", Name: "增量站"}
	batch := newCollectBatchContext(model.CollectTriggerCron, model.CollectTagCronCollect, []model.FilmSource{src}, time.Now()).withHour(24)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}
	batch.AddAffectedMIDs(&src, 24, []int64{11, 12})
	persistCollectRecord(batch, []model.SourceNotifyResult{{
		SourceID: src.Id, SourceName: src.Name, Status: progress.StatusDone,
		PageTotal: 1, SuccessCnt: 1, FilmsTotal: 2,
	}})

	var rows int64
	if err := gdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", batch.batchID).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("incremental ledger rows=%d", rows)
	}
	rec, err := repository.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Ledger {
		t.Fatal("incremental collect should keep the film ledger")
	}
}
