package spider

import (
	"fmt"
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
			FilmsTotal:  3,
			Error:       "连接超时",
		},
	}
	mids := []int64{101, 102, 103}
	errs := map[string]string{src2.Id: "连接超时"}

	persistCollectRecord(batch, results, mids, errs)

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
	if rec.FilmCount != 3 {
		t.Fatalf("expected film count 3, got %d", rec.FilmCount)
	}
	if rec.PageTotal != 15 || rec.PageSuccess != 11 || rec.PageFailed != 1 {
		t.Fatalf("unexpected pages: total=%d, succ=%d, fail=%d", rec.PageTotal, rec.PageSuccess, rec.PageFailed)
	}
}
