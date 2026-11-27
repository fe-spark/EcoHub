package recommend

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"server/internal/film/catalog"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupRecommendTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:rec_svc_%d_%d?mode=memory&cache=shared", time.Now().UnixNano(), rand.Intn(100000))
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.PrimaryState{},
		&model.Category{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
		&model.MappingRule{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.FilmTMDBScrape{},
		&model.HomeRecommendGroup{},
		&model.HomeRecommendItem{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	origMdb, origRdb := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	filmsnapshot.ClearActiveFilmReadModel()
	t.Cleanup(func() {
		db.Mdb, db.Rdb = origMdb, origRdb
		filmsnapshot.ClearActiveFilmReadModel()
		catalog.InvalidatePublicSourceCache()
		taxonomy.RefreshCategoryCache()
		sourcecatalog.InvalidateCollectSourceCatalog()
	})

	now := time.Now()
	if err := gdb.Create(&[]model.FilmSource{
		{Id: "src1", Name: "主站", Uri: "http://src1.test", State: true, Sort: 1, CreatedAt: now},
		{Id: "src2", Name: "副站", Uri: "http://src2.test", State: true, Sort: 2, CreatedAt: now},
	}).Error; err != nil {
		t.Fatalf("seed sources: %v", err)
	}
	_ = gdb.Create(&model.PrimaryState{Id: 1, PrimarySourceId: "src1"}).Error
	_ = gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", StableKey: "rec-movie", Show: true, Sort: 1}).Error
	_ = gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 1, RawName: "电影", Sort: 1}).Error
	_ = gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 1, CategoryId: 1}).Error
	taxonomy.RefreshCategoryCache()
	_ = sourcecatalog.RefreshCollectSourceCatalog()
	return gdb
}

func seedRecommendFilm(t *testing.T, gdb *gorm.DB, mid int64, sourceID, name string, hits int64) {
	t.Helper()
	film := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid, FirstSourceId: sourceID},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid: 1, Cid: 1, CName: "电影",
			RootCategoryKey: taxonomy.BuildSourceCategoryKey(sourceID, 1),
			CategoryKey:     taxonomy.BuildSourceCategoryKey(sourceID, 1),
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: name, Year: 2026, Score: 8.5, Hits: hits, UpdateStamp: hits,
			Picture: fmt.Sprintf("http://img/%d.jpg", mid),
		},
	}
	if err := gdb.Create(&film).Error; err != nil {
		t.Fatalf("seed film: %v", err)
	}
	if err := gdb.Create(&model.FilmSourcePlaylist{
		Mid: mid, SourceId: sourceID, LineKind: model.LineKindPlay, GroupIndex: 0, GroupName: "默认",
	}).Error; err != nil {
		t.Fatalf("seed line: %v", err)
	}
}

