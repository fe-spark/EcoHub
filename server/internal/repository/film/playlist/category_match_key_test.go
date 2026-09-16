package playlist

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"
)

func TestBuildPlaylistMovieKeys_DualKeyFallback(t *testing.T) {
	support.SetCategoryTreeForTest(map[int64]int64{
		20: 0,
		34: 0,
	}, map[int64]string{
		20: model.BigCategoryAnimation,
		34: model.BigCategoryShortFilm,
	})

	detail := model.MovieDetail{
		Pid:  20,
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			CName: "动漫",
		},
	}

	keys := shared.BuildPlaylistMovieKeys(detail)
	if len(keys) < 2 {
		t.Fatalf("Expected dual keys (category key + legacy key), got: %v", keys)
	}

	catKey := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	legacyKey := shared.BuildMovieMatchKeys(0, "仙逆")[0]

	if keys[0] != catKey {
		t.Errorf("First key should be category-specific key: expected=%s, got=%s", catKey, keys[0])
	}
	if keys[1] != legacyKey {
		t.Errorf("Second key should be legacy fallback key: expected=%s, got=%s", legacyKey, keys[1])
	}
}

func TestCategoryAwareMatchKeys_PrimaryKeysDoNotCollide(t *testing.T) {
	animeKeys := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 20)
	shortDramaKeys := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 34)

	if len(animeKeys) == 0 || len(shortDramaKeys) == 0 {
		t.Fatalf("Keys should not be empty")
	}
	if animeKeys[0] == shortDramaKeys[0] {
		t.Fatalf("Category-aware primary keys collided: %s", animeKeys[0])
	}
}

