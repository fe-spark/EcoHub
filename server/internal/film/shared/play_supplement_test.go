package shared

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestLoadPeerPlaylists_PicksStrongestEnabledFilm(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.FilmIndex{}, &model.MovieMatchKey{}, &model.FilmSourcePlaylist{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	gdb.Create(&model.FilmSource{Id: "src-a", Name: "甲", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src-b", Name: "乙", Uri: "http://b", State: true, Sort: 1})
	gdb.Create(&model.FilmSource{Id: "src-off", Name: "停", Uri: "http://off", State: false, Sort: 2})

	strong := BuildMovieMatchKeysWithCategory(36117912, "仙逆", 1)
	title := BuildMovieMatchKeysWithCategory(0, "仙逆", 0)
	films := []model.FilmIndex{
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src-a", DbId: 36117912}, FilmIndexContent: model.FilmIndexContent{Name: "仙逆"}, FilmIndexCategory: model.FilmIndexCategory{Pid: 1}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src-b", DbId: 36117912}, FilmIndexContent: model.FilmIndexContent{Name: "仙逆"}, FilmIndexCategory: model.FilmIndexCategory{Pid: 1}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 4, FirstSourceId: "src-b"}, FilmIndexContent: model.FilmIndexContent{Name: "仙逆"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "src-off", DbId: 36117912}, FilmIndexContent: model.FilmIndexContent{Name: "仙逆"}, FilmIndexCategory: model.FilmIndexCategory{Pid: 1}},
	}
	for _, film := range films {
		if err := gdb.Create(&film).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range strong {
		gdb.Create(&model.MovieMatchKey{Mid: 1, MatchKey: key})
		gdb.Create(&model.MovieMatchKey{Mid: 2, MatchKey: key})
		gdb.Create(&model.MovieMatchKey{Mid: 3, MatchKey: key})
	}
	gdb.Create(&model.MovieMatchKey{Mid: 4, MatchKey: title[0]})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 2, SourceId: "src-b", LineKind: "play", GroupIndex: 0, GroupName: "正片", Content: `[{"episode":"1","link":"http://b/1"}]`})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 2, SourceId: "src-b", LineKind: "play", GroupIndex: 1, GroupName: "备用", Content: `[{"episode":"2","link":"http://b/2"}]`})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 4, SourceId: "src-b", LineKind: "play", GroupIndex: 0, Content: `[{"episode":"弱","link":"http://b/weak"}]`})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 3, SourceId: "src-off", LineKind: "play", GroupIndex: 0, Content: `[{"episode":"停","link":"http://off"}]`})

	got, err := LoadPeerPlaylists(gdb, []model.FilmIndex{films[0]})
	if err != nil {
		t.Fatal(err)
	}
	lines := got[1]
	if len(lines) != 2 {
		t.Fatalf("lines=%d, want the strong film's two lines", len(lines))
	}
	for _, line := range lines {
		if line.Mid != 1 || line.SourceId != "src-b" {
			t.Fatalf("line mid=%d source=%s", line.Mid, line.SourceId)
		}
		if line.GroupName == "" && line.GroupIndex == 0 {
			t.Fatal("weak title film was chosen")
		}
	}
}

