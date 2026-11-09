package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/repository/support"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupBannerSvcDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:banner_svc_%d_%d?mode=memory&cache=shared", time.Now().UnixNano(), rand.Intn(100000))
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(
		&model.FilmIndex{}, &model.FilmSourcePlaylist{}, &model.FilmSource{}, &model.BannerSlot{},
		&model.SourceCategory{}, &model.Category{}, &model.CategoryMapping{}, &model.MappingRule{},
		&model.MovieMatchKey{}, &model.MovieSourceMapping{}, &model.FilmTMDBScrape{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	origMdb, origRdb := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	t.Cleanup(func() { db.Mdb, db.Rdb = origMdb, origRdb })

	now := time.Now()
	sources := []model.FilmSource{
		{Id: "src1", Name: "站点一", Uri: "http://src1.test", State: true, Sort: 1, CreatedAt: now},
		{Id: "src2", Name: "站点二", Uri: "http://src2.test", State: true, Sort: 2, CreatedAt: now},
		{Id: "src3", Name: "停用站", Uri: "http://src3.test", State: false, Sort: 3, CreatedAt: now},
	}
	if err := gdb.Create(&sources).Error; err != nil {
		t.Fatalf("seed sources: %v", err)
	}
	return gdb
}

// seedBannerFilm 建一部属于 source 的影片：档案、该站播放线路、该站分类键。
func seedBannerFilm(t *testing.T, gdb *gorm.DB, mid int64, source string, typeID int64, name string, hits int64) {
	t.Helper()
	film := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid, FirstSourceId: source},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid: typeID, Cid: typeID, CName: "电影",
			RootCategoryKey: support.BuildSourceCategoryKey(source, typeID),
			CategoryKey:     support.BuildSourceCategoryKey(source, typeID),
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: name, Year: 2024, Score: 7.5, Hits: hits, Remarks: "HD", Area: "中国大陆",
			ClassTag: "剧情", Actor: "演员", Director: "导演", Blurb: "片库简介",
			Picture: fmt.Sprintf("http://img/%d.jpg", mid), PictureSlide: fmt.Sprintf("http://img/%d-slide.jpg", mid),
			UpdateStamp: hits,
		},
	}
	if err := gdb.Create(&film).Error; err != nil {
		t.Fatalf("seed film: %v", err)
	}
	line := model.FilmSourcePlaylist{Mid: mid, SourceId: source, LineKind: model.LineKindPlay, GroupIndex: 0, GroupName: "线路"}
	if err := gdb.Create(&line).Error; err != nil {
		t.Fatalf("seed line: %v", err)
	}
}

func strPtr(s string) *string   { return &s }
func i64Ptr(v int64) *int64     { return &v }
func f64Ptr(v float64) *float64 { return &v }
func bannerMidList(items []model.BannerAdminItem) []int64 {
	out := make([]int64, 0, len(items))
	for _, it := range items {
		out = append(out, it.Mid)
	}
	return out
}

// 首页 /home 的 banners 字段集合不能变，Web、Android、鸿蒙按这些键读取。
func TestBannerJSONContract_HomeKeys(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "影片", 10)
	if _, err := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(1), TmdbID: i64Ptr(5)}); err != nil {
		t.Fatalf("create: %v", err)
	}
	home, err := BannerSvc.HomeBanners("src1")
	if err != nil || len(home) != 1 {
		t.Fatalf("home banners: %v %+v", err, home)
	}
	raw, _ := json.Marshal(home[0])
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{
		"actor", "area", "blurb", "cName", "classTag", "customPicture", "director", "id", "isCustomPic",
		"isScraped", "mid", "name", "picture", "pictureSlide", "poster", "remark", "score", "sort", "tmdbId", "year",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("home banner keys changed:\n got %v\nwant %v", got, want)
	}
	b := home[0]
	if b.Name != "影片" || b.CName != "电影" || b.Remark != "HD" || b.Picture != "http://img/1.jpg" ||
		b.PictureSlide != "http://img/1-slide.jpg" || b.Area != "中国大陆" || b.Year != 2024 || !b.IsScraped {
		t.Fatalf("assembled banner mismatch: %+v", b)
	}
}

