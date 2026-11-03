package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupRetryRecordDB(t *testing.T) {
	t.Helper()
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
}

func TestRetryBatchFailedSources_RejectsZeroHourBeforeStart(t *testing.T) {
	setupRetryRecordDB(t)
	if err := repository.CreateCollectRecord(&model.CollectRecord{
		BatchID:    "hour-zero",
		Trigger:    model.CollectTriggerManual,
		Hour:       0,
		Status:     model.CollectRecordStatusFailed,
		StartedAt:  time.Now(),
		DetailJson: `[{"sourceId":"s1","status":"failed"}]`,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := CollectRecordSvc.RetryBatchFailedSources(model.CollectRecordRetryReq{BatchID: "hour-zero"})
	if err == nil || !strings.Contains(err.Error(), "采集时长不能为 0") {
		t.Fatalf("expected hour rejection, got %v", err)
	}
	_, total, qerr := repository.GetCollectRecordList(model.CollectRecordQuery{})
	if qerr != nil || total != 1 {
		t.Fatalf("retry must not create a batch, total=%d err=%v", total, qerr)
	}
}

func TestRetryBatchFailedSources_ReplaysSingleFilm(t *testing.T) {
	setupRetryRecordDB(t)
	if err := repository.CreateCollectRecord(&model.CollectRecord{
		BatchID:    "film-1",
		Trigger:    model.CollectTriggerSingleUpdate,
		Hour:       0,
		FilmIDs:    "42,42,7",
		Status:     model.CollectRecordStatusFailed,
		StartedAt:  time.Now(),
		DetailJson: `[{"sourceId":"s1","status":"failed"}]`,
	}); err != nil {
		t.Fatal(err)
	}

	called := make(chan []int64, 1)
	prev := replayCollectFilms
	replayCollectFilms = func(ids []int64) { called <- append([]int64(nil), ids...) }
	t.Cleanup(func() { replayCollectFilms = prev })

	if _, err := CollectRecordSvc.RetryBatchFailedSources(model.CollectRecordRetryReq{BatchID: "film-1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case ids := <-called:
		if len(ids) != 2 || ids[0] != 42 || ids[1] != 7 {
			t.Fatalf("replay ids = %v", ids)
		}
	case <-time.After(time.Second):
		t.Fatal("single-film replay was not started")
	}
	_, total, err := repository.GetCollectRecordList(model.CollectRecordQuery{})
	if err != nil || total != 1 {
		t.Fatalf("film replay must not open an hour batch, total=%d err=%v", total, err)
	}
}

func TestGetHistoryList_AttachesSingleFilmName(t *testing.T) {
	setupRetryRecordDB(t)
	if err := db.Mdb.AutoMigrate(&model.FilmIndex{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Mdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 42},
		FilmIndexContent:  model.FilmIndexContent{Name: "流浪地球"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	gone := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 7},
		FilmIndexContent:  model.FilmIndexContent{Name: "消失的她"},
	}
	if err := db.Mdb.Create(&gone).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Mdb.Delete(&gone).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, rec := range []model.CollectRecord{
		{
			BatchID:   "single-named",
			Trigger:   model.CollectTriggerSingleUpdate,
			Tag:       model.CollectTagSingleUpdate,
			FilmIDs:   "42,7",
			Status:    model.CollectRecordStatusSuccess,
			StartedAt: now,
		},
		{
			BatchID:   "batch-plain",
			Trigger:   model.CollectTriggerManual,
			Tag:       model.CollectTagBatchCollect,
			FilmIDs:   "42",
			Status:    model.CollectRecordStatusSuccess,
			StartedAt: now.Add(-time.Second),
		},
	} {
		if err := repository.CreateCollectRecord(&rec); err != nil {
			t.Fatal(err)
		}
	}

	list, total, err := CollectRecordSvc.GetHistoryList(model.CollectRecordQuery{Page: 1, PageSize: 10})
	if err != nil || total != 2 || len(list) != 2 {
		t.Fatalf("list total=%d len=%d err=%v", total, len(list), err)
	}
	byBatch := map[string]model.CollectRecord{}
	for _, rec := range list {
		byBatch[rec.BatchID] = rec
	}
	named := byBatch["single-named"]
	if named.FilmName != "流浪地球、消失的她" {
		t.Fatalf("single film name %q", named.FilmName)
	}
	if len(named.Films) != 2 || named.Films[0].Mid != 42 || named.Films[0].Name != "流浪地球" || named.Films[1].Mid != 7 || named.Films[1].Name != "消失的她" {
		t.Fatalf("films %+v", named.Films)
	}
	if byBatch["batch-plain"].FilmName != "" {
		t.Fatalf("batch film name %q", byBatch["batch-plain"].FilmName)
	}
}

func TestRetryBatchFailedSources_SingleFilmWithoutIDs(t *testing.T) {
	setupRetryRecordDB(t)
	if err := repository.CreateCollectRecord(&model.CollectRecord{
		BatchID:    "film-missing",
		Trigger:    model.CollectTriggerSingleUpdate,
		Status:     model.CollectRecordStatusFailed,
		StartedAt:  time.Now(),
		DetailJson: `[{"sourceId":"s1","status":"failed"}]`,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := CollectRecordSvc.RetryBatchFailedSources(model.CollectRecordRetryReq{BatchID: "film-missing"})
	if err == nil || !strings.Contains(err.Error(), "缺少影片编号") {
		t.Fatalf("expected missing film id, got %v", err)
	}
}