func TestLoadPeerPlaylists_DoesNotCrossCategoryWithoutSameDbID(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.FilmIndex{}, &model.MovieMatchKey{}, &model.FilmSourcePlaylist{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	gdb.Create(&model.FilmSource{Id: "src-tv", Name: "剧版站", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src-anime", Name: "动漫站", State: true, Sort: 1})

	// 模拟凡人修仙传：同名，但电视剧 Pid=2，动漫 Pid=4，且无相同豆瓣 ID
	tvKeys := BuildMovieMatchKeysWithCategory(0, "凡人修仙传", 2)
	animeKeys := BuildMovieMatchKeysWithCategory(0, "凡人修仙传", 4)

	filmTV := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101, FirstSourceId: "src-tv", DbId: 0},
		FilmIndexContent:  model.FilmIndexContent{Name: "凡人修仙传"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 2},
	}
	filmAnime := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 102, FirstSourceId: "src-anime", DbId: 0},
		FilmIndexContent:  model.FilmIndexContent{Name: "凡人修仙传"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 4},
	}
	gdb.Create(&filmTV)
	gdb.Create(&filmAnime)

	for _, k := range tvKeys {
		gdb.Create(&model.MovieMatchKey{Mid: 101, MatchKey: k})
	}
	for _, k := range animeKeys {
		gdb.Create(&model.MovieMatchKey{Mid: 102, MatchKey: k})
	}

	gdb.Create(&model.FilmSourcePlaylist{Mid: 102, SourceId: "src-anime", LineKind: "play", GroupIndex: 0, GroupName: "动漫正片", Content: `[{"episode":"1","link":"http://anime/1"}]`})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 101, SourceId: "src-tv", LineKind: "play", GroupIndex: 0, GroupName: "电视剧正片", Content: `[{"episode":"1","link":"http://tv/1"}]`})

	// 查电视剧的关联线路，不应串入动漫线路
	gotTV, err := LoadPeerPlaylists(gdb, []model.FilmIndex{filmTV})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotTV[101]) != 0 {
		t.Fatalf("expected no peer lines from anime for tv series, got %d lines", len(gotTV[101]))
	}

	// 查动漫的关联线路，不应串入电视剧线路
	gotAnime, err := LoadPeerPlaylists(gdb, []model.FilmIndex{filmAnime})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotAnime[102]) != 0 {
		t.Fatalf("expected no peer lines from tv series for anime, got %d lines", len(gotAnime[102]))
	}
}