func TestBannerUpdate_PartialFields(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "影片", 10)
	item, err := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(1)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if item.ImageMode != model.BannerImageFollow || item.Blurb != "片库简介" || item.Library.Poster != "http://img/1.jpg" {
		t.Fatalf("create item mismatch: %+v", item)
	}

	// 只改简介，图片模式、评分不变。
	item, err = BannerSvc.Update(item.Id, BannerFields{Blurb: strPtr("自定义简介")})
	if err != nil {
		t.Fatalf("patch blurb: %v", err)
	}
	if item.Blurb != "自定义简介" || item.ImageMode != model.BannerImageFollow || item.Score != 7.5 || item.Area != "中国大陆" {
		t.Fatalf("patch blurb mismatch: %+v", item)
	}

	// 改成自定义封面，简介保持覆盖值。
	item, err = BannerSvc.Update(item.Id, BannerFields{ImageMode: strPtr("custom"), Poster: strPtr("http://custom.jpg"), Score: f64Ptr(9.1)})
	if err != nil {
		t.Fatalf("patch custom: %v", err)
	}
	if item.Picture != "http://custom.jpg" || !item.IsCustomPic || item.Blurb != "自定义简介" || item.Score != 9.1 ||
		item.PictureSlide != "http://img/1-slide.jpg" {
		t.Fatalf("patch custom mismatch: %+v", item)
	}

	// 清空简介覆盖回到片库值；恢复跟随时清掉覆盖图。
	item, err = BannerSvc.Update(item.Id, BannerFields{Blurb: strPtr(""), ImageMode: strPtr("follow")})
	if err != nil {
		t.Fatalf("patch reset: %v", err)
	}
	if item.Blurb != "片库简介" || item.IsCustomPic || item.Picture != "http://img/1.jpg" || item.Overrides.Poster != "" || item.Score != 9.1 {
		t.Fatalf("patch reset mismatch: %+v", item)
	}

	if _, err := BannerSvc.Update(item.Id, BannerFields{ImageMode: strPtr("pinned")}); !IsBannerUserError(err) {
		t.Fatalf("invalid image mode should be rejected, got %v", err)
	}
	if _, err := BannerSvc.Update("missing", BannerFields{}); !IsBannerUserError(err) {
		t.Fatalf("missing id should be a user error, got %v", err)
	}
}

func TestBannerCreate_Validation(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "站一影片", 10)
	seedBannerFilm(t, gdb, 2, "src2", 1, "站二影片", 10)

	if _, err := BannerSvc.Create("src1", BannerFields{}); !IsBannerUserError(err) {
		t.Fatalf("missing mid should fail, got %v", err)
	}
	if _, err := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(2)}); !IsBannerUserError(err) {
		t.Fatalf("film of another site should be rejected, got %v", err)
	}
	if _, err := BannerSvc.Create("nope", BannerFields{Mid: i64Ptr(1)}); !IsBannerUserError(err) {
		t.Fatalf("unknown site should be rejected, got %v", err)
	}
	if _, err := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(1)}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(1)}); !IsBannerUserError(err) {
		t.Fatalf("duplicate film should be rejected, got %v", err)
	}
}

func TestBanner_PerSiteIsolation(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "站一影片", 10)
	seedBannerFilm(t, gdb, 2, "src2", 1, "站二影片", 10)
	seedBannerFilm(t, gdb, 3, "src2", 1, "站二影片二", 5)
	if _, err := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := BannerSvc.Create("src2", BannerFields{Mid: i64Ptr(2)}); err != nil {
		t.Fatal(err)
	}

	// 不传站点时是首选站。
	view, err := BannerSvc.List("")
	if err != nil || view.SourceId != "src1" || fmt.Sprint(bannerMidList(view.Items)) != "[1]" {
		t.Fatalf("default list should be primary site: %v %+v", err, view)
	}
	view2, _ := BannerSvc.List("src2")
	if fmt.Sprint(bannerMidList(view2.Items)) != "[2]" {
		t.Fatalf("src2 list mismatch: %+v", view2.Items)
	}

	// 整组替换 src2 不影响 src1。
	if _, err := BannerSvc.PublishDeck("src2", view2.Revision, []BannerFields{{Mid: i64Ptr(3)}, {Mid: i64Ptr(2)}}); err != nil {
		t.Fatalf("publish src2: %v", err)
	}
	home1, _ := BannerSvc.HomeBanners("src1")
	home2, _ := BannerSvc.HomeBanners("src2")
	if len(home1) != 1 || home1[0].Mid != 1 || len(home2) != 2 || home2[0].Mid != 3 || home2[1].Mid != 2 {
		t.Fatalf("isolation broken: src1=%+v src2=%+v", home1, home2)
	}
	// 发布 src1 时不能带别站的影片。
	view1, _ := BannerSvc.List("src1")
	if _, err := BannerSvc.PublishDeck("src1", view1.Revision, []BannerFields{{Mid: i64Ptr(2)}}); !IsBannerUserError(err) {
		t.Fatalf("cross-site film in deck should be rejected, got %v", err)
	}
}

