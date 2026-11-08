package service

import (
	"fmt"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/repository/film"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
)

func TestNormalizeDailyUpdateReqDefaults(t *testing.T) {
	got := normalizeDailyUpdateReq(DailyUpdateListReq{})
	if got.Page == nil {
		t.Fatal("page should not be nil")
	}
	if got.Page.Current != 1 {
		t.Fatalf("current: want 1 got %d", got.Page.Current)
	}
	if got.Page.PageSize != dailyUpdateDefaultPageSize {
		t.Fatalf("pageSize: want %d got %d", dailyUpdateDefaultPageSize, got.Page.PageSize)
	}
	if got.Exclude != nil {
		t.Fatalf("exclude should be cleared when not random, got %v", got.Exclude)
	}
}

func TestNormalizeDailyUpdateReqClampAndRandomExclude(t *testing.T) {
	got := normalizeDailyUpdateReq(DailyUpdateListReq{
		Page:    &dto.Page{Current: 0, PageSize: 500},
		Random:  true,
		Exclude: []int64{1, 2},
	})
	if got.Page.Current != 1 {
		t.Fatalf("current: want 1 got %d", got.Page.Current)
	}
	if got.Page.PageSize != dailyUpdateMaxPageSize {
		t.Fatalf("pageSize: want %d got %d", dailyUpdateMaxPageSize, got.Page.PageSize)
	}
	if len(got.Exclude) != 2 {
		t.Fatalf("random should keep exclude, got %v", got.Exclude)
	}

	over := make([]int64, dailyUpdateMaxExclude+10)
	for i := range over {
		over[i] = int64(i + 1)
	}
	got = normalizeDailyUpdateReq(DailyUpdateListReq{Random: true, Exclude: over})
	if len(got.Exclude) != dailyUpdateMaxExclude {
		t.Fatalf("exclude cap: want %d got %d", dailyUpdateMaxExclude, len(got.Exclude))
	}
}

func TestFillDailyUpdatePage(t *testing.T) {
	page := fillDailyUpdatePage(&dto.Page{Current: 2, PageSize: 21}, 50)
	if page.Total != 50 {
		t.Fatalf("total: want 50 got %d", page.Total)
	}
	if page.PageCount != 3 {
		t.Fatalf("pageCount: want 3 got %d", page.PageCount)
	}
	page = fillDailyUpdatePage(&dto.Page{Current: 1, PageSize: 21}, 0)
	if page.PageCount != 1 {
		t.Fatalf("empty pageCount: want 1 got %d", page.PageCount)
	}
}

func TestAssembleDailyUpdateCategories(t *testing.T) {
	nav := []model.Category{
		{Id: 1, Name: "电影"},
		{Id: 2, Name: "剧集"},
		{Id: 3, Name: "动漫"},
	}
	countByPid := map[int64]int{1: 10, 3: 4}
	got := AssembleDailyUpdateCategories(nav, countByPid, 3, 17)
	if len(got) != 4 {
		t.Fatalf("want 4 items (全部+电影+动漫+其他), got %+v", got)
	}
	if got[0].Pid != film.DailyPidAll || got[0].Name != "全部" || got[0].Count != 17 {
		t.Fatalf("all: %+v", got[0])
	}
	if got[1].Pid != 1 || got[1].Count != 10 {
		t.Fatalf("movie: %+v", got[1])
	}
	if got[2].Pid != 3 || got[2].Name != "动漫" {
		t.Fatalf("anime: %+v", got[2])
	}
	if got[3].Pid != film.DailyPidOther || got[3].Count != 3 {
		t.Fatalf("other: %+v", got[3])
	}
}

func TestAssembleDailyUpdateCategoriesEmpty(t *testing.T) {
	got := AssembleDailyUpdateCategories(nil, nil, 0, 0)
	if len(got) != 1 || got[0].Pid != 0 || got[0].Count != 0 {
		t.Fatalf("want only 全部 0, got %+v", got)
	}
}

