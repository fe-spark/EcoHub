package ledger

import (
	"fmt"
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
	prev := db.Mdb
	db.Mdb = testDB
	t.Cleanup(func() { db.Mdb = prev })
	if err := testDB.AutoMigrate(&model.CollectRecord{}, &model.CollectRecordFilmItem{}, &model.FilmIndex{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return testDB
}

func TestCollectRecordFilms_SaveBrowseAndSearchPagination(t *testing.T) {
	testDB := setupRecordFilmsTestDB(t)

	for _, item := range []struct {
		mid  int64
		name string
	}{
		{101, "黑客帝国"},
		{102, "黑客帝国2"},
		{103, "盗梦空间"},
	} {
		testDB.Create(&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: item.mid},
			FilmIndexContent:  model.FilmIndexContent{Name: item.name},
		})
	}

	batchID := "batch_test_123"
	rec := &model.CollectRecord{
		BatchID:    batchID,
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusSuccess,
		Ledger:     true,
		FilmCount:  4,
		DetailJson: `[{"sourceId":"src_1","filmsTotal":2},{"sourceId":"src_2","filmsTotal":2}]`,
		StartedAt:  time.Now(),
	}
	if err := testDB.Create(rec).Error; err != nil {
		t.Fatal(err)
	}

	items := map[string][]int64{
		"src_1": {101, 102},
		"src_2": {102, 103},
	}
	if err := SaveCollectRecordFilms(rec.ID, items); err != nil {
		t.Fatalf("SaveCollectRecordFilms failed: %v", err)
	}
	// 重复写入不累加行数。
	if err := SaveCollectRecordFilms(rec.ID, items); err != nil {
		t.Fatalf("repeated save failed: %v", err)
	}
	var saved int64
	testDB.Model(&model.CollectRecordFilmItem{}).Where("record_id = ?", rec.ID).Count(&saved)
	if saved != 4 {
		t.Fatalf("expected 4 rows after repeated save, got %d", saved)
	}

	// 单站浏览分页。
	srcPage1, err := QueryCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, SourceID: "src_1", PageSize: 1,
	})
	if err != nil || srcPage1.Total != 2 || !srcPage1.HasMore || len(srcPage1.List) != 1 || srcPage1.List[0].Name != "黑客帝国" {
		t.Fatalf("src_1 page1 = %+v err=%v", srcPage1, err)
	}
	srcPage2, err := QueryCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, SourceID: "src_1", AfterMid: srcPage1.NextAfterMid, PageSize: 1,
	})
	if err != nil || srcPage2.Total != 2 || srcPage2.HasMore || len(srcPage2.List) != 1 || srcPage2.List[0].Name != "黑客帝国2" {
		t.Fatalf("src_1 page2 = %+v err=%v", srcPage2, err)
	}

	// 全站浏览游标翻页（覆盖跨站边界）。
	var allGot []string
	var afterSrc string
	var afterMid int64
	for {
		p, err := QueryCollectRecordFilms(model.CollectRecordFilmQuery{
			BatchID: batchID, AfterSource: afterSrc, AfterMid: afterMid, PageSize: 2,
		})
		if err != nil {
			t.Fatalf("all browse failed: %v", err)
		}
		if p.Total != 4 {
			t.Fatalf("all total=%d, want 4", p.Total)
		}
		for _, f := range p.List {
			allGot = append(allGot, fmt.Sprintf("%s:%d", f.SourceID, f.Mid))
		}
		if !p.HasMore {
			break
		}
		afterSrc = p.NextAfterSource
		afterMid = p.NextAfterMid
	}
	if fmt.Sprint(allGot) != "[src_1:101 src_1:102 src_2:102 src_2:103]" {
		t.Fatalf("all browse sequence = %v", allGot)
	}

	// 搜索态：多站同片（mid=102 同属 src_1 与 src_2）按 (name, mid, source_id) 逐页遍历，不漏不重且不做精确 COUNT。
	var searchGot []string
	var sName, sSrc string
	var sMid int64
	for {
		p, err := QueryCollectRecordFilms(model.CollectRecordFilmQuery{
			BatchID: batchID, Keyword: "黑客", AfterName: sName, AfterMid: sMid, AfterSource: sSrc, PageSize: 1,
		})
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		if p.Total != -1 || p.Truncated {
			t.Fatalf("search page unexpected meta: %+v", p)
		}
		for _, f := range p.List {
			searchGot = append(searchGot, fmt.Sprintf("%s:%s:%d", f.Name, f.SourceID, f.Mid))
		}
		if !p.HasMore {
			break
		}
		sName, sMid, sSrc = p.NextAfterName, p.NextAfterMid, p.NextAfterSource
	}
	if fmt.Sprint(searchGot) != "[黑客帝国:src_1:101 黑客帝国2:src_1:102 黑客帝国2:src_2:102]" {
		t.Fatalf("search sequence = %v", searchGot)
	}

	// 分批清理账本。
	prevChunk := collectCleanFilmChunk
	collectCleanFilmChunk = 2
	t.Cleanup(func() { collectCleanFilmChunk = prevChunk })
	if err := DeleteCollectRecordFilms(rec.ID); err != nil {
		t.Fatalf("DeleteCollectRecordFilms failed: %v", err)
	}
	testDB.Model(&model.CollectRecordFilmItem{}).Where("record_id = ?", rec.ID).Count(&saved)
	if saved != 0 {
		t.Fatalf("expected 0 after chunked delete, got %d", saved)
	}
}

