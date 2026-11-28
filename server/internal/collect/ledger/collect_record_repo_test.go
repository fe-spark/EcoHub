package ledger

import (
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CollectRecord{}, &model.FilmIndex{}); err != nil {
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

	oldRec := &model.CollectRecord{
		BatchID:   "batch-old",
		Trigger:   model.CollectTriggerCron,
		Status:    model.CollectRecordStatusSuccess,
		StartedAt: oldTime,
	}
	newRec := &model.CollectRecord{
		BatchID:    "batch-new",
		Trigger:    model.CollectTriggerCron,
		Status:     model.CollectRecordStatusSuccess,
		FilmCount:  1,
		DetailJson: `[{"sourceId":"s1","filmsTotal":1}]`,
		StartedAt:  newTime,
	}
	_ = CreateCollectRecord(oldRec)
	_ = CreateCollectRecord(newRec)

	if err := CleanOldCollectRecords(30); err != nil {
		t.Fatalf("clean old records: %v", err)
	}

	list, total, err := GetCollectRecordList(model.CollectRecordQuery{})
	if err != nil || total != 1 || list[0].BatchID != "batch-new" {
		t.Fatalf("expected only batch-new, got total=%d, list=%+v", total, list)
	}
}

func TestMarkInterruptedCollectRecords(t *testing.T) {
	setupTestDB(t)

	started := time.Now().Add(-2 * time.Minute)
	runRec := &model.CollectRecord{
		BatchID:    "batch-running",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusRunning,
		StartedAt:  started,
		DetailJson: `[{"sourceId":"s1","sourceName":"站","status":"running","filmsTotal":0}]`,
	}
	_ = CreateCollectRecord(runRec)

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
	if !strings.Contains(list[0].DetailJson, `"status":"interrupted"`) {
		t.Fatalf("interrupted detail=%s", list[0].DetailJson)
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
		FilmCount:  2,
		StartedAt:  now,
		DetailJson: `[{"sourceId":"src-a","status":"success","filmsTotal":1},{"sourceId":"src-b","status":"success","filmsTotal":1}]`,
	}
	if err := CreateCollectRecord(alone); err != nil {
		t.Fatal(err)
	}
	if err := CreateCollectRecord(shared); err != nil {
		t.Fatal(err)
	}

	if err := gdb.Transaction(func(tx *gorm.DB) error {
		return PurgeSourceCollectHistoryTx(tx, "src-a")
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
		return PurgeSourceCollectHistoryTx(tx, "src-b")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := GetCollectRecordByBatchID("a-and-b"); err == nil {
		t.Fatal("batch should be deleted after its last source is removed")
	}
}
