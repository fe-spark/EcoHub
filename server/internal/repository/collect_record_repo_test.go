package repository

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CollectRecord{}, &model.CollectRecordFilmItem{}, &model.FilmIndex{}); err != nil {
		t.Fatalf("migrate collect record failed: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() {
		db.Mdb = prev
	})
	return gdb
}

func TestCollectRecordLifecycle(t *testing.T) {
	setupTestDB(t)

	now := time.Now()
	rec := &model.CollectRecord{
		BatchID:     "batch-test-1",
		Trigger:     model.CollectTriggerManual,
		Tag:         "单站采集",
		Hour:        24,
		Status:      model.CollectRecordStatusRunning,
		SourceCount: 1,
		StartedAt:   now,
	}

	if err := CreateCollectRecord(rec); err != nil {
		t.Fatalf("create collect record: %v", err)
	}

	// 验证更新结算
	comp := model.CollectRecordCompletion{
		Status:      model.CollectRecordStatusSuccess,
		FinishedAt:  now.Add(5 * time.Second),
		DurationMs:  5000,
		FilmCount:   10,
		PageTotal:   2,
		PageSuccess: 2,
		PageFailed:  0,
		ErrorMsg:    "",
		DetailJson:  `[{"sourceId":"s1","sourceName":"源1","status":"success"}]`,
	}
	if err := CompleteCollectRecord("batch-test-1", comp); err != nil {
		t.Fatalf("complete collect record: %v", err)
	}

	// 验证查询
	list, total, err := GetCollectRecordList(model.CollectRecordQuery{
		Page:     1,
		PageSize: 10,
		Trigger:  model.CollectTriggerManual,
	})
	if err != nil {
		t.Fatalf("list collect records: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("expected 1 record, got total=%d, len=%d", total, len(list))
	}
	if list[0].Status != model.CollectRecordStatusSuccess || list[0].FilmCount != 10 {
		t.Fatalf("unexpected record content: %+v", list[0])
	}

	// 验证按 ID 批量删除
	if err := DeleteCollectRecords([]uint{list[0].ID}); err != nil {
		t.Fatalf("delete record: %v", err)
	}
	_, totalAfterDel, err := GetCollectRecordList(model.CollectRecordQuery{})
	if err != nil || totalAfterDel != 0 {
		t.Fatalf("expected 0 after delete, got %d, err=%v", totalAfterDel, err)
	}
}

func TestCleanOldCollectRecords(t *testing.T) {
	setupTestDB(t)

	oldTime := time.Now().AddDate(0, 0, -35)
	newTime := time.Now().AddDate(0, 0, -5)

	_ = CreateCollectRecord(&model.CollectRecord{
		BatchID:   "batch-old",
		Trigger:   model.CollectTriggerCron,
		Status:    model.CollectRecordStatusSuccess,
		StartedAt: oldTime,
	})
	_ = CreateCollectRecord(&model.CollectRecord{
		BatchID:    "batch-new",
		Trigger:    model.CollectTriggerCron,
		Status:     model.CollectRecordStatusSuccess,
		FilmCount:  1,
		DetailJson: `[{"sourceId":"s1","filmsTotal":1}]`,
		StartedAt:  newTime,
	})

	if err := BatchSaveCollectRecordFilms("batch-old", map[string][]int64{"s1": {1}}); err != nil {
		t.Fatal(err)
	}
	if err := BatchSaveCollectRecordFilms("batch-new", map[string][]int64{"s1": {2}}); err != nil {
		t.Fatal(err)
	}
	if err := db.Mdb.Create(&model.CollectRecordFilmItem{
		BatchID:   "batch-orphan",
		SourceID:  "s1",
		Mid:       3,
		CreatedAt: oldTime,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := CleanOldCollectRecords(30); err != nil {
		t.Fatalf("clean old records: %v", err)
	}

	list, total, err := GetCollectRecordList(model.CollectRecordQuery{})
	if err != nil || total != 1 || list[0].BatchID != "batch-new" {
		t.Fatalf("expected only batch-new, got total=%d, list=%+v", total, list)
	}
	_, oldTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "batch-old", SourceID: "s1", Page: 1, PageSize: 10,
	})
	if err != nil || oldTotal != 0 {
		t.Fatalf("old batch films should be gone, total=%d err=%v", oldTotal, err)
	}
	_, orphanTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "batch-orphan", SourceID: "s1", Page: 1, PageSize: 10,
	})
	if err != nil || orphanTotal != 0 {
		t.Fatalf("orphan films should be gone, total=%d err=%v", orphanTotal, err)
	}
	keptFilms, keptTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "batch-new", SourceID: "s1", Page: 1, PageSize: 10,
	})
	if err != nil || keptTotal != 1 || len(keptFilms) != 1 || keptFilms[0].Mid != 2 {
		t.Fatalf("new batch films should stay, total=%d list=%+v err=%v", keptTotal, keptFilms, err)
	}
}