func TestSearchCollectRecordFilms_ScanBudgetTruncation(t *testing.T) {
	testDB := setupRecordFilmsTestDB(t)
	prevChunk, prevBudget := searchFilmScanChunk, maxSearchFilmScan
	searchFilmScanChunk = 3
	maxSearchFilmScan = 5
	t.Cleanup(func() {
		searchFilmScanChunk = prevChunk
		maxSearchFilmScan = prevBudget
	})

	// 插入 10 部以 "测试" 开头的影片，但本批次只包含第 9 部（超出 5 行扫描预算）。
	for i := 1; i <= 10; i++ {
		testDB.Create(&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: int64(i)},
			FilmIndexContent:  model.FilmIndexContent{Name: fmt.Sprintf("测试%02d", i)},
		})
	}
	rec := &model.CollectRecord{
		BatchID:    "batch_trunc",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusSuccess,
		Ledger:     true,
		FilmCount:  1,
		DetailJson: `[{"sourceId":"src_1","filmsTotal":1}]`,
		StartedAt:  time.Now(),
	}
	if err := testDB.Create(rec).Error; err != nil {
		t.Fatal(err)
	}
	if err := SaveCollectRecordFilms(rec.ID, map[string][]int64{"src_1": {9}}); err != nil {
		t.Fatal(err)
	}

	page, err := QueryCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "batch_trunc", SourceID: "src_1", Keyword: "测试", PageSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !page.Truncated || len(page.List) != 0 || page.Total != -1 {
		t.Fatalf("expected truncated empty page, got %+v", page)
	}
}

