package recommend

import (
	"context"
	"sync"
	"testing"

	"server/internal/collect"
	"server/internal/film/catalog"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/model"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
)

func TestHomeRecommendInit_PortalAutoImportAndNoResurrect(t *testing.T) {
	gdb := setupRecommendTestDB(t)
	seedRecommendFilm(t, gdb, 1001, "src1", "电影甲", 100)
	if err := filmsnapshot.WarmSearchMetaIndex(); err != nil {
		t.Fatalf("WarmSearchMetaIndex: %v", err)
	}

	// 1. 首次仅通过前台 HomeSections 访问，应自动一次性导入可见一级分类
	sections, ok := HomeRecommendSvc.HomeSections("src1")
	if !ok || len(sections) != 1 || sections[0].Kind != "category" || sections[0].CategoryId != 1 {
		t.Fatalf("expected auto-imported movie category section, got ok=%v sections=%+v", ok, sections)
	}
	inited, exists, err := IsSourceHomeRecommendInited(context.Background(), "src1")
	if err != nil || !exists || !inited {
		t.Fatalf("expected src1 marked inited, got inited=%v exists=%v err=%v", inited, exists, err)
	}

	// 2. 删除该分组后，前台与后台再次读取均不再自动复生
	if err := HomeRecommendSvc.DeleteGroup(context.Background(), sections[0].Id); err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}
	afterSections, ok := HomeRecommendSvc.HomeSections("src1")
	if !ok || len(afterSections) != 0 {
		t.Fatalf("expected empty sections after deleting all groups, got ok=%v sections=%+v", ok, afterSections)
	}
	adminRes, err := HomeRecommendSvc.ListAdmin(context.Background(), "src1")
	if err != nil || len(adminRes.Groups) != 0 {
		t.Fatalf("expected empty admin groups after deleting all groups, got err=%v groups=%+v", err, adminRes.Groups)
	}
}

func TestHomeRecommendInit_SwitchPrimaryAndSourceIsolation(t *testing.T) {
	gdb := setupRecommendTestDB(t)
	seedRecommendFilm(t, gdb, 1001, "src1", "主站电影", 100)

	ctx := context.Background()

	// 1. src1 首次读取自动初始化，随后自定义新增一个专题并删除默认分类
	src1Admin, err := HomeRecommendSvc.ListAdmin(ctx, "src1")
	if err != nil || len(src1Admin.Groups) != 1 {
		t.Fatalf("src1 initial ListAdmin failed: err=%v groups=%+v", err, src1Admin.Groups)
	}
	if err := HomeRecommendSvc.DeleteGroup(ctx, src1Admin.Groups[0].Id); err != nil {
		t.Fatalf("delete src1 default group: %v", err)
	}
	if _, err := HomeRecommendSvc.CreateGroup(ctx, RecommendGroupInput{SourceId: "src1", Title: "主站专题"}); err != nil {
		t.Fatalf("create src1 custom group: %v", err)
	}

	// 2. 切主站到 src2（此时 src2 尚无分类）：不导入也不落标
	if err := sourcecatalog.SetPrimarySourceID("src2"); err != nil {
		t.Fatalf("SetPrimarySourceID src2: %v", err)
	}
	catalog.MarkCategoryChanged()
	src2Empty, err := HomeRecommendSvc.ListAdmin(ctx, "src2")
	if err != nil || len(src2Empty.Groups) != 0 {
		t.Fatalf("expected src2 empty before categories exist, got err=%v groups=%+v", err, src2Empty.Groups)
	}
	inited, _, err := IsSourceHomeRecommendInited(ctx, "src2")
	if err != nil || inited {
		t.Fatalf("expected src2 still uninitialized when no categories, got inited=%v err=%v", inited, err)
	}

	// 3. src2 采集出自己的可见一级分类「动漫」并造片后，首次前台读取自动导入「动漫」
	_ = gdb.Create(&model.Category{Id: 3, Pid: 0, Name: "动漫", StableKey: "rec-anime", Show: true, Sort: 1}).Error
	_ = gdb.Create(&model.SourceCategory{SourceId: "src2", SourceTypeId: 30, RawName: "动漫", Sort: 1}).Error
	_ = gdb.Create(&model.CategoryMapping{SourceId: "src2", SourceTypeId: 30, CategoryId: 3}).Error
	catalog.MarkCategoryChanged()

	animeFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2001, FirstSourceId: "src2"},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid: 3, Cid: 3, CName: "动漫",
			RootCategoryKey: taxonomy.BuildSourceCategoryKey("src2", 30),
			CategoryKey:     taxonomy.BuildSourceCategoryKey("src2", 30),
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "新番甲", Year: 2026, Score: 9.0, Hits: 300, UpdateStamp: 300,
		},
	}
	if err := gdb.Create(&animeFilm).Error; err != nil {
		t.Fatalf("seed anime film: %v", err)
	}
	if err := gdb.Create(&model.FilmSourcePlaylist{
		Mid: 2001, SourceId: "src2", LineKind: model.LineKindPlay, GroupIndex: 0, GroupName: "默认",
	}).Error; err != nil {
		t.Fatalf("seed anime line: %v", err)
	}
	if err := filmsnapshot.WarmSearchMetaIndex(); err != nil {
		t.Fatalf("WarmSearchMetaIndex: %v", err)
	}

	src2Sections, ok := HomeRecommendSvc.HomeSections("src2")
	if !ok || len(src2Sections) != 1 || src2Sections[0].Kind != "category" || src2Sections[0].CategoryId != 30 || src2Sections[0].Title != "动漫" {
		t.Fatalf("expected src2 auto-imported anime category section, got ok=%v sections=%+v", ok, src2Sections)
	}
	inited2, _, err := IsSourceHomeRecommendInited(ctx, "src2")
	if err != nil || !inited2 {
		t.Fatalf("expected src2 marked inited after auto-import, got inited=%v err=%v", inited2, err)
	}

	// 4. 切回 src1，src1 原先的自定义专题原封不动，没有被重新塞入分类
	if err := sourcecatalog.SetPrimarySourceID("src1"); err != nil {
		t.Fatalf("SetPrimarySourceID src1: %v", err)
	}
	catalog.MarkCategoryChanged()
	src1After, err := HomeRecommendSvc.ListAdmin(ctx, "src1")
	if err != nil || len(src1After.Groups) != 1 || src1After.Groups[0].Title != "主站专题" {
		t.Fatalf("expected src1 custom group intact after switching back, got err=%v groups=%+v", err, src1After.Groups)
	}
}