func TestHomeRecommendService_Lifecycle(t *testing.T) {
	gdb := setupRecommendTestDB(t)
	seedRecommendFilm(t, gdb, 1001, "src1", "电影甲", 100)
	seedRecommendFilm(t, gdb, 1002, "src1", "电影乙", 90)
	seedRecommendFilm(t, gdb, 2001, "src2", "副站影片", 80)

	ctx := context.Background()

	// 1. 首次读取自动一次性导入当前站点可见一级分类；手动删除后不应复生
	listRes, err := HomeRecommendSvc.ListAdmin(ctx, "src1")
	if err != nil {
		t.Fatalf("ListAdmin failed: %v", err)
	}
	if len(listRes.Groups) != 1 || listRes.Groups[0].CategoryId != 1 {
		t.Fatalf("expected default category group auto-imported initially, got %+v", listRes.Groups)
	}
	if err := HomeRecommendSvc.DeleteGroup(ctx, listRes.Groups[0].Id); err != nil {
		t.Fatalf("DeleteGroup default category failed: %v", err)
	}
	afterDelRes, err := HomeRecommendSvc.ListAdmin(ctx, "src1")
	if err != nil || len(afterDelRes.Groups) != 0 {
		t.Fatalf("expected deleted default group not re-imported, got err=%v groups=%+v", err, afterDelRes.Groups)
	}

	// 2. 创建两个自定义分组并加片（跨站影片应被拦截）
	g1, err := HomeRecommendSvc.CreateGroup(ctx, RecommendGroupInput{
		SourceId: "src1",
		Title:    "本周精选",
	})
	if err != nil {
		t.Fatalf("CreateGroup g1 failed: %v", err)
	}
	g2, err := HomeRecommendSvc.CreateGroup(ctx, RecommendGroupInput{
		SourceId: "src1",
		Title:    "高分佳片",
	})
	if err != nil {
		t.Fatalf("CreateGroup g2 failed: %v", err)
	}
	if _, err := HomeRecommendSvc.AddItem(ctx, g1.Id, 2001, ""); err == nil {
		t.Fatalf("expected cross-source AddItem to fail")
	}
	item1, err := HomeRecommendSvc.AddItem(ctx, g1.Id, 1001, "8.9")
	if err != nil {
		t.Fatalf("AddItem 1001 failed: %v", err)
	}
	if _, err := HomeRecommendSvc.AddItem(ctx, g1.Id, 1002, "8.1"); err != nil {
		t.Fatalf("AddItem 1002 failed: %v", err)
	}
	if _, err := HomeRecommendSvc.AddItem(ctx, g2.Id, 1002, "8.5"); err != nil {
		t.Fatalf("AddItem g2 failed: %v", err)
	}

	// 3. 重排分组顺序，验证 HomeSections 按顺序输出
	if err := HomeRecommendSvc.ReorderGroups(ctx, "src1", []string{g2.Id, g1.Id}); err != nil {
		t.Fatalf("ReorderGroups failed: %v", err)
	}
	sections, ok := HomeRecommendSvc.HomeSections("src1")
	if !ok || len(sections) != 2 || sections[0].Id != g2.Id || sections[1].Id != g1.Id {
		t.Fatalf("unexpected sections order: ok=%v %+v", ok, sections)
	}

	// 4. 删除条目应递增首页缓存世代
	genBefore := repobase.IndexPageGeneration()
	if err := HomeRecommendSvc.DeleteItem(ctx, item1.Id); err != nil {
		t.Fatalf("DeleteItem failed: %v", err)
	}
	if repobase.IndexPageGeneration() <= genBefore {
		t.Fatalf("expected IndexPageGeneration bumped after DeleteItem")
	}

	// 5. 一键排片预览与发布
	preview, err := HomeRecommendSvc.GenerateQuickPreview(ctx, RecommendGenerateReq{
		SourceId: "src1",
		Strategy: "hot",
		Quotas:   []RecommendGenerateQuota{{GroupId: g1.Id, Count: 2}},
	})
	if err != nil || len(preview.Groups) != 1 || len(preview.Groups[0].Items) != 2 {
		t.Fatalf("GenerateQuickPreview failed: err=%v preview=%+v", err, preview)
	}
	if err := HomeRecommendSvc.PublishQuickDeck(ctx, RecommendDeckSaveReq{
		SourceId: "src1",
		Groups: []RecommendDeckGroupPayload{{
			GroupId: g1.Id,
			Items: []struct {
				Mid         int64  `json:"mid"`
				DoubanScore string `json:"doubanScore"`
			}{{Mid: 1002, DoubanScore: "9.0"}, {Mid: 1001, DoubanScore: "8.8"}},
		}},
	}); err != nil {
		t.Fatalf("PublishQuickDeck failed: %v", err)
	}
	afterSections, ok := HomeRecommendSvc.HomeSections("src1")
	if !ok || len(afterSections) != 2 || afterSections[1].Items[0].Id != 1002 {
		t.Fatalf("unexpected sections after PublishQuickDeck: ok=%v %+v", ok, afterSections)
	}

	// 6. 榜单模式与自动同步开关解耦：关闭 autoSync 保留榜单绑定，切回自选模式传入空字符串才解绑
	chartKey := "movie_showing"
	autoOff := false
	g3, err := HomeRecommendSvc.CreateGroup(ctx, RecommendGroupInput{
		SourceId:     "src1",
		Title:        "手动同步榜单",
		AutoSync:     &autoOff,
		SyncChartKey: &chartKey,
	})
	if err != nil {
		t.Fatalf("CreateGroup chart manual failed: %v", err)
	}
	if g3.SyncChartKey != chartKey || g3.AutoSync {
		t.Fatalf("expected syncChartKey preserved with autoSync=false, got %+v", g3)
	}
	// 一键排片应自动跳过榜单模式分组，避免互相覆写
	quickRes, err := HomeRecommendSvc.GenerateQuickPreview(ctx, RecommendGenerateReq{
		SourceId: "src1",
		Strategy: "hot",
		Quotas: []RecommendGenerateQuota{
			{GroupId: g3.Id, Count: 2},
			{GroupId: g1.Id, Count: 1},
		},
	})
	if err != nil || len(quickRes.Groups) != 1 || quickRes.Groups[0].GroupId != g1.Id {
		t.Fatalf("expected GenerateQuickPreview to skip chart group g3, got err=%v res=%+v", err, quickRes)
	}
	// 仅修改 enabled 时不传递 SyncChartKey，不得误清榜单绑定
	disabled := false
	if err := HomeRecommendSvc.UpdateGroup(ctx, g3.Id, RecommendGroupInput{Enabled: &disabled}); err != nil {
		t.Fatalf("UpdateGroup enabled only failed: %v", err)
	}
	gotG3, _ := GetHomeRecommendGroup(ctx, g3.Id)
	if gotG3.SyncChartKey != chartKey || gotG3.Enabled {
		t.Fatalf("expected SyncChartKey kept after toggling enabled, got %+v", gotG3)
	}
	// 显式切回自选模式
	emptyKey := ""
	if err := HomeRecommendSvc.UpdateGroup(ctx, g3.Id, RecommendGroupInput{SyncChartKey: &emptyKey}); err != nil {
		t.Fatalf("UpdateGroup reset to manual failed: %v", err)
	}
	gotG3, _ = GetHomeRecommendGroup(ctx, g3.Id)
	if gotG3.SyncChartKey != "" || gotG3.AutoSync {
		t.Fatalf("expected SyncChartKey cleared when switching to manual mode, got %+v", gotG3)
	}
	// 7. 自选分组内已有手工影片，切换到榜单模式时应清理旧手工影片，避免不同榜单/自选混搭残留
	if _, err := HomeRecommendSvc.AddItem(ctx, g3.Id, 1001, ""); err != nil {
		t.Fatalf("AddItem to g3 failed: %v", err)
	}
	newChartKey := "tv_hot"
	if err := HomeRecommendSvc.UpdateGroup(ctx, g3.Id, RecommendGroupInput{
		SyncChartKey: &newChartKey,
		AutoSync:     &autoOff,
	}); err != nil {
		t.Fatalf("UpdateGroup switch chart failed: %v", err)
	}
	allItemsAfterSwitch, _ := ListHomeRecommendItemsBySource(ctx, "src1")
	for _, it := range allItemsAfterSwitch {
		if it.GroupId == g3.Id && it.Mid == 1001 {
			t.Fatalf("expected stale manual item 1001 cleared after switching g3 to chart mode")
		}
	}
}

