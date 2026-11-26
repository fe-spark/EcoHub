package collect

import (
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

func TestRunningRecordStreamsLedgerAndKeepsLedgerClosedUntilFinish(t *testing.T) {
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
	prevBatch := collectLedgerFlushBatch
	collectLedgerFlushBatch = 2
	t.Cleanup(func() {
		db.Mdb = prev
		collectLedgerFlushBatch = prevBatch
	})

	src := model.FilmSource{Id: "run-src", Name: "进行中站"}
	batch := newCollectBatchContext(model.CollectTriggerCron, model.CollectTagCronCollect, []model.FilmSource{src}, time.Now()).withHour(24)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}

	// 达到流式阈值后立即落账本并清空内存缓冲，但主记录仍保持 running 且 ledger=false。
	batch.AddAffectedMIDs(&src, 24, []int64{11, 12})
	batch.mu.Lock()
	pendingAfterFlush := len(batch.ledgerPending)
	batch.mu.Unlock()
	if pendingAfterFlush != 0 {
		t.Fatalf("expected streaming flush to empty pending buffer, got %d", pendingAfterFlush)
	}

	rec, err := ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusRunning || rec.Ledger || rec.FilmCount != 0 {
		t.Fatalf("while running status=%s ledger=%v films=%d", rec.Status, rec.Ledger, rec.FilmCount)
	}
	var rows int64
	if err := gdb.Model(&model.CollectRecordFilmItem{}).Where("record_id = ?", rec.ID).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("expected 2 streamed ledger rows, got %d", rows)
	}

	// 再追加 1 条未达阈值留在缓冲，结算时一并刷库并沿主键核对去重总数。
	batch.AddAffectedMIDs(&src, 24, []int64{12, 13})
	persistCollectRecord(batch, []model.SourceNotifyResult{{
		SourceID: src.Id, SourceName: src.Name, Status: progress.StatusDone,
		PageTotal: 1, SuccessCnt: 1,
	}})
	rec, err = ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusSuccess || !rec.Ledger || rec.FilmCount != 3 {
		t.Fatalf("after finish status=%s ledger=%v films=%d", rec.Status, rec.Ledger, rec.FilmCount)
	}
}

func TestStreamingLedgerFlushFailureStillSettlesRecord(t *testing.T) {
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
	prevBatch := collectLedgerFlushBatch
	collectLedgerFlushBatch = 2
	t.Cleanup(func() {
		db.Mdb = prev
		collectLedgerFlushBatch = prevBatch
	})

	stubStreamPublishForTest(t, func(mids []int64) error { return nil }, func() string { return "primary-only" })

	src := model.FilmSource{Id: "fail-src", Name: "非主站"}
	batch := newCollectBatchContext(model.CollectTriggerManual, model.CollectTagBatchCollect, []model.FilmSource{src}, time.Now()).withHour(24)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}

	// 第 1 批正常流式落账；非主站不堆积 pendingSealMIDs。
	batch.AddAffectedMIDs(&src, 24, []int64{1, 2})
	if seal := batch.takeAffectedMIDs(); len(seal) != 0 {
		t.Fatalf("non-primary source should not accumulate pendingSealMIDs, got %v", seal)
	}

	// 模拟中途写账本失败（删除表触发错误），随后再恢复表验证后续不再继续刷账。
	if err := gdb.Migrator().DropTable(&model.CollectRecordFilmItem{}); err != nil {
		t.Fatal(err)
	}
	batch.AddAffectedMIDs(&src, 24, []int64{3, 4})
	if err := gdb.AutoMigrate(&model.CollectRecordFilmItem{}); err != nil {
		t.Fatal(err)
	}
	// 已有刷账错误后，后续页面不再入队写入。
	batch.AddAffectedMIDs(&src, 24, []int64{5, 6})

	var rows int64
	gdb.Model(&model.CollectRecordFilmItem{}).Count(&rows)
	if rows != 0 {
		t.Fatalf("subsequent writes after flush error should be skipped, got %d rows", rows)
	}

	// 结算时即使账本写入失败，主记录仍应落到终态且 ledger=false，不能卡在 running。
	_, ledgerSaved := persistCollectRecord(batch, []model.SourceNotifyResult{{
		SourceID: src.Id, SourceName: src.Name, Status: progress.StatusDone,
		PageTotal: 3, SuccessCnt: 3,
	}})
	if ledgerSaved {
		t.Fatal("expected ledgerSaved=false after streaming flush error")
	}
	rec, err := ledger.GetCollectRecordByBatchID(batch.batchID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != model.CollectRecordStatusSuccess || rec.Ledger {
		t.Fatalf("record should settle to success with ledger=false, got status=%s ledger=%v", rec.Status, rec.Ledger)
	}
}