func TestResolveMovieDetailRootPid(t *testing.T) {
	support.SetCategoryTreeForTest(map[int64]int64{
		1:   0,
		9:   0,
		20:  0,
		34:  0,
		101: 1,
	}, map[int64]string{
		1:   model.BigCategoryTV,
		9:   model.BigCategoryMovie,
		20:  model.BigCategoryAnimation,
		34:  model.BigCategoryShortFilm,
		101: "国产剧",
	})

	tests := []struct {
		name     string
		detail   model.MovieDetail
		expected int64
	}{
		{
			name:     "Master Detail with Pid=20 (主站权威)",
			detail:   model.MovieDetail{Pid: 20, MovieDescriptor: model.MovieDescriptor{CName: "任意副站乱标名称"}},
			expected: 20,
		},
		{
			name:     "Master Detail with Cid=101 (主站权威)",
			detail:   model.MovieDetail{Cid: 101, MovieDescriptor: model.MovieDescriptor{CName: "国产剧"}},
			expected: 1,
		},
		{
			name:     "Exact Category: 短剧 (精确匹配系统分类)",
			detail:   model.MovieDetail{MovieDescriptor: model.MovieDescriptor{CName: "短剧"}},
			expected: 34,
		},
		{
			name:     "Exact Category: 电影 (精确匹配系统分类)",
			detail:   model.MovieDetail{MovieDescriptor: model.MovieDescriptor{CName: "电影"}},
			expected: 9,
		},
		{
			name:     "Exact Category: 动漫 (精确匹配系统分类)",
			detail:   model.MovieDetail{MovieDescriptor: model.MovieDescriptor{CName: "动漫"}},
			expected: 20,
		},
		{
			name:     "Exact Category: 电视剧 (精确匹配系统分类)",
			detail:   model.MovieDetail{MovieDescriptor: model.MovieDescriptor{CName: "电视剧"}},
			expected: 1,
		},
		{
			name:     "External unmapped: 反转爽剧 (副站乱标不盲猜，按 0 处理，后续以主站为准)",
			detail:   model.MovieDetail{MovieDescriptor: model.MovieDescriptor{CName: "反转爽剧"}},
			expected: 0,
		},
		{
			name:     "External unmapped: 国产动漫 (副站乱标不盲猜，按 0 处理)",
			detail:   model.MovieDetail{MovieDescriptor: model.MovieDescriptor{CName: "国产动漫"}},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shared.ResolveMovieDetailRootPid(tt.detail)
			if got != tt.expected {
				t.Errorf("shared.ResolveMovieDetailRootPid() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestPickBestMidForMatchKey_Deterministic(t *testing.T) {
	midsA := []int64{47014, 126574}
	midsB := []int64{126574, 47014}

	gotA := pickBestMidForMatchKey(midsA)
	gotB := pickBestMidForMatchKey(midsB)

	if gotA != gotB {
		t.Fatalf("pickBestMidForMatchKey is not deterministic: gotA=%d, gotB=%d", gotA, gotB)
	}
}

func TestBuildPlaylistPrimaryMovieKey(t *testing.T) {
	support.SetCategoryTreeForTest(map[int64]int64{
		20: 0,
		34: 0,
	}, map[int64]string{
		20: model.BigCategoryAnimation,
		34: model.BigCategoryShortFilm,
	})

	detailWithCat := model.MovieDetail{
		Pid:  20,
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			CName: "动漫",
		},
	}
	keyWithCat := shared.BuildPlaylistPrimaryMovieKey(detailWithCat)
	expectedCatKey := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	if keyWithCat != expectedCatKey {
		t.Errorf("shared.BuildPlaylistPrimaryMovieKey() = %s, expected %s", keyWithCat, expectedCatKey)
	}

	detailWithDb := model.MovieDetail{
		Pid:  20,
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			DbId: 9999,
		},
	}
	keyWithDb := shared.BuildPlaylistPrimaryMovieKey(detailWithDb)
	expectedDbKey := shared.BuildMovieMatchKeysWithCategory(9999, "仙逆", 20)[0]
	if keyWithDb != expectedDbKey {
		t.Errorf("shared.BuildPlaylistPrimaryMovieKey() with DbId = %s, expected %s", keyWithDb, expectedDbKey)
	}

	detailUnmapped := model.MovieDetail{
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			CName: "未映射标签",
		},
	}
	keyUnmapped := shared.BuildPlaylistPrimaryMovieKey(detailUnmapped)
	expectedUnmappedKey := shared.BuildMovieMatchKeys(0, "仙逆")[0]
	if keyUnmapped != expectedUnmappedKey {
		t.Errorf("shared.BuildPlaylistPrimaryMovieKey() unmapped = %s, expected %s", keyUnmapped, expectedUnmappedKey)
	}
}

func TestCrossCategoryPlaylist_NoLeak(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{
		20: 0,
		34: 0,
	}, map[int64]string{
		20: model.BigCategoryAnimation,
		34: model.BigCategoryShortFilm,
	})

	shortDramaDetail := model.MovieDetail{
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			CName: "短剧",
		},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "第1集", Link: "https://short.com/1.m3u8"}},
		},
	}
	if _, err := SaveSitePlayList("slave_short", []model.MovieDetail{shortDramaDetail}); err != nil {
		t.Fatalf("SaveSitePlayList slave_short failed: %v", err)
	}

	animeDetail := model.MovieDetail{
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			CName: "动漫",
		},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "第1集", Link: "https://anime.com/1.m3u8"}},
		},
	}
	if _, err := SaveSitePlayList("slave_anime", []model.MovieDetail{animeDetail}); err != nil {
		t.Fatalf("SaveSitePlayList slave_anime failed: %v", err)
	}

	unmappedDetail := model.MovieDetail{
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			CName: "通用线路",
		},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "第1集", Link: "https://unmapped.com/1.m3u8"}},
		},
	}
	if _, err := SaveSitePlayList("slave_unmapped", []model.MovieDetail{unmappedDetail}); err != nil {
		t.Fatalf("SaveSitePlayList slave_unmapped failed: %v", err)
	}

	var shortRows []model.SlaveMoviePlaylist
	gdb.Where("source_id = ?", "slave_short").Find(&shortRows)
	if len(shortRows) != 1 {
		t.Fatalf("Expected exactly 1 row for slave_short, got %d", len(shortRows))
	}
	cat34Key := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 34)[0]
	if shortRows[0].MovieKey != cat34Key {
		t.Fatalf("slave_short movie_key expected %s, got %s", cat34Key, shortRows[0].MovieKey)
	}

	animeMasterKeys := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 20)
	sources := []model.FilmSource{
		{Id: "slave_short", Name: "短剧专线"},
		{Id: "slave_anime", Name: "动漫专线"},
		{Id: "slave_unmapped", Name: "通用专线"},
	}

	animePlayGroups := GetMultiplePlayGroupsBySourcesAndKeys(sources, animeMasterKeys)
	if _, hasShort := animePlayGroups["slave_short"]; hasShort {
		t.Fatalf("CRITICAL BUG: anime requested play groups but got short drama playlist from slave_short!")
	}
	if _, hasAnime := animePlayGroups["slave_anime"]; !hasAnime {
		t.Fatalf("Expected anime playlist from slave_anime")
	}
	if _, hasUnmapped := animePlayGroups["slave_unmapped"]; !hasUnmapped {
		t.Fatalf("Expected unmapped playlist fallback for anime from slave_unmapped")
	}

	shortMasterKeys := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 34)
	shortPlayGroups := GetMultiplePlayGroupsBySourcesAndKeys(sources, shortMasterKeys)
	if _, hasAnime := shortPlayGroups["slave_anime"]; hasAnime {
		t.Fatalf("CRITICAL BUG: short drama requested play groups but got anime playlist from slave_anime!")
	}
	if _, hasShort := shortPlayGroups["slave_short"]; !hasShort {
		t.Fatalf("Expected short drama playlist from slave_short")
	}
	if _, hasUnmapped := shortPlayGroups["slave_unmapped"]; !hasUnmapped {
		t.Fatalf("Expected unmapped playlist fallback for short drama from slave_unmapped")
	}
}

