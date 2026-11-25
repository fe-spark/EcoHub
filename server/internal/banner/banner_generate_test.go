package banner

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"server/internal/film"
	"server/internal/film/catalog"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
)

func TestBannerGenerate_Validation(t *testing.T) {
	setupBannerSvcDB(t)
	ok := []BannerQuota{{CategoryID: 1, Count: 1}}
	cases := []struct {
		name string
		req  BannerGenerateReq
	}{
		{"非法策略", BannerGenerateReq{Strategy: "hot_random", Quotas: ok}},
		{"空配额", BannerGenerateReq{Strategy: "hot"}},
		{"全为 0", BannerGenerateReq{Strategy: "hot", Quotas: []BannerQuota{{CategoryID: 1, Count: 0}}}},
		{"数量越界", BannerGenerateReq{Strategy: "hot", Quotas: []BannerQuota{{CategoryID: 1, Count: 100}}}},
		{"负数", BannerGenerateReq{Strategy: "hot", Quotas: []BannerQuota{{CategoryID: 1, Count: -1}}}},
		{"分类编号非法", BannerGenerateReq{Strategy: "hot", Quotas: []BannerQuota{{CategoryID: 0, Count: 1}}}},
	}
	for _, tc := range cases {
		if _, err := BannerSvc.Generate(context.Background(), tc.req); !IsBannerUserError(err) {
			t.Errorf("%s: want user error, got %v", tc.name, err)
		}
	}
}

