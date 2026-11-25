package banner

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"

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
		&model.PrimaryState{},
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
			RootCategoryKey: taxonomy.BuildSourceCategoryKey(source, typeID),
			CategoryKey:     taxonomy.BuildSourceCategoryKey(source, typeID),
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
	if _, err := BannerSvc.Create("", BannerFields{Mid: i64Ptr(1), TmdbID: i64Ptr(5)}); err != nil {
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
	item, err := BannerSvc.Create("", BannerFields{Mid: i64Ptr(1)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if item.ImageMode != model.BannerImageFollow || item.Blurb != "片库简介" || item.Library.Poster != "http://img/1.jpg" {
		t.Fatalf("create item mismatch: %+v", item)
	}

	// 只改简介，图片模式、评分不变。
	item, err = BannerSvc.Update(item.Id, "src1", BannerFields{Blurb: strPtr("自定义简介")})
	if err != nil {
		t.Fatalf("patch blurb: %v", err)
	}
	if item.Blurb != "自定义简介" || item.ImageMode != model.BannerImageFollow || item.Score != 7.5 || item.Area != "中国大陆" {
		t.Fatalf("patch blurb mismatch: %+v", item)
	}

	// 改成自定义封面，简介保持覆盖值。
	item, err = BannerSvc.Update(item.Id, "src1", BannerFields{ImageMode: strPtr("custom"), Poster: strPtr("http://custom.jpg"), Score: f64Ptr(9.1)})
	if err != nil {
		t.Fatalf("patch custom: %v", err)
	}
	if item.Picture != "http://custom.jpg" || !item.IsCustomPic || item.Blurb != "自定义简介" || item.Score != 9.1 ||
		item.PictureSlide != "http://img/1-slide.jpg" {
		t.Fatalf("patch custom mismatch: %+v", item)
	}

	// 清空简介覆盖回到片库值；恢复跟随时清掉覆盖图。
	item, err = BannerSvc.Update(item.Id, "src1", BannerFields{Blurb: strPtr(""), ImageMode: strPtr("follow")})
	if err != nil {
		t.Fatalf("patch reset: %v", err)
	}
	if item.Blurb != "片库简介" || item.IsCustomPic || item.Picture != "http://img/1.jpg" || item.Overrides.Poster != "" || item.Score != 9.1 {
		t.Fatalf("patch reset mismatch: %+v", item)
	}

	if _, err := BannerSvc.Update(item.Id, "src1", BannerFields{ImageMode: strPtr("pinned")}); !IsBannerUserError(err) {
		t.Fatalf("invalid image mode should be rejected, got %v", err)
	}
	if _, err := BannerSvc.Update("missing", "src1", BannerFields{}); !IsBannerUserError(err) {
		t.Fatalf("missing id should be a user error, got %v", err)
	}
}

func TestBannerCreate_Validation(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "站一影片", 10)
	seedBannerFilm(t, gdb, 2, "src2", 1, "站二影片", 10)

	if _, err := BannerSvc.Create("", BannerFields{}); !IsBannerUserError(err) {
		t.Fatalf("missing mid should fail, got %v", err)
	}
	if _, err := BannerSvc.Create("", BannerFields{Mid: i64Ptr(2)}); !IsBannerUserError(err) {
		t.Fatalf("film of another site should be rejected, got %v", err)
	}
	if _, err := BannerSvc.Create("src2", BannerFields{Mid: i64Ptr(1)}); !IsBannerUserError(err) {
		t.Fatalf("film not on src2 should be rejected, got %v", err)
	}
	if _, err := BannerSvc.Create("missing", BannerFields{Mid: i64Ptr(1)}); !IsBannerUserError(err) {
		t.Fatalf("unknown source should be rejected, got %v", err)
	}
	if _, err := BannerSvc.Create("", BannerFields{Mid: i64Ptr(1)}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := BannerSvc.Create("", BannerFields{Mid: i64Ptr(1)}); !IsBannerUserError(err) {
		t.Fatalf("duplicate film should be rejected, got %v", err)
	}
	if _, err := BannerSvc.Create("src2", BannerFields{Mid: i64Ptr(2)}); err != nil {
		t.Fatalf("create on non-primary source: %v", err)
	}
}

// 按采集站切换管理轮播：各站独立读写、互不串站，且首页仍按站读取。
func TestBanner_SourceSwitchIsolation(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "站一影片", 10)
	seedBannerFilm(t, gdb, 2, "src2", 1, "站二影片", 10)
	if _, err := BannerSvc.Create("", BannerFields{Mid: i64Ptr(1)}); err != nil {
		t.Fatal(err)
	}
	src2Item, err := BannerSvc.Create("src2", BannerFields{Mid: i64Ptr(2)})
	if err != nil {
		t.Fatalf("create on src2: %v", err)
	}

	view1, err := BannerSvc.List("")
	if err != nil || view1.SourceId != "src1" || view1.PrimarySourceId != "src1" || len(view1.Sources) != 3 || fmt.Sprint(bannerMidList(view1.Items)) != "[1]" {
		t.Fatalf("list default primary mismatch: %v %+v", err, view1)
	}
	view2, err := BannerSvc.List("src2")
	if err != nil || view2.SourceId != "src2" || view2.PrimarySourceId != "src1" || fmt.Sprint(bannerMidList(view2.Items)) != "[2]" {
		t.Fatalf("list src2 mismatch: %v %+v", err, view2)
	}

	// 对象级写传错 sourceId 时拒绝，传入正确 sourceId 时放行。
	if _, err := BannerSvc.Update(src2Item.Id, "src1", BannerFields{Blurb: strPtr("x")}); !IsBannerUserError(err) {
		t.Fatalf("cross-site update should be rejected, got %v", err)
	}
	if err := BannerSvc.Delete(src2Item.Id, "src1"); !IsBannerUserError(err) {
		t.Fatalf("cross-site delete should be rejected, got %v", err)
	}
	if updated, err := BannerSvc.Update(src2Item.Id, "src2", BannerFields{Blurb: strPtr("站二简介")}); err != nil || updated.Blurb != "站二简介" {
		t.Fatalf("update src2 row: %v %+v", err, updated)
	}

	// 排片只能用对应采集站的影片。
	if _, err := BannerSvc.PublishDeck("src2", view2.Revision, []BannerFields{{Mid: i64Ptr(1)}}); !IsBannerUserError(err) {
		t.Fatalf("cross-site film in deck should be rejected, got %v", err)
	}

	home1, _ := BannerSvc.HomeBanners("src1")
	if len(home1) != 1 || home1[0].Mid != 1 {
		t.Fatalf("primary home = %+v", home1)
	}
	home2, _ := BannerSvc.HomeBanners("src2")
	if len(home2) != 1 || home2[0].Mid != 2 || home2[0].Blurb != "站二简介" {
		t.Fatalf("src2 home = %+v", home2)
	}

	if err := BannerSvc.Delete(src2Item.Id, "src2"); err != nil {
		t.Fatalf("delete src2 row: %v", err)
	}
	after2, _ := BannerSvc.List("src2")
	if len(after2.Items) != 0 {
		t.Fatalf("src2 banner should be deleted")
	}
	after1, _ := BannerSvc.List("src1")
	if len(after1.Items) != 1 {
		t.Fatalf("deleting src2 banner must not affect src1")
	}
}

func TestBannerReorder_RevisionConflict(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "A", 10)
	seedBannerFilm(t, gdb, 2, "src1", 1, "B", 9)
	a, _ := BannerSvc.Create("", BannerFields{Mid: i64Ptr(1)})
	b, _ := BannerSvc.Create("", BannerFields{Mid: i64Ptr(2)})
	view, _ := BannerSvc.List("")
	stale := view.Revision

	next, err := BannerSvc.Reorder("", stale, []string{b.Id, a.Id})
	if err != nil {
		t.Fatalf("reorder: %v", err)
	}
	if fmt.Sprint(bannerMidList(next.Items)) != "[2 1]" || next.Revision == stale {
		t.Fatalf("reorder result mismatch: %+v", next)
	}
	if _, err := BannerSvc.Reorder("src2", next.Revision, []string{a.Id, b.Id}); !IsBannerConflict(err) {
		t.Fatalf("reorder on src2 with src1 ids should conflict, got %v", err)
	}
	if _, err := BannerSvc.Reorder("", stale, []string{a.Id, b.Id}); !IsBannerConflict(err) {
		t.Fatalf("stale revision should conflict, got %v", err)
	}
	if _, err := BannerSvc.Reorder("", next.Revision, []string{a.Id}); !IsBannerConflict(err) {
		t.Fatalf("ids not matching current rows should conflict, got %v", err)
	}
	// 单行编辑也会让版本号变化。
	if _, err := BannerSvc.Update(a.Id, "src1", BannerFields{Blurb: strPtr("x")}); err != nil {
		t.Fatal(err)
	}
	if _, err := BannerSvc.Reorder("", next.Revision, []string{a.Id, b.Id}); !IsBannerConflict(err) {
		t.Fatalf("revision should change after patch, got %v", err)
	}
}