func TestBannerReorder_RevisionConflict(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "A", 10)
	seedBannerFilm(t, gdb, 2, "src1", 1, "B", 9)
	a, _ := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(1)})
	b, _ := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(2)})
	view, _ := BannerSvc.List("src1")
	stale := view.Revision

	next, err := BannerSvc.Reorder("src1", stale, []string{b.Id, a.Id})
	if err != nil {
		t.Fatalf("reorder: %v", err)
	}
	if fmt.Sprint(bannerMidList(next.Items)) != "[2 1]" || next.Revision == stale {
		t.Fatalf("reorder result mismatch: %+v", next)
	}
	if _, err := BannerSvc.Reorder("src1", stale, []string{a.Id, b.Id}); !IsBannerConflict(err) {
		t.Fatalf("stale revision should conflict, got %v", err)
	}
	if _, err := BannerSvc.Reorder("src1", next.Revision, []string{a.Id}); !IsBannerConflict(err) {
		t.Fatalf("ids not matching current rows should conflict, got %v", err)
	}
	// 单行编辑也会让版本号变化。
	if _, err := BannerSvc.Update(a.Id, BannerFields{Blurb: strPtr("x")}); err != nil {
		t.Fatal(err)
	}
	if _, err := BannerSvc.Reorder("src1", next.Revision, []string{a.Id, b.Id}); !IsBannerConflict(err) {
		t.Fatalf("revision should change after patch, got %v", err)
	}
}

