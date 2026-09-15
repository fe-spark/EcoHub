package film

import (
	"encoding/json"
	"testing"

	"server/internal/model"
	"server/internal/repository/support"
)

func TestSelectBestPlayGroupsPrefersMoreEpisodes(t *testing.T) {
	byKey := map[string][]model.SlaveMoviePlaylist{
		"dbid": {{
			MovieKey:   "dbid",
			GroupIndex: 0,
			Content:    `[{"episode":"第1集","link":"http://a/1.m3u8"},{"episode":"第8集","link":"http://a/8.m3u8"}]`,
		}},
		"name": {{
			MovieKey:   "name",
			GroupIndex: 0,
			Content:    `[{"episode":"第1集","link":"http://b/1.m3u8"},{"episode":"第8集","link":"http://b/8.m3u8"},{"episode":"第9集","link":"http://b/9.m3u8"}]`,
		}},
	}
	got := selectBestPlayGroups("subo", "速博", []string{"dbid", "name"}, byKey)
	if len(got) != 1 || len(got[0].LinkList) != 3 {
		t.Fatalf("should prefer the key with more episodes, got %+v", got)
	}

	tied := selectBestPlayGroups("subo", "速博", []string{"dbid", "name"}, map[string][]model.SlaveMoviePlaylist{
		"dbid": {{MovieKey: "dbid", Content: `[{"episode":"第1集","link":"http://a/1.m3u8"}]`}},
		"name": {{MovieKey: "name", Content: `[{"episode":"第1集","link":"http://b/1.m3u8"}]`}},
	})
	if len(tied) != 1 || tied[0].LinkList[0].Link != "http://a/1.m3u8" {
		t.Fatalf("equal counts should keep the first key, got %+v", tied)
	}
}

func TestGetMultiplePlayGroupsBySourcesAndKeysUsesNewestSlaveList(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{20: 0}, map[int64]string{20: model.BigCategoryAnimation})

	primary := BuildMovieMatchKeysWithCategory(0, "测试剧", 20)[0]
	legacy := BuildMovieMatchKeys(0, "测试剧")[0]
	eight, _ := json.Marshal([]model.MovieUrlInfo{
		{Episode: "第1集", Link: "http://old/1.m3u8"},
		{Episode: "第8集", Link: "http://old/8.m3u8"},
	})
	nine, _ := json.Marshal([]model.MovieUrlInfo{
		{Episode: "第1集", Link: "http://new/1.m3u8"},
		{Episode: "第8集", Link: "http://new/8.m3u8"},
		{Episode: "第9集", Link: "http://new/9.m3u8"},
	})
	if err := gdb.Create(&model.SlaveMoviePlaylist{SourceId: "subo", MovieKey: primary, GroupIndex: 0, GroupName: "m3u8", Content: string(eight)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.SlaveMoviePlaylist{SourceId: "subo", MovieKey: legacy, GroupIndex: 0, GroupName: "m3u8", Content: string(nine)}).Error; err != nil {
		t.Fatal(err)
	}

	groups := GetMultiplePlayGroupsBySourcesAndKeys(
		[]model.FilmSource{{Id: "subo", Name: "速博(SUBO)"}},
		[]string{primary, legacy},
	)
	got := groups["subo"]
	if len(got) != 1 {
		t.Fatalf("expected 1 play group, got %d", len(got))
	}
	if len(got[0].LinkList) != 3 {
		t.Fatalf("detail page should show the slave's latest 3 episodes, got %d", len(got[0].LinkList))
	}
}

func TestSaveSitePlayListAffectedEvenWhenNotLeadingEpisodeCount(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	const mid int64 = 10
	key := BuildMovieMatchKeys(0, "追平主站剧")[0]

	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid, ContentKey: "vod_10", SourceId: "master"},
		FilmIndexContent:  model.FilmIndexContent{Name: "追平主站剧", UpdateStamp: 100},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.MovieMatchKey{Mid: mid, MatchKey: key}).Error; err != nil {
		t.Fatal(err)
	}
	masterDetail := model.MovieDetail{
		Name: "追平主站剧",
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "第1集", Link: "http://m/1.m3u8"}, {Episode: "第2集", Link: "http://m/2.m3u8"}},
		},
	}
	raw, _ := json.Marshal(masterDetail)
	if err := gdb.Create(&model.MovieDetailInfo{Mid: mid, Content: string(raw)}).Error; err != nil {
		t.Fatal(err)
	}

	old := model.MovieDetail{
		Name: "追平主站剧",
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "第1集", Link: "http://s/1.m3u8"}},
		},
	}
	if _, err := SaveSitePlayList("subo", []model.MovieDetail{old}); err != nil {
		t.Fatal(err)
	}

	latest := model.MovieDetail{
		Name: "追平主站剧",
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "第1集", Link: "http://s/1.m3u8"}, {Episode: "第2集", Link: "http://s/2.m3u8"}},
		},
	}
	result, err := SaveSitePlayList("subo", []model.MovieDetail{latest})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NotifyMIDs) != 0 {
		t.Fatalf("catching up to master episode count must not enter 最近更新, got %v", result.NotifyMIDs)
	}
	if len(result.AffectedMIDs) != 1 || result.AffectedMIDs[0] != mid {
		t.Fatalf("playlist write must still affect detail cache, got %v", result.AffectedMIDs)
	}
}