func TestLoadPeerPlaylistsWithFilter_SkipsHiddenCategory(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.FilmIndex{}, &model.MovieMatchKey{}, &model.FilmSourcePlaylist{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	gdb.Create(&model.FilmSource{Id: "src-a", Name: "主站", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src-b", Name: "副站", Uri: "http://b", State: true, Sort: 1})

	keys := BuildMovieMatchKeysWithCategory(555, "隐藏分类测试", 1)
	filmA := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src-a", DbId: 555},
		FilmIndexContent:  model.FilmIndexContent{Name: "隐藏分类测试"},
		FilmIndexCategory: model.FilmIndexCategory{Cid: 1, Pid: 1, CategoryKey: "source:src-a:1"},
	}
	filmB := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src-b", DbId: 555},
		FilmIndexContent:  model.FilmIndexContent{Name: "隐藏分类测试"},
		FilmIndexCategory: model.FilmIndexCategory{Cid: 22, Pid: 20, CategoryKey: "source:src-b:12", RootCategoryKey: "source:src-b:2"},
	}
	gdb.Create(&filmA)
	gdb.Create(&filmB)
	for _, k := range keys {
		gdb.Create(&model.MovieMatchKey{Mid: 1, MatchKey: k})
		gdb.Create(&model.MovieMatchKey{Mid: 2, MatchKey: k})
	}
	gdb.Create(&model.FilmSourcePlaylist{Mid: 2, SourceId: "src-b", LineKind: "play", GroupIndex: 0, Content: `[{"episode":"1","link":"http://b/1"}]`})

	got, err := LoadPeerPlaylistsWithFilter(gdb, []model.FilmIndex{filmA}, func(sourceID, categoryKey, rootCategoryKey string, cid, pid int64) bool {
		return !(sourceID == "src-b" && categoryKey == "source:src-b:12")
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got[1]) != 0 {
		t.Fatalf("expected hidden category peer playlist to be excluded, got %+v", got[1])
	}
}

func TestLoadPeerPlaylists_AllowsDifferentLocalPidsWithSameSemanticBigCategory(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.Category{}, &model.MappingRule{}, &model.CategoryMapping{}, &model.SourceCategory{}, &model.FilmIndex{}, &model.MovieMatchKey{}, &model.FilmSourcePlaylist{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	prevSnap := repobase.CurrentMappingSnapshotForTest()
	t.Cleanup(func() {
		db.Mdb = prev
		repobase.StoreMappingSnapshotForTest(prevSnap)
		repobase.ResetCategoryNameCache()
		repobase.ClearRootCategoryCNameCache()
	})

	for _, src := range []model.FilmSource{
		{Id: "src-duck", Name: "鸭鸭", Uri: "http://duck.test", State: true, Sort: 0},
		{Id: "src-tianya", Name: "天涯影视", Uri: "http://tianya.test", State: true, Sort: 1},
		{Id: "src-anime", Name: "动漫站", Uri: "http://anime.test", State: true, Sort: 2},
		{Id: "src-short", Name: "短剧站", Uri: "http://short.test", State: true, Sort: 3},
		{Id: "src-custom", Name: "自定义站", Uri: "http://custom.test", State: true, Sort: 4},
	} {
		if err := gdb.Create(&src).Error; err != nil {
			t.Fatal(err)
		}
	}

	// 各自源独立建档：本地 Pid 完全不同，但语义同属「电视剧」与不同大类「动漫」「短剧」。
	for _, cat := range []model.Category{
		{Id: 353, Pid: 0, Name: "电视剧", StableKey: "source:src-duck:2"},
		{Id: 266, Pid: 0, Name: "连续剧", StableKey: "source:src-tianya:2"},
		{Id: 412, Pid: 0, Name: "动漫", StableKey: "source:src-anime:4"},
		{Id: 518, Pid: 0, Name: "微短剧", StableKey: "source:src-short:5"},
		{Id: 601, Pid: 0, Name: "怀旧剧场", StableKey: "source:src-custom:6"},
	} {
		if err := gdb.Create(&cat).Error; err != nil {
			t.Fatal(err)
		}
	}

	// 测试 CategoryRoot 映射规则同样生效：把「怀旧剧场」规整为「电视剧」。
	if err := gdb.Create(&model.MappingRule{SourceId: "src-custom", Group: "CategoryRoot", Raw: "怀旧剧场", Target: "电视剧", MatchType: "exact"}).Error; err != nil {
		t.Fatal(err)
	}
	repobase.ReloadMappingRules()

	filmDuck := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src-duck", DbId: 4303597},
		FilmIndexContent:  model.FilmIndexContent{Name: "乡村爱情3"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 353},
	}
	filmTianya := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src-tianya", DbId: 0},
		FilmIndexContent:  model.FilmIndexContent{Name: "乡村爱情3"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 266},
	}
	filmAnime := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "src-anime", DbId: 0},
		FilmIndexContent:  model.FilmIndexContent{Name: "乡村爱情3"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 412},
	}
	filmShort := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 4, FirstSourceId: "src-short", DbId: 0},
		FilmIndexContent:  model.FilmIndexContent{Name: "乡村爱情3"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 518},
	}
	filmCustom := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 5, FirstSourceId: "src-custom", DbId: 0},
		FilmIndexContent:  model.FilmIndexContent{Name: "乡村爱情3"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 601},
	}
	for _, film := range []model.FilmIndex{filmDuck, filmTianya, filmAnime, filmShort, filmCustom} {
		gdb.Create(&film)
		for _, k := range BuildMovieMatchKeysWithCategory(film.DbId, film.Name, film.Pid) {
			gdb.Create(&model.MovieMatchKey{Mid: film.Mid, MatchKey: k})
		}
	}

	gdb.Create(&model.FilmSourcePlaylist{Mid: 2, SourceId: "src-tianya", LineKind: "play", GroupIndex: 0, GroupName: "天涯", Content: `[{"episode":"1","link":"http://tianya/1"}]`})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 3, SourceId: "src-anime", LineKind: "play", GroupIndex: 0, GroupName: "动漫", Content: `[{"episode":"1","link":"http://anime/1"}]`})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 4, SourceId: "src-short", LineKind: "play", GroupIndex: 0, GroupName: "短剧", Content: `[{"episode":"1","link":"http://short/1"}]`})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 5, SourceId: "src-custom", LineKind: "play", GroupIndex: 0, GroupName: "自定义", Content: `[{"episode":"1","link":"http://custom/1"}]`})

	got, err := LoadPeerPlaylists(gdb, []model.FilmIndex{filmDuck})
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{}
	for _, line := range got[1] {
		sources[line.SourceId] = true
	}
	if !sources["src-tianya"] {
		t.Fatalf("expected src-tianya (Pid=266 连续剧) to match src-duck (Pid=353 电视剧), got %+v", got[1])
	}
	if !sources["src-custom"] {
		t.Fatalf("expected src-custom (mapped 电视剧) to match src-duck, got %+v", got[1])
	}
	if sources["src-anime"] {
		t.Fatalf("did not expect src-anime (动漫) to cross-match 电视剧, got %+v", got[1])
	}
	if sources["src-short"] {
		t.Fatalf("did not expect src-short (微短剧) to cross-match 电视剧, got %+v", got[1])
	}
}

