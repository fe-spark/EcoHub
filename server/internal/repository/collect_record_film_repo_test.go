package repository

import (
	"fmt"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupRecordFilmsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	testDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.Mdb = testDB
	if err := testDB.AutoMigrate(&model.CollectRecordFilmItem{}, &model.FilmIndex{}); err != nil {
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

	// 全部站点按 mid 去重，同一影片跨站只出现一次。
	allQuery := model.CollectRecordFilmQuery{BatchID: batchID, Page: 1, PageSize: 10}
	all, allTotal, err := GetCollectRecordFilms(allQuery)
	if err != nil {
		t.Fatalf("all-source query failed: %v", err)
	}
	if allTotal != 3 || len(all) != 3 || all[0].Mid != 101 || all[1].Mid != 102 || all[2].Mid != 103 {
		t.Fatalf("expected distinct mids 101,102,103, got total=%d list=%+v", allTotal, all)
	}
	for _, film := range all {
		if film.SourceID != "" {
			t.Fatalf("all-source row should not carry source, got %+v", film)
		}
	}
	named, namedTotal, err := GetCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, Keyword: "黑客", Page: 1, PageSize: 10,
	})
	if err != nil || namedTotal != 2 || len(named) != 2 || named[0].Mid != 101 || named[1].Mid != 102 {
		t.Fatalf("expected keyword distinct 101,102, got total=%d list=%+v err=%v", namedTotal, named, err)
	}

	// 清理
	if err := DeleteCollectRecordFilmsByBatchIDs([]string{batchID}); err != nil {
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

func TestCleanOldCollectRecordFilms(t *testing.T) {
	testDB := setupRecordFilmsTestDB(t)
	oldTime := time.Now().AddDate(0, 0, -40)
	testDB.Create(&model.CollectRecordFilmItem{
		BatchID:   "old_batch",
		SourceID:  "src_1",
		Mid:       999,
		CreatedAt: oldTime,
	})
	testDB.Create(&model.CollectRecordFilmItem{
		BatchID:   "new_batch",
		SourceID:  "src_1",
		Mid:       888,
		CreatedAt: time.Now(),
	})

	if err := CleanOldCollectRecordFilms(30); err != nil {
		t.Fatalf("clean failed: %v", err)
	}
	var count int64
	testDB.Model(&model.CollectRecordFilmItem{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 remaining, got %d", count)
	}
}