func TestHomeRecommendInit_V317UpgradeAppendsMissingCategoriesOnce(t *testing.T) {
	setupRecommendTestDB(t)
	ctx := context.Background()

	// 模拟 v3.1.7 升级：已有豆瓣榜单/自选分组（category_id=0），但 home_recommend_inited 仍为 false
	if _, err := CreateHomeRecommendGroup(ctx, model.HomeRecommendGroup{
		SourceId:     "src1",
		Title:        "热门电影",
		Enabled:      true,
		AutoSync:     true,
		SyncChartKey: "movie_hot",
	}); err != nil {
		t.Fatalf("seed v3.1.7 group: %v", err)
	}

	adminRes, err := HomeRecommendSvc.ListAdmin(ctx, "src1")
	if err != nil {
		t.Fatalf("ListAdmin failed: %v", err)
	}
	// 升级后保留原推荐分组在前，并在其后补齐站点可见一级分类，保持与 v3.1.7「上推荐、下分类」一致
	if len(adminRes.Groups) != 2 || adminRes.Groups[0].Title != "热门电影" || adminRes.Groups[1].CategoryId != 1 {
		t.Fatalf("expected existing group preserved + missing category appended, got %+v", adminRes.Groups)
	}
	inited, _, err := IsSourceHomeRecommendInited(ctx, "src1")
	if err != nil || !inited {
		t.Fatalf("expected source marked inited=true, got inited=%v err=%v", inited, err)
	}

	// 用户手动删除补齐的分类分组后，只保留原推荐分组，后续不再复生
	if err := HomeRecommendSvc.DeleteGroup(ctx, adminRes.Groups[1].Id); err != nil {
		t.Fatalf("delete appended category group: %v", err)
	}
	afterDel, err := HomeRecommendSvc.ListAdmin(ctx, "src1")
	if err != nil || len(afterDel.Groups) != 1 || afterDel.Groups[0].Title != "热门电影" {
		t.Fatalf("expected deleted category group not re-appended, got err=%v groups=%+v", err, afterDel.Groups)
	}
}

func TestHomeRecommendInit_ReplaceSourcesCleansOrphanGroups(t *testing.T) {
	gdb := setupRecommendTestDB(t)
	if err := gdb.AutoMigrate(&model.BannerSlot{}); err != nil {
		t.Fatalf("migrate banner_slot: %v", err)
	}
	ctx := context.Background()

	// src1 先自动初始化生成分组并手动删除（标记为 inited=true 且空分组），src2 也自动初始化后删除
	res1, err := HomeRecommendSvc.ListAdmin(ctx, "src1")
	if err != nil || len(res1.Groups) != 1 {
		t.Fatalf("initial src1 ListAdmin failed: err=%v groups=%+v", err, res1.Groups)
	}
	if err := HomeRecommendSvc.DeleteGroup(ctx, res1.Groups[0].Id); err != nil {
		t.Fatalf("delete src1 group: %v", err)
	}
	// 备份导入保留 src1（同 URI）并移除 src2：src1 必须保留 inited=true（不复生分类），src2 残留分组被清理
	if _, err := CreateHomeRecommendGroup(ctx, model.HomeRecommendGroup{SourceId: "src2", Title: "副站专题", Enabled: true}); err != nil {
		t.Fatalf("seed src2 group: %v", err)
	}
	if err := collect.ReplaceCollectSources([]model.FilmSource{
		{Id: "src1", Name: "主站", Uri: "http://src1.test", State: true, Sort: 0},
	}); err != nil {
		t.Fatalf("ReplaceCollectSources failed: %v", err)
	}
	leftover, err := ListHomeRecommendGroups(ctx, "src2")
	if err != nil || len(leftover) != 0 {
		t.Fatalf("expected removed source recommend groups cleaned, got err=%v leftover=%+v", err, leftover)
	}
	inited1, _, err := IsSourceHomeRecommendInited(ctx, "src1")
	if err != nil || !inited1 {
		t.Fatalf("expected kept source src1 to preserve home_recommend_inited=true, got inited=%v err=%v", inited1, err)
	}
	afterReplace, err := HomeRecommendSvc.ListAdmin(ctx, "src1")
	if err != nil || len(afterReplace.Groups) != 0 {
		t.Fatalf("expected deleted category group not resurrected after backup import, got err=%v groups=%+v", err, afterReplace.Groups)
	}
}

