package repository

import (
	"errors"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupRecordFilmsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testutil.MemDSN(t)
	testDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.Mdb = testDB
	if err := testDB.AutoMigrate(&model.CollectRecord{}, &model.CollectRecordFilmItem{}, &model.FilmIndex{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return testDB
}

func TestCollectRecordFilms_BatchSaveAndQuery(t *testing.T) {
	testDB := setupRecordFilmsTestDB(t)

	// 准备电影片名
	testDB.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101},
		FilmIndexContent:  model.FilmIndexContent{Name: "黑客帝国"},
	})
	testDB.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 102},
		FilmIndexContent:  model.FilmIndexContent{Name: "黑客帝国2"},
	})
	testDB.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 103},
		FilmIndexContent:  model.FilmIndexContent{Name: "盗梦空间"},
	})

	batchID := "batch_test_123"
	items := map[string][]int64{
		"src_1": {101, 102},
		"src_2": {102, 103},
	}
	if err := testDB.Create(&model.CollectRecord{
		BatchID:    batchID,
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusSuccess,
		FilmCount:  4,
		DetailJson: `[{"sourceId":"src_1","filmsTotal":2},{"sourceId":"src_2","filmsTotal":2}]`,
		StartedAt:  time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := BatchSaveCollectRecordFilms(batchID, items); err != nil {
		t.Fatalf("BatchSaveCollectRecordFilms failed: %v", err)
	}

	// 查 src_1
	q := model.CollectRecordFilmQuery{
		BatchID:  batchID,
		SourceID: "src_1",
		Page:     1,
		PageSize: 10,
	}
	list, total, err := GetCollectRecordFilms(q)
	if err != nil {
		t.Fatalf("GetCollectRecordFilms failed: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("expected total 2, got %d, len=%d", total, len(list))
	}
	if list[0].Name != "黑客帝国" || list[1].Name != "黑客帝国2" {
		t.Fatalf("unexpected names: %+v", list)
	}

	// 关键字筛选
	q.Keyword = "2"
	listFiltered, totalFiltered, err := GetCollectRecordFilms(q)
	if err != nil {
		t.Fatalf("filtered query failed: %v", err)
	}
	if totalFiltered != 1 || len(listFiltered) != 1 || listFiltered[0].Mid != 102 {
		t.Fatalf("unexpected filtered result: %+v", listFiltered)
	}

	// 同一批次重复结算不累加行数
	if err := BatchSaveCollectRecordFilms(batchID, items); err != nil {
		t.Fatalf("repeated save failed: %v", err)
	}
	var saved int64
	if err := testDB.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", batchID).Count(&saved).Error; err != nil {
		t.Fatalf("count saved rows failed: %v", err)
	}
	if saved != 4 {
		t.Fatalf("expected 4 rows after repeated save, got %d", saved)
	}

	// 全部站点的总数是各站清单行数之和，和分页列表一致。
	allQuery := model.CollectRecordFilmQuery{BatchID: batchID, Page: 1, PageSize: 10}
	all, allTotal, err := GetCollectRecordFilms(allQuery)
	if err != nil {
		t.Fatalf("all-source query failed: %v", err)
	}
	if allTotal != 4 || len(all) != 4 {
		t.Fatalf("expected total 4 and 4 rows, got total=%d list=%+v", allTotal, all)
	}
	named, namedTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, Keyword: "黑客", Page: 1, PageSize: 10,
	})
	if err != nil || namedTotal != 3 || len(named) != 3 {
		t.Fatalf("expected 3 keyword rows, got total=%d list=%+v err=%v", namedTotal, named, err)
	}

	// 清理
	if err := testDB.Where("batch_id = ?", batchID).Delete(&model.CollectRecordFilmItem{}).Error; err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	_, totalAfter, _ := GetCollectRecordFilms(q)
	if totalAfter != 0 {
		t.Fatalf("expected 0 after delete, got %d", totalAfter)
	}
}

func TestGetCollectRecordFilms_NameLookupError(t *testing.T) {
	testDB := setupRecordFilmsTestDB(t)
	if err := testDB.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101},
		FilmIndexContent:  model.FilmIndexContent{Name: "黑客帝国"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := BatchSaveCollectRecordFilms("batch-names", map[string][]int64{"src_1": {101}}); err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(&model.CollectRecord{
		BatchID:    "batch-names",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusSuccess,
		FilmCount:  1,
		DetailJson: `[{"sourceId":"src_1","filmsTotal":1}]`,
		StartedAt:  time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Migrator().DropTable(&model.FilmIndex{}); err != nil {
		t.Fatal(err)
	}
	_, _, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "batch-names", SourceID: "src_1", Page: 1, PageSize: 10,
	})
	if err == nil {
		t.Fatal("expected name lookup error")
	}
}