func TestBannerDeck_RevisionConflict(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "A", 10)
	seedBannerFilm(t, gdb, 2, "src1", 1, "B", 9)
	a, _ := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(1)})
	b, _ := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(2)})
	view, _ := BannerSvc.List("src1")

	// 生成预览之后有人保存了顺序，再发布应当冲突。
	if _, err := BannerSvc.Reorder("src1", view.Revision, []string{b.Id, a.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := BannerSvc.PublishDeck("src1", view.Revision, []BannerFields{{Mid: i64Ptr(1)}}); !IsBannerConflict(err) {
		t.Fatalf("stale deck publish should conflict, got %v", err)
	}
	cur, _ := BannerSvc.List("src1")
	published, err := BannerSvc.PublishDeck("src1", cur.Revision, []BannerFields{
		{Mid: i64Ptr(2), ImageMode: strPtr("custom"), Poster: strPtr("http://tmdb.jpg"), TmdbID: i64Ptr(42)},
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(published.Items) != 1 || published.Items[0].Picture != "http://tmdb.jpg" || published.Items[0].TmdbID != 42 {
		t.Fatalf("published deck mismatch: %+v", published.Items)
	}
	if _, err := BannerSvc.PublishDeck("src1", published.Revision, []BannerFields{{Mid: i64Ptr(1)}, {Mid: i64Ptr(1)}}); !IsBannerUserError(err) {
		t.Fatalf("duplicate films in deck should be rejected, got %v", err)
	}
}

func TestBannerGenerate_Validation(t *testing.T) {
	setupBannerSvcDB(t)
	ok := []BannerQuota{{CategoryID: 1, Count: 1}}
	cases := []struct {
		name string
		req  BannerGenerateReq
	}{
		{"缺站点", BannerGenerateReq{Strategy: "hot", Quotas: ok}},
		{"站点不存在", BannerGenerateReq{SourceId: "nope", Strategy: "hot", Quotas: ok}},
		{"站点停用", BannerGenerateReq{SourceId: "src3", Strategy: "hot", Quotas: ok}},
		{"非法策略", BannerGenerateReq{SourceId: "src1", Strategy: "hot_random", Quotas: ok}},
		{"空配额", BannerGenerateReq{SourceId: "src1", Strategy: "hot"}},
		{"全为 0", BannerGenerateReq{SourceId: "src1", Strategy: "hot", Quotas: []BannerQuota{{CategoryID: 1, Count: 0}}}},
		{"数量越界", BannerGenerateReq{SourceId: "src1", Strategy: "hot", Quotas: []BannerQuota{{CategoryID: 1, Count: 100}}}},
		{"负数", BannerGenerateReq{SourceId: "src1", Strategy: "hot", Quotas: []BannerQuota{{CategoryID: 1, Count: -1}}}},
		{"分类编号非法", BannerGenerateReq{SourceId: "src1", Strategy: "hot", Quotas: []BannerQuota{{CategoryID: 0, Count: 1}}}},
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
	support.RefreshCategoryCache()
	seedBannerFilm(t, gdb, 11, "src1", 1, "电影一", 100)
	seedBannerFilm(t, gdb, 12, "src1", 1, "电影二", 90)
	seedBannerFilm(t, gdb, 21, "src1", 2, "剧一", 80)
	seedBannerFilm(t, gdb, 31, "src2", 1, "别站电影", 1000)

	res, err := BannerSvc.Generate(context.Background(), BannerGenerateReq{
		SourceId: "src1", Strategy: "hot",
		Quotas: []BannerQuota{{CategoryID: 1, Count: 2}, {CategoryID: 2, Count: 3}, {CategoryID: 99, Count: 1}},
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
	if slots, _ := repository.ListBannerSlots("src1"); len(slots) != 0 {
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
	if _, err := BannerSvc.PublishDeck("src1", res.Revision, items); err != nil {
		t.Fatalf("publish generated deck: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BannerSvc.Generate(ctx, BannerGenerateReq{SourceId: "src1", Strategy: "hot", Quotas: []BannerQuota{{CategoryID: 1, Count: 1}}}); err == nil {
		t.Fatalf("canceled generate should fail")
	}
}

func TestBannerCascadeDelete(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "站一影片", 10)
	seedBannerFilm(t, gdb, 2, "src1", 1, "站一影片二", 9)
	seedBannerFilm(t, gdb, 3, "src2", 1, "站二影片", 10)
	for _, c := range []struct {
		src string
		mid int64
	}{{"src1", 1}, {"src1", 2}, {"src2", 3}} {
		if _, err := BannerSvc.Create(c.src, BannerFields{Mid: i64Ptr(c.mid)}); err != nil {
			t.Fatal(err)
		}
	}
	count := func(where string, args ...any) int64 {
		var n int64
		gdb.Model(&model.BannerSlot{}).Where(where, args...).Count(&n)
		return n
	}

	// 后台删影片：同事务删轮播。
	if err := filmrepo.DelFilmSearchBatch([]int64{1}); err != nil {
		t.Fatalf("delete film: %v", err)
	}
	if count("mid = ?", 1) != 0 || count("mid = ?", 2) != 1 {
		t.Fatalf("deleting film should delete only its banner")
	}

	// 定时清理空片：同样删轮播。
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 2).Update("name", "")
	if cleaned := filmrepo.CleanEmptyFilms(); cleaned == 0 {
		t.Fatalf("expected empty film cleaned")
	}
	if count("mid = ?", 2) != 0 {
		t.Fatalf("cleaning empty film should delete its banner")
	}
	seedBannerFilm(t, gdb, 4, "src1", 1, "站一影片三", 8)
	if _, err := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(4)}); err != nil {
		t.Fatal(err)
	}

	// 清空采集站数据：该站轮播整组删除，别站不动。
	if _, err := repository.CascadeCleanSourceData("src1"); err != nil {
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
	a, _ := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(1)})
	b, _ := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(2)})
	c, _ := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(3)})

	// 2 在本站没线路了（档案还在），3 档案没了但轮播行还在（例如导入的旧数据）。
	gdb.Where("mid = ? AND source_id = ?", 2, "src1").Delete(&model.FilmSourcePlaylist{})
	gdb.Where("mid = ?", 3).Delete(&model.FilmIndex{})

	home, err := BannerSvc.HomeBanners("src1")
	if err != nil || len(home) != 1 || home[0].Mid != 1 || home[0].Sort != 1 {
		t.Fatalf("home should only show films playable on this site: %v %+v", err, home)
	}

	view, err := BannerSvc.List("src1")
	if err != nil || len(view.Items) != 3 {
		t.Fatalf("admin list should keep all rows: %v %+v", err, view.Items)
	}
	if view.Items[0].Unavailable || !view.Items[1].Unavailable || !view.Items[2].Unavailable {
		t.Fatalf("unavailable flags mismatch: %+v", view.Items)
	}

	next, err := BannerSvc.Reorder("src1", view.Revision, []string{c.Id, a.Id, b.Id})
	if err != nil {
		t.Fatalf("reorder with unavailable rows should succeed: %v", err)
	}
	if fmt.Sprint(bannerMidList(next.Items)) != "[3 1 2]" {
		t.Fatalf("reorder result = %v", bannerMidList(next.Items))
	}
	if err := BannerSvc.Delete(c.Id); err != nil {
		t.Fatalf("delete unavailable row: %v", err)
	}
	if err := BannerSvc.Delete(c.Id); !IsBannerUserError(err) {
		t.Fatalf("deleting twice should be not found, got %v", err)
	}
	after, _ := BannerSvc.List("src1")
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
	item, err := BannerSvc.Create("src1", BannerFields{
		Mid: i64Ptr(1), ImageMode: strPtr("custom"), Poster: strPtr("http://old.jpg"), Backdrop: strPtr("http://old-slide.jpg"),
	})
	if err != nil || !item.IsCustomPic {
		t.Fatalf("create custom: %v %+v", err, item)
	}
	item, err = BannerSvc.Update(item.Id, BannerFields{Mid: i64Ptr(2)})
	if err != nil {
		t.Fatalf("change film: %v", err)
	}
	if item.Mid != 2 || item.ImageMode != model.BannerImageFollow || item.IsCustomPic ||
		item.Overrides.Poster != "" || item.Overrides.Backdrop != "" || item.Picture != "http://img/2.jpg" {
		t.Fatalf("changing film should reset images: %+v", item)
	}
	// 换片时一起提交了新图片，则保留新图片。
	item, err = BannerSvc.Update(item.Id, BannerFields{Mid: i64Ptr(3), ImageMode: strPtr("custom"), Poster: strPtr("http://new.jpg")})
	if err != nil {
		t.Fatalf("change film with image: %v", err)
	}
	if item.Mid != 3 || !item.IsCustomPic || item.Picture != "http://new.jpg" {
		t.Fatalf("submitted image should be kept: %+v", item)
	}
}