func TestHomeRecommendSync_NoAutoGroupsIsNoop(t *testing.T) {
	setupRecommendTestDB(t)
	// 无任何开启 autoSync 的榜单分组时，SyncAllAutoGroups 应安全快速返回且不触发外部请求
	HomeRecommendSvc.SyncAllAutoGroups()
}

func TestHomeRecommendInit_SourceUpdateKeepsFlagAndCleanResets(t *testing.T) {
	gdb := setupRecommendTestDB(t)
	if err := gdb.AutoMigrate(
		&model.FilmSourcePlaylist{},
		&model.MovieSourceMapping{},
		&model.MovieMatchKey{},
		&model.BannerSlot{},
	); err != nil {
		t.Fatalf("migrate extra tables: %v", err)
	}
	ctx := context.Background()

	// 1. 首次自动导入后删除分组，标记应保持 true
	res, err := HomeRecommendSvc.ListAdmin(ctx, "src1")
	if err != nil || len(res.Groups) != 1 {
		t.Fatalf("initial ListAdmin failed: err=%v groups=%+v", err, res.Groups)
	}
	if err := HomeRecommendSvc.DeleteGroup(ctx, res.Groups[0].Id); err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}

	// 2. 普通更新站点配置（前端不携带 HomeRecommendInited，默认零值 false），不得覆盖标记
	edited := model.FilmSource{
		Id:    "src1",
		Name:  "主站改名",
		Uri:   "http://src1.test",
		State: true,
		Sort:  1,
	}
	if err := collect.UpdateCollectSource(edited); err != nil {
		t.Fatalf("UpdateCollectSource failed: %v", err)
	}
	initedAfterEdit, _, err := IsSourceHomeRecommendInited(ctx, "src1")
	if err != nil || !initedAfterEdit {
		t.Fatalf("normal source update must preserve home_recommend_inited=true, got inited=%v err=%v", initedAfterEdit, err)
	}

	// 3. 换源清库（CascadeCleanSourceData + UpdateCollectSourceReplacingData(clearCategories=true)）后，标记复位为 false
	if _, err := catalog.CascadeCleanSourceData("src1"); err != nil {
		t.Fatalf("CascadeCleanSourceData failed: %v", err)
	}
	replaced := edited
	replaced.Uri = "http://src1-new.test"
	if err := collect.UpdateCollectSourceReplacingData(replaced, true); err != nil {
		t.Fatalf("UpdateCollectSourceReplacingData failed: %v", err)
	}
	initedAfterReplace, _, err := IsSourceHomeRecommendInited(ctx, "src1")
	if err != nil || initedAfterReplace {
		t.Fatalf("source URI replacement must reset home_recommend_inited=false, got inited=%v err=%v", initedAfterReplace, err)
	}

	// 4. 新源重新产生可见分类后，再次自动导入
	_ = gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "纪录片", StableKey: "rec-doc", Show: true, Sort: 1}).Error
	_ = gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 9, RawName: "纪录片", Sort: 1}).Error
	_ = gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 9, CategoryId: 9}).Error
	catalog.MarkCategoryChanged()

	resAfterNewCat, err := HomeRecommendSvc.ListAdmin(ctx, "src1")
	if err != nil || len(resAfterNewCat.Groups) != 1 || resAfterNewCat.Groups[0].CategoryId != 9 {
		t.Fatalf("expected auto-import after source replacement, got err=%v groups=%+v", err, resAfterNewCat.Groups)
	}
}

func TestHomeRecommendInit_ConcurrentSafe(t *testing.T) {
	setupRecommendTestDB(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = HomeRecommendSvc.ListAdmin(ctx, "src1")
			_, _ = HomeRecommendSvc.HomeSections("src1")
		}()
	}
	wg.Wait()

	groups, err := ListHomeRecommendGroups(ctx, "src1")
	if err != nil {
		t.Fatalf("ListHomeRecommendGroups failed: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected exactly 1 default category group under concurrency, got %d: %+v", len(groups), groups)
	}
}
