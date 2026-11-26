package ledger

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
	midTime := time.Now().AddDate(0, 0, -10) // 主记录保留（<30天），但账本超7天应回收
	newTime := time.Now().AddDate(0, 0, -2)

	addBatch := func(batchID, status string, started time.Time, films int) uint {
		t.Helper()
		rec := &model.CollectRecord{
			BatchID:   batchID,
			Trigger:   model.CollectTriggerCron,
			Status:    status,
			FilmCount: films,
			Ledger:    films > 0 && status != model.CollectRecordStatusRunning,
			StartedAt: started,
		}
		if err := gdb.Create(rec).Error; err != nil {
			t.Fatal(err)
		}
		for i := 0; i < films; i++ {
			if err := gdb.Create(&model.CollectRecordFilmItem{
				RecordID: rec.ID, SourceID: "s1", Mid: int64(i + 1),
			}).Error; err != nil {
				t.Fatal(err)
			}
		}
		return rec.ID
	}

	for i := 0; i < 5; i++ {
		addBatch(fmt.Sprintf("old-%d", i), model.CollectRecordStatusSuccess, oldTime, 7)
	}
	midID := addBatch("mid-expired-ledger", model.CollectRecordStatusSuccess, midTime, 5)
	runID := addBatch("old-running", model.CollectRecordStatusRunning, oldTime, 4)
	recentID := addBatch("recent", model.CollectRecordStatusSuccess, newTime, 4)

	// 无主记录的孤儿账本行。
	for i := 0; i < 7; i++ {
		gdb.Create(&model.CollectRecordFilmItem{RecordID: 999999, SourceID: "s1", Mid: int64(100 + i)})
	}

	if err := CleanOldCollectRecords(30); err != nil {
		t.Fatalf("clean: %v", err)
	}

	var batches []string
	db.Mdb.Model(&model.CollectRecord{}).Order("batch_id").Pluck("batch_id", &batches)
	if fmt.Sprint(batches) != "[mid-expired-ledger old-running recent]" {
		t.Fatalf("records left = %v", batches)
	}
	count := func(recordID uint) int64 {
		var n int64
		db.Mdb.Model(&model.CollectRecordFilmItem{}).Where("record_id = ?", recordID).Count(&n)
		return n
	}
	if n := count(runID); n != 4 {
		t.Fatalf("running batch films = %d, want 4", n)
	}
	if n := count(recentID); n != 4 {
		t.Fatalf("recent batch films = %d, want 4", n)
	}
	if n := count(midID); n != 0 {
		t.Fatalf("expired ledger films = %d, want 0", n)
	}
	midRec, err := GetCollectRecordByBatchID("mid-expired-ledger")
	if err != nil || midRec.Ledger || midRec.FilmCount != 5 {
		t.Fatalf("expired ledger record should keep filmCount=5 and set ledger=false, got %+v err=%v", midRec, err)
	}
	var total int64
	db.Mdb.Model(&model.CollectRecordFilmItem{}).Count(&total)
	if total != 8 {
		t.Fatalf("films left = %d, want 8", total)
	}
}

func TestCleanOldCollectRecords_PrunesOldestLedgersOverMaxRows(t *testing.T) {
	gdb := setupTestDB(t)
	origMax := collectLedgerMaxRows
	collectLedgerMaxRows = 6
	t.Cleanup(func() { collectLedgerMaxRows = origMax })

	now := time.Now()
	add := func(batchID string, films int) uint {
		rec := &model.CollectRecord{
			BatchID:   batchID,
			Trigger:   model.CollectTriggerManual,
			Status:    model.CollectRecordStatusSuccess,
			FilmCount: films,
			Ledger:    true,
			StartedAt: now,
		}
		if err := gdb.Create(rec).Error; err != nil {
			t.Fatal(err)
		}
		for i := 0; i < films; i++ {
			if err := gdb.Create(&model.CollectRecordFilmItem{
				RecordID: rec.ID, SourceID: "s1", Mid: int64(i + 1),
			}).Error; err != nil {
				t.Fatal(err)
			}
		}
		return rec.ID
	}
	id1 := add("b1", 4)
	id2 := add("b2", 4)
	id3 := add("b3", 2)

	if err := CleanOldCollectRecords(30); err != nil {
		t.Fatalf("clean: %v", err)
	}
	// 总计 10 > 上限 6，应先淘汰最旧的 b1（剩余 6 <= 6），b2 与 b3 保留。
	var n1, n2, n3 int64
	gdb.Model(&model.CollectRecordFilmItem{}).Where("record_id = ?", id1).Count(&n1)
	gdb.Model(&model.CollectRecordFilmItem{}).Where("record_id = ?", id2).Count(&n2)
	gdb.Model(&model.CollectRecordFilmItem{}).Where("record_id = ?", id3).Count(&n3)
	if n1 != 0 || n2 != 4 || n3 != 2 {
		t.Fatalf("rows left n1=%d n2=%d n3=%d", n1, n2, n3)
	}
	r1, _ := GetCollectRecordByBatchID("b1")
	if r1.Ledger || r1.FilmCount != 4 {
		t.Fatalf("b1 should disable ledger while keeping count=4, got %+v", r1)
	}
}

func TestCleanOldCollectRecords_RecordTurnedRunningKeepsRecordAndFilms(t *testing.T) {
	gdb := setupTestDB(t)
	oldTime := time.Now().AddDate(0, 0, -40)
	ids := map[string]uint{}
	for _, batchID := range []string{"done", "revived"} {
		rec := &model.CollectRecord{
			BatchID: batchID, Trigger: model.CollectTriggerCron, Status: model.CollectRecordStatusSuccess, StartedAt: oldTime,
		}
		if err := gdb.Create(rec).Error; err != nil {
			t.Fatal(err)
		}
		ids[batchID] = rec.ID
		for i := 0; i < 3; i++ {
			if err := gdb.Create(&model.CollectRecordFilmItem{
				RecordID: rec.ID, SourceID: "s1", Mid: int64(i + 1),
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
	db.Mdb.Model(&model.CollectRecordFilmItem{}).Where("record_id = ?", ids["revived"]).Count(&n)
	if n != 3 {
		t.Fatalf("revived batch films = %d, want 3", n)
	}
	db.Mdb.Model(&model.CollectRecordFilmItem{}).Where("record_id = ?", ids["done"]).Count(&n)
	if n != 0 {
		t.Fatalf("done batch films = %d, want 0", n)
	}
}
