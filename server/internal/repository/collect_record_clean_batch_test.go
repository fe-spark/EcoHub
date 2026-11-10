package repository

import (
	"fmt"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

func TestCleanOldCollectRecords_MultiChunkKeepsRunningAndRecent(t *testing.T) {
	gdb := setupTestDB(t)
	origRec, origFilm := collectCleanRecordChunk, collectCleanFilmChunk
	collectCleanRecordChunk, collectCleanFilmChunk = 2, 3
	t.Cleanup(func() { collectCleanRecordChunk, collectCleanFilmChunk = origRec, origFilm })

	oldTime := time.Now().AddDate(0, 0, -40)
	newTime := time.Now().AddDate(0, 0, -2)
	addBatch := func(batchID, status string, started time.Time, films int) {
		t.Helper()
		if err := gdb.Create(&model.CollectRecord{
			BatchID: batchID, Trigger: model.CollectTriggerCron, Status: status, StartedAt: started,
		}).Error; err != nil {
			t.Fatal(err)
		}
		for i := 0; i < films; i++ {
			if err := gdb.Create(&model.CollectRecordFilmItem{
				BatchID: batchID, SourceID: "s1", Mid: int64(i + 1), CreatedAt: started,
			}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	// 5 条过期已结束批次，每批 7 行清单 → 多轮记录 + 多轮清单删除。
	for i := 0; i < 5; i++ {
		addBatch(fmt.Sprintf("old-%d", i), model.CollectRecordStatusSuccess, oldTime, 7)
	}
	addBatch("old-running", model.CollectRecordStatusRunning, oldTime, 4)
	addBatch("recent", model.CollectRecordStatusSuccess, newTime, 4)
	// 没有记录的旧清单行（7 行）与没有记录的新清单行（2 行）。
	for i := 0; i < 7; i++ {
		gdb.Create(&model.CollectRecordFilmItem{BatchID: "gone", SourceID: "s1", Mid: int64(100 + i), CreatedAt: oldTime})
	}
	for i := 0; i < 2; i++ {
		gdb.Create(&model.CollectRecordFilmItem{BatchID: "gone-new", SourceID: "s1", Mid: int64(200 + i), CreatedAt: newTime})
	}

	if err := CleanOldCollectRecords(30); err != nil {
		t.Fatalf("clean: %v", err)
	}

	var batches []string
	db.Mdb.Model(&model.CollectRecord{}).Order("batch_id").Pluck("batch_id", &batches)
	if fmt.Sprint(batches) != "[old-running recent]" {
		t.Fatalf("records left = %v", batches)
	}
	count := func(batchID string) int64 {
		var n int64
		db.Mdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", batchID).Count(&n)
		return n
	}
	if n := count("old-running"); n != 4 {
		t.Fatalf("running batch films = %d, want 4", n)
	}
	if n := count("recent"); n != 4 {
		t.Fatalf("recent batch films = %d, want 4", n)
	}
	if n := count("gone-new"); n != 2 {
		t.Fatalf("recent orphan films = %d, want 2", n)
	}
	var total int64
	db.Mdb.Model(&model.CollectRecordFilmItem{}).Count(&total)
	if total != 10 {
		t.Fatalf("films left = %d, want 10", total)
	}
}

func TestCleanOldCollectRecords_RecordTurnedRunningKeepsRecordAndFilms(t *testing.T) {
	gdb := setupTestDB(t)
	oldTime := time.Now().AddDate(0, 0, -40)
	for _, batchID := range []string{"done", "revived"} {
		if err := gdb.Create(&model.CollectRecord{
			BatchID: batchID, Trigger: model.CollectTriggerCron, Status: model.CollectRecordStatusSuccess, StartedAt: oldTime,
		}).Error; err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if err := gdb.Create(&model.CollectRecordFilmItem{
				BatchID: batchID, SourceID: "s1", Mid: int64(i + 1), CreatedAt: oldTime,
			}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	// 查出过期记录之后、删除之前，"revived" 变成 running。
	collectCleanBeforeDeleteHook = func([]uint) {
		gdb.Model(&model.CollectRecord{}).Where("batch_id = ?", "revived").Update("status", model.CollectRecordStatusRunning)
	}
	t.Cleanup(func() { collectCleanBeforeDeleteHook = nil })

	if err := CleanOldCollectRecords(30); err != nil {
		t.Fatalf("clean: %v", err)
	}
	var batches []string
	db.Mdb.Model(&model.CollectRecord{}).Order("batch_id").Pluck("batch_id", &batches)
	if fmt.Sprint(batches) != "[revived]" {
		t.Fatalf("records left = %v", batches)
	}
	var n int64
	db.Mdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", "revived").Count(&n)
	if n != 3 {
		t.Fatalf("revived batch films = %d, want 3", n)
	}
	db.Mdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", "done").Count(&n)
	if n != 0 {
		t.Fatalf("done batch films = %d, want 0", n)
	}
}
