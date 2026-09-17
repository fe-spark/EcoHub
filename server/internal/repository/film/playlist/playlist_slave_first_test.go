package playlist

import (
	"testing"

	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func TestSlaveFirstCollect_ViewableAfterMasterCollected(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{20: 0}, map[int64]string{20: model.BigCategoryAnimation})

	slave := model.MovieDetail{
		Name:            "仙逆",
		MovieDescriptor: model.MovieDescriptor{CName: "动漫", DbId: 12345},
		PlayList: [][]model.MovieUrlInfo{{
			{Episode: "第1集", Link: "https://subo/1.m3u8"},
			{Episode: "第12集", Link: "https://subo/12.m3u8"},
		}},
	}
	if _, err := SaveSitePlayList("subo", []model.MovieDetail{slave}); err != nil {
		t.Fatalf("slave-first save: %v", err)
	}

	dbKey := shared.BuildMovieMatchKeysWithCategory(12345, "仙逆", 20)[0]
	catKey := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	written := loadPlaylistKeys(t, gdb, "subo")
	if !written[dbKey] || !written[catKey] {
		t.Fatalf("slave-first write should cover candidate keys %s/%s, got %v", dbKey, catKey, written)
	}

	masterKeys := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 20)
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 901, ContentKey: "vod_901", SourceId: "master"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 20, CName: "动漫"},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙逆"},
	}).Error; err != nil {
		t.Fatalf("create master film: %v", err)
	}
	for _, key := range masterKeys {
		if err := gdb.Create(&model.MovieMatchKey{Mid: 901, MatchKey: key}).Error; err != nil {
			t.Fatalf("create master match key: %v", err)
		}
	}

	groups := GetMultiplePlayGroupsBySourcesAndKeys(
		[]model.FilmSource{{Id: "subo", Name: "速博"}},
		masterKeys,
	)
	got := groups["subo"]
	if len(got) != 1 || len(got[0].LinkList) != 2 {
		t.Fatalf("detail page must show slave episodes collected before the master, got %+v", got)
	}
	if last := got[0].LinkList[1].Episode; last != "第12集" {
		t.Fatalf("detail page must show latest slave episode, got %s", last)
	}
}

func TestSlaveFirstCollect_KeepsCategoryIsolation(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{20: 0}, map[int64]string{20: model.BigCategoryAnimation})

	slave := model.MovieDetail{
		Name:            "同名剧",
		MovieDescriptor: model.MovieDescriptor{CName: "动漫"},
		PlayList: [][]model.MovieUrlInfo{{
			{Episode: "第1集", Link: "https://subo/1.m3u8"},
		}},
	}
	if _, err := SaveSitePlayList("subo", []model.MovieDetail{slave}); err != nil {
		t.Fatalf("slave-first save: %v", err)
	}

	catKey := shared.BuildMovieMatchKeysWithCategory(0, "同名剧", 20)[0]
	plainNameKey := shared.BuildMovieMatchKeys(0, "同名剧")[0]
	written := loadPlaylistKeys(t, gdb, "subo")
	if len(written) != 1 || !written[catKey] {
		t.Fatalf("categorized slave detail must only use category-scoped keys, got %v", written)
	}
	if written[plainNameKey] {
		t.Fatalf("categorized slave detail must not write the bare title key %s", plainNameKey)
	}
}

func loadPlaylistKeys(t *testing.T, gdb *gorm.DB, sourceID string) map[string]bool {
	t.Helper()
	var rows []model.SlaveMoviePlaylist
	if err := gdb.Where("source_id = ?", sourceID).Find(&rows).Error; err != nil {
		t.Fatalf("load slave rows: %v", err)
	}
	keys := make(map[string]bool, len(rows))
	for _, row := range rows {
		keys[row.MovieKey] = true
	}
	return keys
}