func TestBannerGenerate_PerCategoryLimit(t *testing.T) {
	setupBannerSvcDB(t)
	quotas := []BannerQuota{{CategoryID: 1, Count: bannerQuotaMax + 1}}
	if _, err := BannerSvc.Generate(context.Background(), BannerGenerateReq{SourceId: "src1", Strategy: "hot", Quotas: quotas}); !IsBannerUserError(err) {
		t.Fatalf("generate per-category over limit should be rejected, got %v", err)
	}
}

func TestGetNavCategory_RejectsUnknownOrDisabledSource(t *testing.T) {
	setupBannerSvcDB(t)
	if _, err := IndexSvc.GetNavCategory("nope"); !errors.Is(err, ErrNavCategorySourceUnavailable) {
		t.Fatalf("unknown source should be rejected, got %v", err)
	}
	if _, err := IndexSvc.GetNavCategory("src3"); !errors.Is(err, ErrNavCategorySourceUnavailable) {
		t.Fatalf("disabled source should be rejected, got %v", err)
	}
	if _, err := IndexSvc.GetNavCategory("src1"); err != nil {
		t.Fatalf("enabled source should pass, got %v", err)
	}
}

// 片库查询出错时 HomeBanners 返回 error，不当成空轮播。
func TestHomeBanners_ReturnsErrorOnLibraryFailure(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "A", 10)
	if _, err := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(1)}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Migrator().DropTable(&model.FilmIndex{}); err != nil {
		t.Fatal(err)
	}
	if _, err := BannerSvc.HomeBanners("src1"); err == nil {
		t.Fatalf("library failure should surface as error")
	}
}