func TestMarkInterruptedCollectRecords(t *testing.T) {
	setupTestDB(t)

	started := time.Now().Add(-2 * time.Minute)
	_ = CreateCollectRecord(&model.CollectRecord{
		BatchID:    "batch-running",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusRunning,
		StartedAt:  started,
		DetailJson: `[{"sourceId":"s1","sourceName":"站","status":"running","filmsTotal":0}]`,
	})
	if err := BatchSaveCollectRecordFilms("batch-running", map[string][]int64{"s1": {11, 12}}); err != nil {
		t.Fatal(err)
	}

	if err := MarkInterruptedCollectRecords(); err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}

	list, _, err := GetCollectRecordList(model.CollectRecordQuery{})
	if err != nil || len(list) != 1 {
		t.Fatalf("query failed: %v", err)
	}
	if list[0].Status != model.CollectRecordStatusInterrupted {
		t.Fatalf("expected status interrupted, got %s", list[0].Status)
	}
	if list[0].DurationMs < int64(time.Minute/time.Millisecond) {
		t.Fatalf("expected duration from started_at, got %d", list[0].DurationMs)
	}
	if list[0].FinishedAt == nil {
		t.Fatal("expected finished_at")
	}
	if list[0].FilmCount != 2 || !strings.Contains(list[0].DetailJson, `"filmsTotal":2`) || !strings.Contains(list[0].DetailJson, `"status":"interrupted"`) {
		t.Fatalf("interrupted films=%d detail=%s", list[0].FilmCount, list[0].DetailJson)
	}
}