func TestBatchSaveCollectRecordFilms_ContinuesAfterChunkError(t *testing.T) {
	testDB := setupRecordFilmsTestDB(t)
	prevSize := collectFilmChunkSize
	prevInsert := insertCollectFilmChunk
	collectFilmChunkSize = 2
	calls := 0
	insertCollectFilmChunk = func(chunk []model.CollectRecordFilmItem) error {
		calls++
		if calls == 2 {
			return errors.New("lock")
		}
		return prevInsert(chunk)
	}
	t.Cleanup(func() {
		collectFilmChunkSize = prevSize
		insertCollectFilmChunk = prevInsert
	})

	err := BatchSaveCollectRecordFilms("batch-chunk", map[string][]int64{
		"s1": {1, 2, 3, 4, 5},
	})
	missed := UnstoredCollectFilms(err)
	if err == nil || len(missed["s1"]) != 2 || missed["s1"][0] != 3 || missed["s1"][1] != 4 {
		t.Fatalf("err=%v missed=%v calls=%d", err, missed, calls)
	}
	var saved []int64
	if err := testDB.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", "batch-chunk").Order("mid ASC").Pluck("mid", &saved).Error; err != nil {
		t.Fatal(err)
	}
	if len(saved) != 3 || saved[0] != 1 || saved[1] != 2 || saved[2] != 5 {
		t.Fatalf("saved=%v", saved)
	}
}

func TestGetCollectRecordFilms_RunningBatchWithoutDetailJson(t *testing.T) {
	testDB := setupRecordFilmsTestDB(t)
	batchID := "batch_running_test"
	testDB.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 201},
		FilmIndexContent:  model.FilmIndexContent{Name: "运行中测试影片"},
	})
	if err := BatchSaveCollectRecordFilms(batchID, map[string][]int64{"src_run": {201}}); err != nil {
		t.Fatal(err)
	}
	// 清单行数写在记录上。没有站点明细时，单站总数为 0。
	if err := testDB.Create(&model.CollectRecord{
		BatchID:    batchID,
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusRunning,
		FilmCount:  1,
		DetailJson: "",
		StartedAt:  time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	list, total, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, Page: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Mid != 201 {
		t.Fatalf("expected total 1 and mid 201, got total=%d list=%+v", total, list)
	}
	if _, totalSrc, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, SourceID: "src_run", Page: 1, PageSize: 10,
	}); err != nil || totalSrc != 0 {
		t.Fatalf("source without detail total=%d err=%v", totalSrc, err)
	}

	if err := testDB.Model(&model.CollectRecord{}).Where("batch_id = ?", batchID).
		Update("detail_json", `[{"sourceId":"src_run","filmsTotal":1}]`).Error; err != nil {
		t.Fatal(err)
	}
	listSrc, totalSrc, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, SourceID: "src_run", Page: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("query src failed: %v", err)
	}
	if totalSrc != 1 || len(listSrc) != 1 || listSrc[0].Mid != 201 {
		t.Fatalf("expected total 1 and mid 201, got total=%d list=%+v", totalSrc, listSrc)
	}
}

func TestGetCollectRecordFilms_AllSitesTotalCountsRowsNotFilmCount(t *testing.T) {
	testDB := setupRecordFilmsTestDB(t)
	batchID := "batch_rows"
	for _, item := range []struct {
		mid  int64
		name string
	}{
		{201, "甲"},
		{202, "乙"},
	} {
		testDB.Create(&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: item.mid},
			FilmIndexContent:  model.FilmIndexContent{Name: item.name},
		})
	}
	if err := BatchSaveCollectRecordFilms(batchID, map[string][]int64{
		"src_1": {201, 202},
		"src_2": {202},
	}); err != nil {
		t.Fatal(err)
	}
	// 同一影片在两站各算一行。总数用记录上的清单行数，单站用明细行数。
	if err := testDB.Create(&model.CollectRecord{
		BatchID:    batchID,
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusRunning,
		FilmCount:  3,
		DetailJson: `[{"sourceId":"src_1","filmsTotal":2},{"sourceId":"src_2","filmsTotal":1}]`,
		StartedAt:  time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	all := model.CollectRecordFilmQuery{BatchID: batchID, Page: 1, PageSize: 10}
	list, total, err := GetCollectRecordFilms(all)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("all total=%d list=%d", total, len(list))
	}
	if _, total, err = GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, SourceID: "src_1", Page: 1, PageSize: 10,
	}); err != nil || total != 2 {
		t.Fatalf("src_1 total=%d err=%v", total, err)
	}
	if _, total, err = GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, SourceID: "src_2", Page: 1, PageSize: 10,
	}); err != nil || total != 1 {
		t.Fatalf("src_2 total=%d err=%v", total, err)
	}
}