func TestBannerGenerate_ResultAndSkipped(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	cats := []model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 1, RawName: "电影", Sort: 1},
		{SourceId: "src1", SourceTypeId: 2, RawName: "电视剧", Sort: 2},
		{SourceId: "src2", SourceTypeId: 1, RawName: "电影", Sort: 1},
	}
	if err := gdb.Create(&cats).Error; err != nil {
		t.Fatal(err)
	}
	taxonomy.RefreshCategoryCache()
	seedBannerFilm(t, gdb, 11, "src1", 1, "电影一", 100)
	seedBannerFilm(t, gdb, 12, "src1", 1, "电影二", 90)
	seedBannerFilm(t, gdb, 21, "src1", 2, "剧一", 80)
	seedBannerFilm(t, gdb, 31, "src2", 1, "别站电影", 1000)

	res, err := BannerSvc.Generate(context.Background(), BannerGenerateReq{
		Strategy: "hot",
		Quotas:   []BannerQuota{{CategoryID: 1, Count: 2}, {CategoryID: 2, Count: 3}, {CategoryID: 99, Count: 1}},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// 分类轮流穿插，只取该站影片。
	if fmt.Sprint(bannerMidList(res.Items)) != "[11 21 12]" {
		t.Fatalf("generate items = %v", bannerMidList(res.Items))
	}
	reasons := map[int64]BannerSkipped{}
	for _, s := range res.Skipped {
		reasons[s.CategoryID] = s
	}
	if reasons[2].Reason != "not_enough" || reasons[2].Got != 1 || reasons[2].Requested != 3 {
		t.Fatalf("category 2 should be not_enough: %+v", res.Skipped)
	}
	if reasons[99].Reason != "not_site_category" {
		t.Fatalf("category 99 should be not_site_category: %+v", res.Skipped)
	}
	// 不落库，响应里没有回显配置。
	if slots, _ := ListBannerSlots("src1"); len(slots) != 0 {
		t.Fatalf("generate must not persist: %+v", slots)
	}
	raw, _ := json.Marshal(res)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if _, ok := m["strategy"]; ok {
		t.Fatalf("generate should not echo strategy: %s", raw)
	}
	if _, ok := m["quotas"]; ok {
		t.Fatalf("generate should not echo quotas: %s", raw)
	}
	// 用生成时的 revision 发布成功。
	items := make([]BannerFields, 0, len(res.Items))
	for _, it := range res.Items {
		items = append(items, BannerFields{Mid: i64Ptr(it.Mid)})
	}
	if _, err := BannerSvc.PublishDeck("", res.Revision, items); err != nil {
		t.Fatalf("publish generated deck: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BannerSvc.Generate(ctx, BannerGenerateReq{Strategy: "hot", Quotas: []BannerQuota{{CategoryID: 1, Count: 1}}}); err == nil {
		t.Fatalf("canceled generate should fail")
	}
}

func TestBannerCascadeDelete(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "站一影片", 10)
	seedBannerFilm(t, gdb, 2, "src1", 1, "站一影片二", 9)
	seedBannerFilm(t, gdb, 3, "src2", 1, "站二影片", 10)
	// 主站影片走服务接口；别站行模拟切换主站前遗留的数据直接入库。
	for _, mid := range []int64{1, 2} {
		if _, err := BannerSvc.Create("", BannerFields{Mid: i64Ptr(mid)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := gdb.Create(&model.BannerSlot{Id: "legacy-src2-3", SourceId: "src2", Mid: 3, ImageMode: model.BannerImageFollow, Sort: 3}).Error; err != nil {
		t.Fatal(err)
	}
	count := func(where string, args ...any) int64 {
		var n int64
		gdb.Model(&model.BannerSlot{}).Where(where, args...).Count(&n)
		return n
	}

	// 后台删影片：同事务删轮播。
	if err := film.DelFilmSearchBatch([]int64{1}); err != nil {
		t.Fatalf("delete film: %v", err)
	}
	if count("mid = ?", 1) != 0 || count("mid = ?", 2) != 1 {
		t.Fatalf("deleting film should delete only its banner")
	}

	// 定时清理空片：同样删轮播。
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 2).Update("name", "")
	if cleaned, err := film.CleanEmptyFilms(); err != nil || cleaned == 0 {
		t.Fatalf("expected empty film cleaned, got %d, %v", cleaned, err)
	}
	if count("mid = ?", 2) != 0 {
		t.Fatalf("cleaning empty film should delete its banner")
	}
	seedBannerFilm(t, gdb, 4, "src1", 1, "站一影片三", 8)
	if _, err := BannerSvc.Create("", BannerFields{Mid: i64Ptr(4)}); err != nil {
		t.Fatal(err)
	}

	// 清空采集站数据：该站轮播整组删除，别站不动。
	if _, err := catalog.CascadeCleanSourceData("src1"); err != nil {
		t.Fatalf("clean source: %v", err)
	}
	if count("source_id = ?", "src1") != 0 || count("source_id = ?", "src2") != 1 {
		t.Fatalf("cleaning src1 should only remove src1 banners")
	}
}

// 影片在该站没有播放线路（或已不在片库）时：首页跳过；后台照常列出并标记 unavailable，
// 排序提交全部行不会一直 409，不可用的行也能删除。
func TestBanner_UnavailableRows(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "A", 10)
	seedBannerFilm(t, gdb, 2, "src1", 1, "B", 9)
	seedBannerFilm(t, gdb, 3, "src1", 1, "C", 8)
	a, _ := BannerSvc.Create("", BannerFields{Mid: i64Ptr(1)})
	b, _ := BannerSvc.Create("", BannerFields{Mid: i64Ptr(2)})
	c, _ := BannerSvc.Create("", BannerFields{Mid: i64Ptr(3)})

	// 2 在本站没线路了（档案还在），3 档案没了但轮播行还在（例如导入的旧数据）。
	gdb.Where("mid = ? AND source_id = ?", 2, "src1").Delete(&model.FilmSourcePlaylist{})
	gdb.Where("mid = ?", 3).Delete(&model.FilmIndex{})

	home, err := BannerSvc.HomeBanners("src1")
	if err != nil || len(home) != 1 || home[0].Mid != 1 || home[0].Sort != 1 {
		t.Fatalf("home should only show films playable on this site: %v %+v", err, home)
	}

	view, err := BannerSvc.List("")
	if err != nil || len(view.Items) != 3 {
		t.Fatalf("admin list should keep all rows: %v %+v", err, view.Items)
	}
	if view.Items[0].Unavailable || !view.Items[1].Unavailable || !view.Items[2].Unavailable {
		t.Fatalf("unavailable flags mismatch: %+v", view.Items)
	}

	next, err := BannerSvc.Reorder("", view.Revision, []string{c.Id, a.Id, b.Id})
	if err != nil {
		t.Fatalf("reorder with unavailable rows should succeed: %v", err)
	}
	if fmt.Sprint(bannerMidList(next.Items)) != "[3 1 2]" {
		t.Fatalf("reorder result = %v", bannerMidList(next.Items))
	}
	if err := BannerSvc.Delete(c.Id, ""); err != nil {
		t.Fatalf("delete unavailable row: %v", err)
	}
	if err := BannerSvc.Delete(c.Id, ""); !IsBannerUserError(err) {
		t.Fatalf("deleting twice should be not found, got %v", err)
	}
	after, _ := BannerSvc.List("")
	if fmt.Sprint(bannerMidList(after.Items)) != "[1 2]" {
		t.Fatalf("after delete = %v", bannerMidList(after.Items))
	}
}

// 换片且没有提交图片时，旧影片的自定义海报和横图要一起清掉。
func TestBannerUpdate_ChangeFilmResetsImages(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "A", 10)
	seedBannerFilm(t, gdb, 2, "src1", 1, "B", 9)
	seedBannerFilm(t, gdb, 3, "src1", 1, "C", 8)
	item, err := BannerSvc.Create("", BannerFields{
		Mid: i64Ptr(1), ImageMode: strPtr("custom"), Poster: strPtr("http://old.jpg"), Backdrop: strPtr("http://old-slide.jpg"),
	})
	if err != nil || !item.IsCustomPic {
		t.Fatalf("create custom: %v %+v", err, item)
	}
	item, err = BannerSvc.Update(item.Id, "", BannerFields{Mid: i64Ptr(2)})
	if err != nil {
		t.Fatalf("change film: %v", err)
	}
	if item.Mid != 2 || item.ImageMode != model.BannerImageFollow || item.IsCustomPic ||
		item.Overrides.Poster != "" || item.Overrides.Backdrop != "" || item.Picture != "http://img/2.jpg" {
		t.Fatalf("changing film should reset images: %+v", item)
	}
	// 换片时一起提交了新图片，则保留新图片。
	item, err = BannerSvc.Update(item.Id, "", BannerFields{Mid: i64Ptr(3), ImageMode: strPtr("custom"), Poster: strPtr("http://new.jpg")})
	if err != nil {
		t.Fatalf("change film with image: %v", err)
	}
	if item.Mid != 3 || !item.IsCustomPic || item.Picture != "http://new.jpg" {
		t.Fatalf("submitted image should be kept: %+v", item)
	}
	// 换片时提交图片但未传 ImageMode，也应自动切为 custom 并保留新图。
	item, err = BannerSvc.Update(item.Id, "", BannerFields{Mid: i64Ptr(1), Backdrop: strPtr("http://new-slide.jpg")})
	if err != nil {
		t.Fatalf("change film with backdrop only: %v", err)
	}
	if item.Mid != 1 || !item.IsCustomPic || item.PictureSlide != "http://new-slide.jpg" {
		t.Fatalf("submitted backdrop without imageMode should switch to custom: %+v", item)
	}
	// custom 模式下图片全部为空应自动回退 follow，避免状态自相矛盾。
	item, err = BannerSvc.Update(item.Id, "", BannerFields{ImageMode: strPtr("custom"), Poster: strPtr(""), Backdrop: strPtr("")})
	if err != nil {
		t.Fatalf("custom with empty images: %v", err)
	}
	if item.IsCustomPic || item.ImageMode != model.BannerImageFollow {
		t.Fatalf("empty custom images should fall back to follow: %+v", item)
	}
	// PublishDeck 不允许传空切片清空全站轮播。
	view, _ := BannerSvc.List("")
	if _, err := BannerSvc.PublishDeck("", view.Revision, []BannerFields{}); !IsBannerUserError(err) {
		t.Fatalf("empty deck publish should be rejected, got %v", err)
	}
}

func TestBannerGenerate_PerCategoryLimit(t *testing.T) {
	setupBannerSvcDB(t)
	quotas := []BannerQuota{{CategoryID: 1, Count: bannerQuotaMax + 1}}
	if _, err := BannerSvc.Generate(context.Background(), BannerGenerateReq{Strategy: "hot", Quotas: quotas}); !IsBannerUserError(err) {
		t.Fatalf("generate per-category over limit should be rejected, got %v", err)
	}
}

// 片库查询出错时 HomeBanners 返回 error，不当成空轮播。
func TestHomeBanners_ReturnsErrorOnLibraryFailure(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "A", 10)
	if _, err := BannerSvc.Create("", BannerFields{Mid: i64Ptr(1)}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Migrator().DropTable(&model.FilmIndex{}); err != nil {
		t.Fatal(err)
	}
	if _, err := BannerSvc.HomeBanners("src1"); err == nil {
		t.Fatalf("library failure should surface as error")
	}
}

func TestBannerSearchFilmsAndCategories_BySource(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	cats := []model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 1, RawName: "站一电影", Sort: 1},
		{SourceId: "src2", SourceTypeId: 10, RawName: "站二剧集", Sort: 1},
	}
	if err := gdb.Create(&cats).Error; err != nil {
		t.Fatal(err)
	}
	taxonomy.RefreshCategoryCache()
	seedBannerFilm(t, gdb, 1, "src1", 1, "站一独有", 100)
	seedBannerFilm(t, gdb, 2, "src2", 10, "站二独有", 90)

	cat1, err := BannerSvc.Categories("src1")
	if err != nil || len(cat1) != 1 || cat1[0].Name != "站一电影" {
		t.Fatalf("src1 categories mismatch: %v %+v", err, cat1)
	}
	cat2, err := BannerSvc.Categories("src2")
	if err != nil || len(cat2) != 1 || cat2[0].Name != "站二剧集" {
		t.Fatalf("src2 categories mismatch: %v %+v", err, cat2)
	}

	page := &dto.Page{Current: 1, PageSize: 10}
	films2, resolved, err := BannerSvc.SearchFilms(context.Background(), "src2", "", "hits", page)
	if err != nil || resolved != "src2" || len(films2) != 1 || films2[0].Mid != 2 {
		t.Fatalf("src2 search films mismatch: err=%v resolved=%s list=%+v", err, resolved, films2)
	}

	res, err := BannerSvc.Generate(context.Background(), BannerGenerateReq{
		SourceId: "src2",
		Strategy: "hot",
		Quotas:   []BannerQuota{{CategoryID: cat2[0].Id, Count: 1}},
	})
	if err != nil || res.SourceId != "src2" || fmt.Sprint(bannerMidList(res.Items)) != "[2]" {
		t.Fatalf("src2 generate mismatch: err=%v res=%+v", err, res)
	}

	randRes, err := BannerSvc.Generate(context.Background(), BannerGenerateReq{
		SourceId: "src1",
		Strategy: "random",
		Quotas:   []BannerQuota{{CategoryID: cat1[0].Id, Count: 1}},
	})
	if err != nil || randRes.SourceId != "src1" || fmt.Sprint(bannerMidList(randRes.Items)) != "[1]" {
		t.Fatalf("src1 random generate mismatch: err=%v res=%+v", err, randRes)
	}
}