func TestInheritPrimaryMovieKeyIfUnique(t *testing.T) {
	primary := "cat_key_20"
	legacy := "title_key"
	keysByMid := map[int64][]string{
		101: {primary, legacy},
		202: {"cat_key_34", legacy},
	}

	if got := inheritPrimaryMovieKeyIfUnique([]int64{101}, keysByMid); got != primary {
		t.Fatalf("unique mid should inherit primary, got %q", got)
	}
	if got := inheritPrimaryMovieKeyIfUnique([]int64{101, 101}, keysByMid); got != primary {
		t.Fatalf("duplicate mid ids should still count as unique, got %q", got)
	}
	if got := inheritPrimaryMovieKeyIfUnique([]int64{101, 202}, keysByMid); got != "" {
		t.Fatalf("cross-category same title must not inherit, got %q", got)
	}
	if got := inheritPrimaryMovieKeyIfUnique(nil, keysByMid); got != "" {
		t.Fatalf("no candidates should not inherit, got %q", got)
	}
	if got := inheritPrimaryMovieKeyIfUnique([]int64{0, -1}, keysByMid); got != "" {
		t.Fatalf("invalid mids should not inherit, got %q", got)
	}
	if got := inheritPrimaryMovieKeyIfUnique([]int64{303}, keysByMid); got != "" {
		t.Fatalf("unique mid without stored keys should not inherit, got %q", got)
	}
}