func TestCompleteCollectRecordMissing(t *testing.T) {
	setupTestDB(t)

	err := CompleteCollectRecord("missing-batch", model.CollectRecordCompletion{
		Status:     model.CollectRecordStatusFailed,
		FinishedAt: time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "采集记录不存在") {
		t.Fatalf("expected 采集记录不存在, got %v", err)
	}
}

func TestCompleteCollectRecordIdempotentAndExcludeRunning(t *testing.T) {
	setupTestDB(t)

	rec := &model.CollectRecord{
		BatchID:   "batch-running-protect",
		Trigger:   model.CollectTriggerManual,
		Status:    model.CollectRecordStatusRunning,
		StartedAt: time.Now(),
	}
	if err := CreateCollectRecord(rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	// 运行中记录不可被批量删除或清空
	if err := DeleteCollectRecords([]uint{rec.ID}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, total, err := GetCollectRecordList(model.CollectRecordQuery{})
	if err != nil || total != 1 {
		t.Fatalf("running record should not be deleted, total=%d", total)
	}

	if err := ClearCollectRecords(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	_, total, err = GetCollectRecordList(model.CollectRecordQuery{})
	if err != nil || total != 1 {
		t.Fatalf("running record should not be cleared, total=%d", total)
	}

	// 正常完成
	comp := model.CollectRecordCompletion{
		Status:     model.CollectRecordStatusSuccess,
		FinishedAt: time.Now(),
	}
	if err := CompleteCollectRecord("batch-running-protect", comp); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, _ := GetCollectRecordByBatchID("batch-running-protect")
	if got.Status != model.CollectRecordStatusSuccess {
		t.Fatalf("expected success, got %s", got.Status)
	}

	// 二次完成保持幂等，不会被后来的调用覆盖
	compFailed := model.CollectRecordCompletion{
		Status:     model.CollectRecordStatusFailed,
		FinishedAt: time.Now(),
	}
	if err := CompleteCollectRecord("batch-running-protect", compFailed); err != nil {
		t.Fatalf("second complete: %v", err)
	}
	gotAgain, _ := GetCollectRecordByBatchID("batch-running-protect")
	if gotAgain.Status != model.CollectRecordStatusSuccess {
		t.Fatalf("status should stay success, got %s", gotAgain.Status)
	}
}

func TestGetCollectRecordListQuickFilters(t *testing.T) {
	setupTestDB(t)

	now := time.Now()
	rows := []model.CollectRecord{
		{BatchID: "ok-empty", Trigger: model.CollectTriggerManual, Status: model.CollectRecordStatusSuccess, FilmCount: 0, StartedAt: now},
		{BatchID: "ok-films", Trigger: model.CollectTriggerManual, Status: model.CollectRecordStatusSuccess, FilmCount: 25, StartedAt: now.Add(-time.Minute)},
		{BatchID: "partial", Trigger: model.CollectTriggerCron, Status: model.CollectRecordStatusPartial, FilmCount: 3, FailedSources: 1, StartedAt: now.Add(-2 * time.Minute)},
		{BatchID: "interrupted", Trigger: model.CollectTriggerManual, Status: model.CollectRecordStatusInterrupted, StartedAt: now.Add(-3 * time.Minute)},
	}
	for i := range rows {
		if err := CreateCollectRecord(&rows[i]); err != nil {
			t.Fatalf("create %s: %v", rows[i].BatchID, err)
		}
	}

	updated, total, err := GetCollectRecordList(model.CollectRecordQuery{Page: 1, PageSize: 10, UpdatedOnly: true})
	if err != nil {
		t.Fatalf("updated filter: %v", err)
	}
	if total != 2 || len(updated) != 2 {
		t.Fatalf("updated filter expected 2, total=%d len=%d", total, len(updated))
	}

	failed, total, err := GetCollectRecordList(model.CollectRecordQuery{Page: 1, PageSize: 10, FailedOnly: true})
	if err != nil {
		t.Fatalf("failed filter: %v", err)
	}
	if total != 2 || len(failed) != 2 {
		t.Fatalf("failed filter expected 2, total=%d len=%d", total, len(failed))
	}

	both, total, err := GetCollectRecordList(model.CollectRecordQuery{Page: 1, PageSize: 10, UpdatedOnly: true, FailedOnly: true})
	if err != nil {
		t.Fatalf("both filters: %v", err)
	}
	if total != 1 || len(both) != 1 || both[0].BatchID != "partial" {
		t.Fatalf("both filters expected partial, total=%d list=%v", total, both)
	}
}

func TestPurgeSourceCollectHistory(t *testing.T) {
	gdb := setupTestDB(t)
	if err := gdb.AutoMigrate(&model.CollectSourceStats{}, &model.FilmSource{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src-a", Name: "A", Uri: "http://a.example"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src-b", Name: "B", Uri: "http://b.example"}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := gdb.Create(&model.CollectSourceStats{SourceId: "src-a"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CollectSourceStats{SourceId: "src-b"}).Error; err != nil {
		t.Fatal(err)
	}
	alone := &model.CollectRecord{
		BatchID:    "only-a",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusSuccess,
		StartedAt:  now,
		DetailJson: `[{"sourceId":"src-a","sourceName":"A","status":"success"}]`,
	}
	shared := &model.CollectRecord{
		BatchID:    "a-and-b",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusSuccess,
		StartedAt:  now,
		DetailJson: `[{"sourceId":"src-a","status":"success","filmsTotal":1},{"sourceId":"src-b","status":"success","filmsTotal":1}]`,
	}
	if err := CreateCollectRecord(alone); err != nil {
		t.Fatal(err)
	}
	if err := CreateCollectRecord(shared); err != nil {
		t.Fatal(err)
	}
	if err := BatchSaveCollectRecordFilms("only-a", map[string][]int64{"src-a": {1}}); err != nil {
		t.Fatal(err)
	}
	if err := BatchSaveCollectRecordFilms("a-and-b", map[string][]int64{
		"src-a": {2},
		"src-b": {3},
	}); err != nil {
		t.Fatal(err)
	}

	if err := gdb.Transaction(func(tx *gorm.DB) error {
		return purgeSourceCollectHistoryTx(tx, "src-a")
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := GetCollectRecordByBatchID("only-a"); err == nil {
		t.Fatal("single-source batch should be deleted")
	}
	sharedLeft, err := GetCollectRecordByBatchID("a-and-b")
	if err != nil {
		t.Fatal("multi-source batch should stay")
	}
	if strings.Contains(sharedLeft.DetailJson, "src-a") || sharedLeft.SourceCount != 1 || sharedLeft.FilmCount != 1 {
		t.Fatalf("src-a should be removed from the batch, got %+v", sharedLeft)
	}
	_, goneTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "only-a", SourceID: "src-a", Page: 1, PageSize: 10,
	})
	if err != nil || goneTotal != 0 {
		t.Fatalf("deleted batch films should be gone, total=%d err=%v", goneTotal, err)
	}
	_, aTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "a-and-b", SourceID: "src-a", Page: 1, PageSize: 10,
	})
	if err != nil || aTotal != 0 {
		t.Fatalf("src-a films should be gone, total=%d err=%v", aTotal, err)
	}
	kept, keptTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "a-and-b", SourceID: "src-b", Page: 1, PageSize: 10,
	})
	if err != nil || keptTotal != 1 || len(kept) != 1 || kept[0].Mid != 3 {
		t.Fatalf("src-b films should stay, total=%d list=%+v err=%v", keptTotal, kept, err)
	}
	var statCount int64
	if err := gdb.Model(&model.CollectSourceStats{}).Where("source_id = ?", "src-a").Count(&statCount).Error; err != nil {
		t.Fatal(err)
	}
	if statCount != 0 {
		t.Fatalf("src-a stats should be deleted, count=%d", statCount)
	}
	if err := gdb.Model(&model.CollectSourceStats{}).Where("source_id = ?", "src-b").Count(&statCount).Error; err != nil {
		t.Fatal(err)
	}
	if statCount != 1 {
		t.Fatalf("src-b stats should stay, count=%d", statCount)
	}

	if err := gdb.Where("id = ?", "src-a").Delete(&model.FilmSource{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		return purgeSourceCollectHistoryTx(tx, "src-b")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := GetCollectRecordByBatchID("a-and-b"); err == nil {
		t.Fatal("batch should be deleted after its last source is removed")
	}
}

func TestClearAndDeleteKeepRunningFilms(t *testing.T) {
	setupTestDB(t)
	now := time.Now()
	running := &model.CollectRecord{
		BatchID:    "batch-running",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusRunning,
		FilmCount:  1,
		DetailJson: `[{"sourceId":"s1","filmsTotal":1}]`,
		StartedAt:  now,
	}
	done := &model.CollectRecord{
		BatchID:   "batch-done",
		Trigger:   model.CollectTriggerManual,
		Status:    model.CollectRecordStatusSuccess,
		StartedAt: now,
	}
	if err := CreateCollectRecord(running); err != nil {
		t.Fatal(err)
	}
	if err := CreateCollectRecord(done); err != nil {
		t.Fatal(err)
	}
	if err := BatchSaveCollectRecordFilms("batch-running", map[string][]int64{"s1": {11}}); err != nil {
		t.Fatal(err)
	}
	if err := BatchSaveCollectRecordFilms("batch-done", map[string][]int64{"s1": {22}}); err != nil {
		t.Fatal(err)
	}

	if err := ClearCollectRecords(); err != nil {
		t.Fatal(err)
	}
	if _, err := GetCollectRecordByBatchID("batch-done"); err == nil {
		t.Fatal("finished record should be cleared")
	}
	kept, err := GetCollectRecordByBatchID("batch-running")
	if err != nil || kept.Status != model.CollectRecordStatusRunning {
		t.Fatalf("running record missing: %v", err)
	}
	runningFilms, runningTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "batch-running", SourceID: "s1", Page: 1, PageSize: 10,
	})
	if err != nil || runningTotal != 1 || len(runningFilms) != 1 || runningFilms[0].Mid != 11 {
		t.Fatalf("running films = %+v total=%d err=%v", runningFilms, runningTotal, err)
	}
	_, doneTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "batch-done", SourceID: "s1", Page: 1, PageSize: 10,
	})
	if err != nil || doneTotal != 0 {
		t.Fatalf("finished films should be gone, total=%d err=%v", doneTotal, err)
	}

	extra := &model.CollectRecord{
		BatchID:   "batch-extra",
		Trigger:   model.CollectTriggerManual,
		Status:    model.CollectRecordStatusFailed,
		StartedAt: now,
	}
	if err := CreateCollectRecord(extra); err != nil {
		t.Fatal(err)
	}
	if err := BatchSaveCollectRecordFilms("batch-extra", map[string][]int64{"s1": {33}}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCollectRecords([]uint{extra.ID, kept.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := GetCollectRecordByBatchID("batch-extra"); err == nil {
		t.Fatal("selected finished record should be deleted")
	}
	if _, err := GetCollectRecordByBatchID("batch-running"); err != nil {
		t.Fatal("running record should stay when selected")
	}
	_, extraTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "batch-extra", SourceID: "s1", Page: 1, PageSize: 10,
	})
	if err != nil || extraTotal != 0 {
		t.Fatalf("deleted batch films should be gone, total=%d err=%v", extraTotal, err)
	}
	_, stillTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "batch-running", SourceID: "s1", Page: 1, PageSize: 10,
	})
	if err != nil || stillTotal != 1 {
		t.Fatalf("running films should stay, total=%d err=%v", stillTotal, err)
	}
}