// 兜底补位按精确展示分类 cid 与跨站同名分类筛选，不能按根 pid 把同根下的隐藏/成人子分类影片带出。
func TestBannerGenerate_FallbackIgnoresDifferentSubcategoryUnderSameRoot(t *testing.T) {
	prevSnap := taxonomy.CurrentMappingSnapshotForTest()
	t.Cleanup(func() { taxonomy.StoreMappingSnapshotForTest(prevSnap) })

	gdb := setupBannerSvcDB(t)
	cats := []model.Category{
		{Id: 100, Pid: 0, Name: "电影", StableKey: "s1-movie", Show: true, Sort: 1},
		{Id: 101, Pid: 100, Name: "动作片", StableKey: "s1-action", Show: true, Sort: 1},
		{Id: 102, Pid: 100, Name: "福利片", StableKey: "s1-adult", Show: false, Sort: 2},
		{Id: 200, Pid: 0, Name: "影视库", StableKey: "s2-root", Show: true, Sort: 1},
		{Id: 201, Pid: 200, Name: "动作片", StableKey: "s2-action", Show: true, Sort: 1},
	}
	if err := gdb.Create(&cats).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&model.Category{}).Where("id = ?", 102).Update("show", false).Error; err != nil {
		t.Fatal(err)
	}
	mappings := []model.CategoryMapping{
		{SourceId: "src1", SourceTypeId: 1, CategoryId: 100},
		{SourceId: "src1", SourceTypeId: 11, CategoryId: 101},
		{SourceId: "src1", SourceTypeId: 12, CategoryId: 102},
		{SourceId: "src2", SourceTypeId: 21, CategoryId: 201},
	}
	if err := gdb.Create(&mappings).Error; err != nil {
		t.Fatal(err)
	}
	srcCats := []model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0},
		{SourceId: "src1", SourceTypeId: 11, ParentSourceTypeId: 1, RawName: "动作片", Sort: 1, Depth: 1},
		{SourceId: "src1", SourceTypeId: 12, ParentSourceTypeId: 1, RawName: "福利片", Sort: 2, Depth: 1},
		{SourceId: "src2", SourceTypeId: 21, ParentSourceTypeId: 20, RawName: "动作片", Sort: 1, Depth: 1},
	}
	if err := gdb.Create(&srcCats).Error; err != nil {
		t.Fatal(err)
	}
	taxonomy.RefreshCategoryCache()
	catalog.MarkCategoryChanged()
	taxonomy.ReloadMappingRules()
	_ = sourcecatalog.RefreshCollectSourceCatalog()

	// mid=10：首采来自 src2 的「动作片」（pid=200, cid=201），但挂载了 src1 播放线路，应通过同名分类兜底命中。
	aggFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 10, FirstSourceId: "src2"},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid: 200, Cid: 201, CName: "动作片",
			RootCategoryKey: taxonomy.BuildSourceCategoryKey("src2", 20),
			CategoryKey:     taxonomy.BuildSourceCategoryKey("src2", 21),
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "跨站动作片", Year: 2024, Score: 8.1, Hits: 500,
			Picture: "http://img/10.jpg", UpdateStamp: 500,
		},
	}
	// mid=20：同属 src1 根分类 pid=100 下的隐藏子类「福利片」(cid=102)，即便热度更高也不得混入「电影」轮播。
	hiddenSubFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 20, FirstSourceId: "src1"},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid: 100, Cid: 102, CName: "福利片",
			RootCategoryKey: taxonomy.BuildSourceCategoryKey("src1", 1),
			CategoryKey:     taxonomy.BuildSourceCategoryKey("src1", 12),
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "隐藏子分类影片", Year: 2024, Score: 9.0, Hits: 9999,
			Picture: "http://img/20.jpg", UpdateStamp: 9999,
		},
	}
	if err := gdb.Create(&[]model.FilmIndex{aggFilm, hiddenSubFilm}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&[]model.FilmSourcePlaylist{
		{Mid: 10, SourceId: "src1", LineKind: model.LineKindPlay, GroupIndex: 0, GroupName: "线路"},
		{Mid: 20, SourceId: "src1", LineKind: model.LineKindPlay, GroupIndex: 0, GroupName: "线路"},
	}).Error; err != nil {
		t.Fatal(err)
	}

	res, err := BannerSvc.Generate(context.Background(), BannerGenerateReq{
		SourceId: "src1",
		Strategy: "hot",
		Quotas:   []BannerQuota{{CategoryID: 1, Count: 5}},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if fmt.Sprint(bannerMidList(res.Items)) != "[10]" {
		t.Fatalf("expected only aggregated matching subcategory [10] and exclude hidden subcategory 20, got %v", bannerMidList(res.Items))
	}
}