func TestSearchCollectRecordFilms_TrailingSpaceAndRoundBudget(t *testing.T) {
	testDB := setupRecordFilmsTestDB(t)
	testDB.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 301},
		FilmIndexContent:  model.FilmIndexContent{Name: "黑客帝国 "}, // 含尾部空格
	})
	testDB.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 302},
		FilmIndexContent:  model.FilmIndexContent{Name: "黑客帝国2"},
	})
	rec := &model.CollectRecord{
		BatchID:    "batch_space",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusSuccess,
		Ledger:     true,
		FilmCount:  3,
		DetailJson: `[{"sourceId":"src_1","filmsTotal":2},{"sourceId":"src_2","filmsTotal":1}]`,
		StartedAt:  time.Now(),
	}
	if err := testDB.Create(rec).Error; err != nil {
		t.Fatal(err)
	}
	if err := SaveCollectRecordFilms(rec.ID, map[string][]int64{
		"src_1": {301, 302},
		"src_2": {301},
	}); err != nil {
		t.Fatal(err)
	}

	var got []string
	var sName, sSrc string
	var sMid int64
	for step := 0; step < 10; step++ {
		p, err := QueryCollectRecordFilms(model.CollectRecordFilmQuery{
			BatchID: "batch_space", Keyword: "黑客", AfterName: sName, AfterMid: sMid, AfterSource: sSrc, PageSize: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range p.List {
			got = append(got, fmt.Sprintf("%s:%s:%d", f.Name, f.SourceID, f.Mid))
		}
		if !p.HasMore {
			break
		}
		sName, sMid, sSrc = p.NextAfterName, p.NextAfterMid, p.NextAfterSource
	}
	if fmt.Sprint(got) != "[黑客帝国:src_1:301 黑客帝国:src_2:301 黑客帝国2:src_1:302]" {
		t.Fatalf("trailing-space pagination got %v", got)
	}

	// 验证轮次上限截断。
	prevChunk, prevRounds, prevScan := searchFilmScanChunk, maxSearchFilmRounds, maxSearchFilmScan
	searchFilmScanChunk, maxSearchFilmRounds, maxSearchFilmScan = 1, 1, 100
	t.Cleanup(func() {
		searchFilmScanChunk, maxSearchFilmRounds, maxSearchFilmScan = prevChunk, prevRounds, prevScan
	})
	truncPage, err := QueryCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "batch_space", SourceID: "src_1", Keyword: "黑客", PageSize: 5,
	})
	if err != nil || !truncPage.Truncated || len(truncPage.List) != 1 {
		t.Fatalf("expected round-budget truncation with 1 hit, got %+v err=%v", truncPage, err)
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
	rec := &model.CollectRecord{
		BatchID:    "batch-names",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusSuccess,
		Ledger:     true,
		FilmCount:  1,
		DetailJson: `[{"sourceId":"src_1","filmsTotal":1}]`,
		StartedAt:  time.Now(),
	}
	if err := testDB.Create(rec).Error; err != nil {
		t.Fatal(err)
	}
	if err := SaveCollectRecordFilms(rec.ID, map[string][]int64{"src_1": {101}}); err != nil {
		t.Fatal(err)
	}
	if err := testDB.Migrator().DropTable(&model.FilmIndex{}); err != nil {
		t.Fatal(err)
	}
	if _, err := QueryCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: "batch-names", SourceID: "src_1", PageSize: 10,
	}); err == nil {
		t.Fatal("expected name lookup error")
	}
}

func TestQueryCollectRecordFilms_RunningBatchWithoutDetailJson(t *testing.T) {
	testDB := setupRecordFilmsTestDB(t)
	batchID := "batch_running_test"
	testDB.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 201},
		FilmIndexContent:  model.FilmIndexContent{Name: "运行中测试影片"},
	})
	rec := &model.CollectRecord{
		BatchID:    batchID,
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusRunning,
		Ledger:     true,
		FilmCount:  1,
		DetailJson: "",
		StartedAt:  time.Now(),
	}
	if err := testDB.Create(rec).Error; err != nil {
		t.Fatal(err)
	}
	if err := SaveCollectRecordFilms(rec.ID, map[string][]int64{"src_run": {201}}); err != nil {
		t.Fatal(err)
	}

	allPage, err := QueryCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, PageSize: 10,
	})
	if err != nil || allPage.Total != 1 || len(allPage.List) != 1 || allPage.List[0].Mid != 201 {
		t.Fatalf("expected total 1 and mid 201, got %+v err=%v", allPage, err)
	}
	if srcPage, err := QueryCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, SourceID: "src_run", PageSize: 10,
	}); err != nil || srcPage.Total != 0 {
		t.Fatalf("source without detail total=%d err=%v", srcPage.Total, err)
	}

	if err := testDB.Model(&model.CollectRecord{}).Where("id = ?", rec.ID).
		Update("detail_json", `[{"sourceId":"src_run","filmsTotal":1}]`).Error; err != nil {
		t.Fatal(err)
	}
	srcPage, err := QueryCollectRecordFilms(model.CollectRecordFilmQuery{
		BatchID: batchID, SourceID: "src_run", PageSize: 10,
	})
	if err != nil || srcPage.Total != 1 || len(srcPage.List) != 1 || srcPage.List[0].Mid != 201 {
		t.Fatalf("expected total 1 and mid 201, got %+v err=%v", srcPage, err)
	}
}