func TestDailyUpdates_CacheBehavior(t *testing.T) {
	gdb, mr := setupTestDBAndRedis(t)
	_ = mr

	now := time.Now()
	version := "v_test_daily"
	// 插入测试影片索引与快照
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1001},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 11},
		FilmIndexContent:  model.FilmIndexContent{Name: "测试电影1", UpdateStamp: now.Unix()},
	})
	_ = filmsnapshot.SetActiveSnapshotVersion(version)
	gdb.Create(&model.Category{Id: 1, Name: "电影", Show: true})

	req := DailyUpdateListReq{
		Pid:    0,
		Page:   &dto.Page{Current: 1, PageSize: 21},
		Random: false,
	}

	// 1. 首次调用
	res1, err := IndexSvc.DailyUpdates(req)
	if err != nil {
		t.Fatalf("first DailyUpdates call failed: %v", err)
	}
	if len(res1.Categories) == 0 {
		t.Fatalf("expected categories not empty")
	}
	if res1.Page.Total != 1 {
		t.Fatalf("want total 1, got %d", res1.Page.Total)
	}
	catKey := config.DailyUpdatesCatCacheKey
	if mr.Exists(catKey) {
		t.Fatalf("category cache %s must not be written", catKey)
	}
	pageKey := fmt.Sprintf("%s:p0:c1:s21", config.DailyUpdatesCachePrefix)
	if mr.Exists(pageKey) {
		t.Fatalf("page cache %s must not be written", pageKey)
	}

	res2, err := IndexSvc.DailyUpdates(req)
	if err != nil {
		t.Fatalf("second DailyUpdates call failed: %v", err)
	}
	if res2.Page.Total != res1.Page.Total {
		t.Fatalf("want total %d, got %d", res1.Page.Total, res2.Page.Total)
	}
}

func TestDailyUpdates_PrimarySourceCategoryIsolation(t *testing.T) {
	gdb, _ := setupTestDBAndRedis(t)
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.SourceCategory{}, &model.FilmSourcePlaylist{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now()
	_ = filmsnapshot.SetActiveSnapshotVersion("live")

	gdb.Create(&model.FilmSource{Id: "src_a", Name: "A站", State: true, Sort: 1})
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "B站", State: true, Sort: 2})
	gdb.Create(&model.SourceCategory{SourceId: "src_a", SourceTypeId: 10, ParentSourceTypeId: 0, RawName: "电影", Sort: 1})
	gdb.Create(&model.SourceCategory{SourceId: "src_b", SourceTypeId: 20, ParentSourceTypeId: 0, RawName: "连续剧", Sort: 1})

	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2001, FirstSourceId: "src_a"},
		FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src_a:10", RootCategoryKey: "source:src_a:10"},
		FilmIndexContent:  model.FilmIndexContent{Name: "A站电影", UpdateStamp: now.Unix()},
	})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 2001, SourceId: "src_a", LineKind: "play", GroupIndex: 0})

	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2002, FirstSourceId: "src_b"},
		FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src_b:20", RootCategoryKey: "source:src_b:20"},
		FilmIndexContent:  model.FilmIndexContent{Name: "B站剧集", UpdateStamp: now.Unix()},
	})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 2002, SourceId: "src_b", LineKind: "play", GroupIndex: 0})

	resA, err := IndexSvc.DailyUpdates(DailyUpdateListReq{Pid: 10, SourceId: "src_a"})
	if err != nil {
		t.Fatalf("src_a query failed: %v", err)
	}
	if resA.Page.Total != 1 || len(resA.Categories) != 2 || resA.Categories[1].Pid != 10 {
		t.Fatalf("src_a unexpected result: total=%d cats=%+v", resA.Page.Total, resA.Categories)
	}

	resB, err := IndexSvc.DailyUpdates(DailyUpdateListReq{Pid: 20, SourceId: "src_b"})
	if err != nil {
		t.Fatalf("src_b query failed: %v", err)
	}
	if resB.Page.Total != 1 || len(resB.Categories) != 2 || resB.Categories[1].Pid != 20 {
		t.Fatalf("src_b unexpected result: total=%d cats=%+v", resB.Page.Total, resB.Categories)
	}
}