func TestUnmappedSlavePlaylist_InheritsUniqueMainPrimaryKey(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{
		20: 0,
	}, map[int64]string{
		20: model.BigCategoryAnimation,
	})

	primary := shared.BuildMovieMatchKeysWithCategory(0, "独行月球", 20)[0]
	legacy := shared.BuildMovieMatchKeys(0, "独行月球")[0]
	if err := gdb.Create(&model.MovieMatchKey{Mid: 501, MatchKey: primary}).Error; err != nil {
		t.Fatalf("create primary match key: %v", err)
	}
	if err := gdb.Create(&model.MovieMatchKey{Mid: 501, MatchKey: legacy}).Error; err != nil {
		t.Fatalf("create legacy match key: %v", err)
	}

	unmapped := model.MovieDetail{
		Name: "独行月球",
		MovieDescriptor: model.MovieDescriptor{
			CName: "通用线路",
		},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "第1集", Link: "https://slave.com/1.m3u8"}},
		},
	}
	if _, err := SaveSitePlayList("slave_generic", []model.MovieDetail{unmapped}); err != nil {
		t.Fatalf("SaveSitePlayList: %v", err)
	}

	var rows []model.SlaveMoviePlaylist
	gdb.Where("source_id = ?", "slave_generic").Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("expected 1 slave row, got %d", len(rows))
	}
	if rows[0].MovieKey != primary {
		t.Fatalf("unmapped unique title should inherit main primary %s, got %s", primary, rows[0].MovieKey)
	}

	unmapped.PlayList = [][]model.MovieUrlInfo{
		{
			{Episode: "第1集", Link: "https://slave.com/1.m3u8"},
			{Episode: "第2集", Link: "https://slave.com/2.m3u8"},
		},
	}
	if _, err := SaveSitePlayList("slave_generic", []model.MovieDetail{unmapped}); err != nil {
		t.Fatalf("SaveSitePlayList update: %v", err)
	}
	gdb.Where("source_id = ?", "slave_generic").Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("update should keep a single row, got %d", len(rows))
	}
	if rows[0].MovieKey != primary {
		t.Fatalf("updated episodes should stay on inherited primary, got %s", rows[0].MovieKey)
	}
	if !strings.Contains(rows[0].Content, "第2集") {
		t.Fatalf("expected episode 2 in playlist content, got %s", rows[0].Content)
	}

	sources := []model.FilmSource{{Id: "slave_generic", Name: "通用专线"}}
	groups := GetMultiplePlayGroupsBySourcesAndKeys(sources, []string{primary, legacy})
	if _, ok := groups["slave_generic"]; !ok {
		t.Fatalf("playback lookup by main dual keys should hit inherited primary")
	}
}

func TestSaveSitePlayList_WritesOntoMasterPrimaryWhenUniqueMatch(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{20: 0}, map[int64]string{20: model.BigCategoryAnimation})

	dbidKey := shared.BuildMovieMatchKeysWithCategory(12345, "仙逆", 20)[0]
	catKey := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	legacy := shared.BuildMovieMatchKeys(0, "仙逆")[0]
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 801, ContentKey: "vod_801", SourceId: "master"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 20, CName: "动漫"},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙逆"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{dbidKey, catKey, legacy} {
		if err := gdb.Create(&model.MovieMatchKey{Mid: 801, MatchKey: key}).Error; err != nil {
			t.Fatal(err)
		}
	}

	slave := model.MovieDetail{
		Name:            "仙逆",
		MovieDescriptor: model.MovieDescriptor{CName: "动漫"},
		PlayList: [][]model.MovieUrlInfo{
			{
				{Episode: "第1集", Link: "https://subo/1.m3u8"},
				{Episode: "第9集", Link: "https://subo/9.m3u8"},
			},
		},
	}
	if _, err := SaveSitePlayList("subo", []model.MovieDetail{slave}); err != nil {
		t.Fatal(err)
	}

	var rows []model.SlaveMoviePlaylist
	gdb.Where("source_id = ?", "subo").Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].MovieKey != dbidKey {
		t.Fatalf("unique same-category match must write master primary %s, got %s", dbidKey, rows[0].MovieKey)
	}

	groups := GetMultiplePlayGroupsBySourcesAndKeys(
		[]model.FilmSource{{Id: "subo", Name: "速博"}},
		[]string{dbidKey, catKey, legacy},
	)
	got := groups["subo"]
	if len(got) != 1 || len(got[0].LinkList) != 2 {
		t.Fatalf("detail lookup must see latest slave episodes, got %+v", got)
	}
}