func TestBannerDeck_RevisionConflict(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	seedBannerFilm(t, gdb, 1, "src1", 1, "A", 10)
	seedBannerFilm(t, gdb, 2, "src1", 1, "B", 9)
	a, _ := BannerSvc.Create("", BannerFields{Mid: i64Ptr(1)})
	b, _ := BannerSvc.Create("", BannerFields{Mid: i64Ptr(2)})
	view, _ := BannerSvc.List("")

	// 生成预览之后有人保存了顺序，再发布应当冲突。
	if _, err := BannerSvc.Reorder("", view.Revision, []string{b.Id, a.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := BannerSvc.PublishDeck("", view.Revision, []BannerFields{{Mid: i64Ptr(1)}}); !IsBannerConflict(err) {
		t.Fatalf("stale deck publish should conflict, got %v", err)
	}
	cur, _ := BannerSvc.List("")
	published, err := BannerSvc.PublishDeck("", cur.Revision, []BannerFields{
		{Mid: i64Ptr(2), ImageMode: strPtr("custom"), Poster: strPtr("http://tmdb.jpg"), TmdbID: i64Ptr(42)},
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(published.Items) != 1 || published.Items[0].Picture != "http://tmdb.jpg" || published.Items[0].TmdbID != 42 {
		t.Fatalf("published deck mismatch: %+v", published.Items)
	}
	if _, err := BannerSvc.PublishDeck("", published.Revision, []BannerFields{{Mid: i64Ptr(1)}, {Mid: i64Ptr(1)}}); !IsBannerUserError(err) {
		t.Fatalf("duplicate films in deck should be rejected, got %v", err)
	}
}