func TestDailyUpdates_HiddenSourceCategorySkipped(t *testing.T) {
	gdb, _ := setupTestDBAndRedis(t)
	prev := support.CurrentMappingSnapshotForTest()
	t.Cleanup(func() {
		support.StoreMappingSnapshotForTest(prev)
	})

	now := time.Now()
	_ = filmsnapshot.SetActiveSnapshotVersion("live")

	if err := gdb.Create(&model.FilmSource{Id: "src_daily_hide", Name: "爱旦", State: true, Sort: 1}).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := gdb.Create([]model.Category{
		{Id: 1, Pid: 0, Name: "电影", Show: true, StableKey: "src_daily_hide:1"},
		{Id: 17, Pid: 0, Name: "公告", Show: true, StableKey: "src_daily_hide:17"},
	}).Error; err != nil {
		t.Fatalf("create categories: %v", err)
	}
	if err := gdb.Model(&model.Category{}).Where("id = ?", 17).Update("show", false).Error; err != nil {
		t.Fatalf("hide 公告: %v", err)
	}
	if err := gdb.Create([]model.CategoryMapping{
		{SourceId: "src_daily_hide", SourceTypeId: 1, CategoryId: 1},
		{SourceId: "src_daily_hide", SourceTypeId: 17, CategoryId: 17},
	}).Error; err != nil {
		t.Fatalf("create mappings: %v", err)
	}
	if err := gdb.Create([]model.SourceCategory{
		{SourceId: "src_daily_hide", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0},
		{SourceId: "src_daily_hide", SourceTypeId: 17, ParentSourceTypeId: 0, RawName: "公告", Sort: 2, Depth: 0},
	}).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}
	repository.ReloadMappingRules()

	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3001, FirstSourceId: "src_daily_hide"},
		FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src_daily_hide:1", RootCategoryKey: "source:src_daily_hide:1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "可见电影", UpdateStamp: now.Unix()},
	}).Error; err != nil {
		t.Fatalf("create visible film: %v", err)
	}
	if err := gdb.Create(&model.FilmSourcePlaylist{Mid: 3001, SourceId: "src_daily_hide", LineKind: "play", GroupIndex: 0}).Error; err != nil {
		t.Fatalf("create visible playlist: %v", err)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3002, FirstSourceId: "src_daily_hide"},
		FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src_daily_hide:17", RootCategoryKey: "source:src_daily_hide:17"},
		FilmIndexContent:  model.FilmIndexContent{Name: "隐藏公告片", UpdateStamp: now.Unix()},
	}).Error; err != nil {
		t.Fatalf("create hidden film: %v", err)
	}
	if err := gdb.Create(&model.FilmSourcePlaylist{Mid: 3002, SourceId: "src_daily_hide", LineKind: "play", GroupIndex: 0}).Error; err != nil {
		t.Fatalf("create hidden playlist: %v", err)
	}

	all, err := IndexSvc.DailyUpdates(DailyUpdateListReq{Pid: 0, SourceId: "src_daily_hide"})
	if err != nil {
		t.Fatalf("pid=0: %v", err)
	}
	if all.Page.Total != 1 {
		t.Fatalf("全部 total=%d, want 1 (hidden skipped)", all.Page.Total)
	}
	if len(all.Categories) != 2 || all.Categories[0].Count != 1 || all.Categories[1].Pid != 1 {
		t.Fatalf("categories=%+v, want 全部=1 and 电影", all.Categories)
	}
	for _, cat := range all.Categories {
		if cat.Pid == film.DailyPidOther {
			t.Fatalf("hidden film leaked into 其他: %+v", all.Categories)
		}
	}

	other, err := IndexSvc.DailyUpdates(DailyUpdateListReq{Pid: film.DailyPidOther, SourceId: "src_daily_hide"})
	if err != nil {
		t.Fatalf("pid=-1: %v", err)
	}
	if other.Page.Total != 0 {
		t.Fatalf("其他 total=%d, want 0", other.Page.Total)
	}

	hiddenPid, err := IndexSvc.DailyUpdates(DailyUpdateListReq{Pid: 17, SourceId: "src_daily_hide"})
	if err != nil {
		t.Fatalf("pid=17: %v", err)
	}
	if hiddenPid.Page.Total != 0 {
		t.Fatalf("停用分类 pid=17 total=%d, want 0", hiddenPid.Page.Total)
	}
}