func TestSaveSitePlayList_MismatchedCategoryUniqueTitleWritesMasterPrimary(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{
		1:  0,
		20: 0,
	}, map[int64]string{
		1:  model.BigCategoryTV,
		20: model.BigCategoryAnimation,
	})

	cat20 := shared.BuildMovieMatchKeysWithCategory(0, "一斩苍穹", 20)[0]
	legacy := shared.BuildMovieMatchKeys(0, "一斩苍穹")[0]
	cat1 := shared.BuildMovieMatchKeysWithCategory(0, "一斩苍穹", 1)[0]
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 116429, ContentKey: "vod_116429", SourceId: "master"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 20, CName: "中国动漫"},
		FilmIndexContent:  model.FilmIndexContent{Name: "一斩苍穹"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{cat20, legacy} {
		if err := gdb.Create(&model.MovieMatchKey{Mid: 116429, MatchKey: key}).Error; err != nil {
			t.Fatal(err)
		}
	}

	slave := model.MovieDetail{
		Name:            "一斩苍穹",
		MovieDescriptor: model.MovieDescriptor{CName: "电视剧"},
		PlayList: [][]model.MovieUrlInfo{
			{
				{Episode: "第1集", Link: "https://subo/1.m3u8"},
				{Episode: "第9集", Link: "https://subo/9.m3u8"},
			},
		},
	}
	if _, err := SaveSitePlayList("subo", []model.MovieDetail{slave}); err != nil {
		t.Fatal(err)
	}

	var rows []model.SlaveMoviePlaylist
	gdb.Where("source_id = ?", "subo").Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].MovieKey != cat20 {
		t.Fatalf("unique title must write master primary %s even when slave category differs, got %s", cat20, rows[0].MovieKey)
	}
	if rows[0].MovieKey == cat1 {
		t.Fatalf("must not write the slave's own 电视剧 key %s", cat1)
	}

	groups := GetMultiplePlayGroupsBySourcesAndKeys(
		[]model.FilmSource{{Id: "subo", Name: "速博"}},
		[]string{cat20, legacy},
	)
	got := groups["subo"]
	if len(got) != 1 || len(got[0].LinkList) != 2 {
		t.Fatalf("detail lookup must see 9th episode on master keys, got %+v", got)
	}
	if last := got[0].LinkList[1].Episode; last != "第9集" {
		t.Fatalf("expected 第9集, got %s", last)
	}
}

func TestSaveSitePlayList_MismatchedCategoryDoesNotMergeCollidingTitles(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{
		1:  0,
		20: 0,
		34: 0,
	}, map[int64]string{
		1:  model.BigCategoryTV,
		20: model.BigCategoryAnimation,
		34: model.BigCategoryShortFilm,
	})

	animePrimary := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	shortPrimary := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 34)[0]
	legacy := shared.BuildMovieMatchKeys(0, "仙逆")[0]
	cat1 := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 1)[0]
	for _, row := range []model.FilmIndex{
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 701, ContentKey: "vod_701", SourceId: "master"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 20, CName: "动漫"},
			FilmIndexContent:  model.FilmIndexContent{Name: "仙逆"},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 702, ContentKey: "vod_702", SourceId: "master"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 34, CName: "短剧"},
			FilmIndexContent:  model.FilmIndexContent{Name: "仙逆"},
		},
	} {
		if err := gdb.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, rec := range []model.MovieMatchKey{
		{Mid: 701, MatchKey: animePrimary},
		{Mid: 701, MatchKey: legacy},
		{Mid: 702, MatchKey: shortPrimary},
		{Mid: 702, MatchKey: legacy},
	} {
		if err := gdb.Create(&rec).Error; err != nil {
			t.Fatal(err)
		}
	}

	slave := model.MovieDetail{
		Name:            "仙逆",
		MovieDescriptor: model.MovieDescriptor{CName: "电视剧"},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "第1集", Link: "https://subo/1.m3u8"}},
		},
	}
	if _, err := SaveSitePlayList("subo", []model.MovieDetail{slave}); err != nil {
		t.Fatal(err)
	}

	var rows []model.SlaveMoviePlaylist
	gdb.Where("source_id = ?", "subo").Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].MovieKey != cat1 {
		t.Fatalf("colliding titles with wrong slave category must stay on %s, got %s", cat1, rows[0].MovieKey)
	}

	groups := GetMultiplePlayGroupsBySourcesAndKeys(
		[]model.FilmSource{{Id: "subo", Name: "速博"}},
		[]string{animePrimary, legacy},
	)
	if _, ok := groups["subo"]; ok {
		t.Fatalf("anime 仙逆 must not display the 电视剧-tagged colliding title")
	}
}

