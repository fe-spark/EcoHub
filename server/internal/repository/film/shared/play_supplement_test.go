package shared

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestLoadPeerPlaylists_PicksStrongestEnabledFilm(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
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
