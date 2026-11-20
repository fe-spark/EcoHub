package shared

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
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