func TestNormalizeCanonicalBigCategory_SuffixIsNotBigCategory(t *testing.T) {
	if got := normalizeCanonicalBigCategory("伦理片"); got == model.BigCategoryMovie {
		t.Fatalf("伦理片 must not become 电影, got %s", got)
	}
	if got := normalizeCanonicalBigCategory("音乐剧"); got == model.BigCategoryTV {
		t.Fatalf("音乐剧 must not become 电视剧, got %s", got)
	}
	if got := normalizeCanonicalBigCategory("电视剧"); got != model.BigCategoryTV {
		t.Fatalf("电视剧: got %s", got)
	}
	if got := normalizeCanonicalBigCategory("连续剧"); got != model.BigCategoryTV {
		t.Fatalf("连续剧: got %s", got)
	}
	if got := normalizeCanonicalBigCategory("电影"); got != model.BigCategoryMovie {
		t.Fatalf("电影: got %s", got)
	}
}

func TestLoadPeerPlaylists_MissingCategoryNameDoesNotMerge(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.FilmIndex{}, &model.MovieMatchKey{}, &model.FilmSourcePlaylist{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	for _, src := range []model.FilmSource{
		{Id: "src-home", Name: "主站", Uri: "http://home.test", State: true, Sort: 0},
		{Id: "src-douban", Name: "豆瓣站", Uri: "http://douban.test", State: true, Sort: 1},
		{Id: "src-anime", Name: "动漫站", Uri: "http://anime.test", State: true, Sort: 2},
		{Id: "src-blank", Name: "缺分类站", Uri: "http://blank.test", State: true, Sort: 3},
	} {
		if err := gdb.Create(&src).Error; err != nil {
			t.Fatal(err)
		}
	}

	films := []model.FilmIndex{
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src-home", DbId: 88},
			FilmIndexContent:  model.FilmIndexContent{Name: "仙逆"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src-douban", DbId: 88},
			FilmIndexContent:  model.FilmIndexContent{Name: "仙逆"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 77},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "src-anime", DbId: 0},
			FilmIndexContent:  model.FilmIndexContent{Name: "仙逆"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 4, OriginalCategory: "动漫"},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 4, FirstSourceId: "src-blank", DbId: 0},
			FilmIndexContent:  model.FilmIndexContent{Name: "仙逆"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 99},
		},
	}
	for _, film := range films {
		if err := gdb.Create(&film).Error; err != nil {
			t.Fatal(err)
		}
		for _, k := range BuildMovieMatchKeysWithCategory(film.DbId, film.Name, film.Pid) {
			if err := gdb.Create(&model.MovieMatchKey{Mid: film.Mid, MatchKey: k}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, line := range []model.FilmSourcePlaylist{
		{Mid: 2, SourceId: "src-douban", LineKind: "play", GroupIndex: 0, Content: `[{"episode":"1","link":"http://douban/1"}]`},
		{Mid: 3, SourceId: "src-anime", LineKind: "play", GroupIndex: 0, Content: `[{"episode":"1","link":"http://anime/1"}]`},
		{Mid: 4, SourceId: "src-blank", LineKind: "play", GroupIndex: 0, Content: `[{"episode":"1","link":"http://blank/1"}]`},
	} {
		if err := gdb.Create(&line).Error; err != nil {
			t.Fatal(err)
		}
	}

	gotAnime, err := LoadPeerPlaylists(gdb, []model.FilmIndex{films[2]})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotAnime[3]) != 0 {
		t.Fatalf("missing category must not merge by title, got %+v", gotAnime[3])
	}

	gotHome, err := LoadPeerPlaylists(gdb, []model.FilmIndex{films[0]})
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{}
	for _, line := range gotHome[1] {
		sources[line.SourceId] = true
	}
	if !sources["src-douban"] {
		t.Fatalf("same douban id should still merge, got %+v", gotHome[1])
	}
	if sources["src-anime"] || sources["src-blank"] {
		t.Fatalf("different or missing category must not merge without the same douban id, got %+v", gotHome[1])
	}
}