func TestUnmappedSlavePlaylist_DoesNotInheritWhenTitleCollides(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{
		20: 0,
		34: 0,
	}, map[int64]string{
		20: model.BigCategoryAnimation,
		34: model.BigCategoryShortFilm,
	})

	animePrimary := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	shortPrimary := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 34)[0]
	legacy := shared.BuildMovieMatchKeys(0, "仙逆")[0]
	gdb.Create(&model.MovieMatchKey{Mid: 601, MatchKey: animePrimary})
	gdb.Create(&model.MovieMatchKey{Mid: 601, MatchKey: legacy})
	gdb.Create(&model.MovieMatchKey{Mid: 602, MatchKey: shortPrimary})
	gdb.Create(&model.MovieMatchKey{Mid: 602, MatchKey: legacy})

	unmapped := model.MovieDetail{
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			CName: "通用线路",
		},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "第1集", Link: "https://unmapped.com/1.m3u8"}},
		},
	}
	if _, err := SaveSitePlayList("slave_unmapped", []model.MovieDetail{unmapped}); err != nil {
		t.Fatalf("SaveSitePlayList: %v", err)
	}

	var rows []model.SlaveMoviePlaylist
	gdb.Where("source_id = ?", "slave_unmapped").Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("expected 1 slave row, got %d", len(rows))
	}
	if rows[0].MovieKey != legacy {
		t.Fatalf("colliding titles must keep plain-title key, expected %s got %s", legacy, rows[0].MovieKey)
	}
}

func TestSameTitleCrossCategory_AnimeUpdateNotMaskedByFinishedShort(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{
		20: 0,
		34: 0,
	}, map[int64]string{
		20: model.BigCategoryAnimation,
		34: model.BigCategoryShortFilm,
	})

	animeKeys := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 20)
	shortKeys := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 34)
	oldStamp := time.Now().Add(-48 * time.Hour).Unix()

	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 701, ContentKey: "vod_701", SourceId: "master"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 20, CName: "动漫"},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙逆", UpdateStamp: oldStamp},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 702, ContentKey: "vod_702", SourceId: "master"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 34, CName: "短剧"},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙逆", UpdateStamp: oldStamp},
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, key := range animeKeys {
		if err := gdb.Create(&model.MovieMatchKey{Mid: 701, MatchKey: key}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range shortKeys {
		if err := gdb.Create(&model.MovieMatchKey{Mid: 702, MatchKey: key}).Error; err != nil {
			t.Fatal(err)
		}
	}

	shortLinks := make([]model.MovieUrlInfo, 0, 80)
	for i := 1; i <= 80; i++ {
		shortLinks = append(shortLinks, model.MovieUrlInfo{Episode: "第" + strconv.Itoa(i) + "集", Link: "https://short/" + strconv.Itoa(i) + ".m3u8"})
	}
	if _, err := SaveSitePlayList("slave_short", []model.MovieDetail{{
		Name:            "仙逆",
		MovieDescriptor: model.MovieDescriptor{CName: "短剧"},
		PlayList:        [][]model.MovieUrlInfo{shortLinks},
	}}); err != nil {
		t.Fatal(err)
	}

	anime21 := make([]model.MovieUrlInfo, 0, 21)
	for i := 1; i <= 21; i++ {
		anime21 = append(anime21, model.MovieUrlInfo{Episode: "第" + strconv.Itoa(i) + "集", Link: "https://anime/" + strconv.Itoa(i) + ".m3u8"})
	}
	if _, err := SaveSitePlayList("slave_anime", []model.MovieDetail{{
		Name:            "仙逆",
		MovieDescriptor: model.MovieDescriptor{CName: "动漫"},
		PlayList:        [][]model.MovieUrlInfo{anime21},
	}}); err != nil {
		t.Fatal(err)
	}

	anime22 := append(append([]model.MovieUrlInfo{}, anime21...), model.MovieUrlInfo{Episode: "第22集", Link: "https://anime/22.m3u8"})
	result, err := SaveSitePlayList("slave_anime", []model.MovieDetail{{
		Name:            "仙逆",
		MovieDescriptor: model.MovieDescriptor{CName: "动漫"},
		PlayList:        [][]model.MovieUrlInfo{anime22},
	}})
	if err != nil {
		t.Fatal(err)
	}

	foundAnime := false
	for _, mid := range result.NotifyMIDs {
		if mid == 701 {
			foundAnime = true
		}
		if mid == 702 {
			t.Fatalf("finished short drama must not enter daily updates when the cartoon adds an episode")
		}
	}
	if !foundAnime {
		t.Fatalf("cartoon 仙逆 must enter daily updates on episode 22, got NotifyMIDs=%v", result.NotifyMIDs)
	}
}