func TestHomeRecommendService_CategoryControlAndVisibility(t *testing.T) {
	gdb := setupRecommendTestDB(t)
	// 增加第二个一级分类「剧集」
	_ = gdb.Create(&model.Category{Id: 2, Pid: 0, Name: "剧集", StableKey: "rec-tv", Show: true, Sort: 2}).Error
	_ = gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 2, RawName: "剧集", Sort: 2}).Error
	_ = gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 2, CategoryId: 2}).Error
	catalog.MarkCategoryChanged()

	seedRecommendFilm(t, gdb, 1001, "src1", "电影甲", 100)
	// 为剧集分类造片
	tvFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1002, FirstSourceId: "src1"},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid: 2, Cid: 2, CName: "剧集",
			RootCategoryKey: taxonomy.BuildSourceCategoryKey("src1", 2),
			CategoryKey:     taxonomy.BuildSourceCategoryKey("src1", 2),
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "剧集乙", Year: 2026, Score: 8.8, Hits: 200, UpdateStamp: 200,
		},
	}
	if err := gdb.Create(&tvFilm).Error; err != nil {
		t.Fatalf("seed tv film: %v", err)
	}
	if err := gdb.Create(&model.FilmSourcePlaylist{
		Mid: 1002, SourceId: "src1", LineKind: model.LineKindPlay, GroupIndex: 0, GroupName: "默认",
	}).Error; err != nil {
		t.Fatalf("seed tv line: %v", err)
	}
	if err := filmsnapshot.WarmSearchMetaIndex(); err != nil {
		t.Fatalf("WarmSearchMetaIndex: %v", err)
	}

	ctx := context.Background()

	// 1. 一键导入站点可见分类，生成分类楼层分组
	imp, err := HomeRecommendSvc.ImportCategoryGroups(ctx, "src1")
	if err != nil || imp.CreatedCount != 2 {
		t.Fatalf("expected 2 category groups imported, got err=%v res=%+v", err, imp)
	}

	// 再加一个自选专题，放入电影甲(1001) 与 剧集乙(1002)
	manualGrp, err := HomeRecommendSvc.CreateGroup(ctx, RecommendGroupInput{
		SourceId: "src1",
		Title:    "精选专题",
	})
	if err != nil {
		t.Fatalf("CreateGroup manual failed: %v", err)
	}
	if _, err := HomeRecommendSvc.AddItem(ctx, manualGrp.Id, 1001, ""); err != nil {
		t.Fatalf("AddItem 1001 failed: %v", err)
	}
	if _, err := HomeRecommendSvc.AddItem(ctx, manualGrp.Id, 1002, ""); err != nil {
		t.Fatalf("AddItem 1002 failed: %v", err)
	}

	// 2. 在首页推荐中关闭「剧集」分类显示，仅隐藏首页，不改变分类管理里的 Show
	var tvGroupID string
	for _, g := range imp.Groups {
		if g.CategoryId == 2 {
			tvGroupID = g.Id
		}
	}
	off := false
	if err := HomeRecommendSvc.UpdateGroup(ctx, tvGroupID, RecommendGroupInput{Enabled: &off}); err != nil {
		t.Fatalf("disable tv recommend group failed: %v", err)
	}
	sections, _ := HomeRecommendSvc.HomeSections("src1")
	foundMovieCatSection := false
	for _, sec := range sections {
		if sec.Id == tvGroupID {
			t.Fatalf("disabled category group should not appear in HomeSections")
		}
		if sec.Kind == "category" && sec.CategoryId == 1 {
			foundMovieCatSection = true
			if sec.Nav == nil || len(sec.Items) == 0 || len(sec.Hot) == 0 {
				t.Fatalf("expected movie category section with nav/items/hot, got %+v", sec)
			}
		}
	}
	if !foundMovieCatSection {
		t.Fatalf("expected enabled movie category section in HomeSections, got %+v", sections)
	}
	activeTree, _ := catalog.GetActiveCategoryTree("src1")
	foundTV := false
	for _, ch := range activeTree.Children {
		if ch.Id == 2 && ch.Show {
			foundTV = true
		}
	}
	if !foundTV {
		t.Fatalf("disabling in home recommend must NOT hide category in catalog tree")
	}

	// 3. 在分类管理中隐藏「电影」(Id=1)：首页分类楼层和推荐分组中该分类影片均同步隐藏
	if err := gdb.Model(&model.Category{}).Where("id = ?", 1).Update("show", false).Error; err != nil {
		t.Fatalf("hide category 1: %v", err)
	}
	catalog.MarkCategoryChanged()

	sectionsAfterHide, _ := HomeRecommendSvc.HomeSections("src1")
	for _, sec := range sectionsAfterHide {
		if sec.Kind == "category" && sec.CategoryId == 1 {
			t.Fatalf("hidden catalog category should not appear as category section in HomeSections")
		}
		if sec.Id == manualGrp.Id {
			for _, it := range sec.Items {
				if it.Id == 1001 {
					t.Fatalf("film in hidden category should be filtered out from manual recommend section")
				}
			}
			if len(sec.Items) != 1 || sec.Items[0].Id != 1002 {
				t.Fatalf("expected only visible-category film 1002 remaining, got %+v", sec.Items)
			}
		}
	}

	adminRes, err := HomeRecommendSvc.ListAdmin(ctx, "src1")
	if err != nil {
		t.Fatalf("ListAdmin failed: %v", err)
	}
	for _, opt := range adminRes.CategoryOptions {
		if opt.Id == 1 {
			t.Fatalf("hidden category should not appear in CategoryOptions")
		}
	}
	for _, g := range adminRes.Groups {
		if g.CategoryId == 1 && !g.CategoryHidden {
			t.Fatalf("expected CategoryHidden=true for hidden category group")
		}
		if g.Id == manualGrp.Id {
			for _, it := range g.Items {
				if it.Mid == 1001 && (!it.Unavailable || !it.CategoryHidden) {
					t.Fatalf("expected film 1001 marked Unavailable+CategoryHidden, got %+v", it)
				}
			}
		}
	}
}
