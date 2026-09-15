package film

import (
	"strings"
	"testing"

	"server/internal/model"
	"server/internal/repository/support"
)

func TestBuildPlaylistMovieKeys_DualKeyFallback(t *testing.T) {
	support.SetCategoryTreeForTest(map[int64]int64{
		20: 0, // 动漫
		34: 0, // 短剧
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

	keys := BuildPlaylistMovieKeys(detail)
	if len(keys) < 2 {
		t.Fatalf("Expected dual keys (category key + legacy key), got: %v", keys)
	}

	catKey := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	legacyKey := BuildMovieMatchKeys(0, "仙逆")[0]

	if keys[0] != catKey {
		t.Errorf("First key should be category-specific key: expected=%s, got=%s", catKey, keys[0])
	}
	if keys[1] != legacyKey {
		t.Errorf("Second key should be legacy fallback key: expected=%s, got=%s", legacyKey, keys[1])
	}
}

func TestCategoryAwareMatchKeys_PrimaryKeysDoNotCollide(t *testing.T) {
	animeKeys := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)
	shortDramaKeys := BuildMovieMatchKeysWithCategory(0, "仙逆", 34)

	if len(animeKeys) == 0 || len(shortDramaKeys) == 0 {
		t.Fatalf("Keys should not be empty")
	}
	// 首选大类精准隔离键不可碰撞
	if animeKeys[0] == shortDramaKeys[0] {
		t.Fatalf("Category-aware primary keys collided: %s", animeKeys[0])
	}
}

func TestResolveMovieDetailRootPid(t *testing.T) {
	// 动态注入模拟大类 ID（例如在某个生产环境中电视剧=1, 电影=9, 动漫=20, 短剧=34, 国产剧子类 101->1）
	support.SetCategoryTreeForTest(map[int64]int64{
		1:   0,
		9:   0,
		20:  0,
		34:  0,
		101: 1, // 子分类国产剧，父类为 1 (电视剧)
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
			got := ResolveMovieDetailRootPid(tt.detail)
			if got != tt.expected {
				t.Errorf("ResolveMovieDetailRootPid() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestPickBestMidForMatchKey_Deterministic(t *testing.T) {
	// 测试无论输入顺序如何，返回的 mid 均保持一致
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
		20: 0, // 动漫
		34: 0, // 短剧
	}, map[int64]string{
		20: model.BigCategoryAnimation,
		34: model.BigCategoryShortFilm,
	})

	// 1. 明确带大类时，主键必须为 hash("片名#cat_pid")，不可为裸片名
	detailWithCat := model.MovieDetail{
		Pid:  20,
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			CName: "动漫",
		},
	}
	keyWithCat := BuildPlaylistPrimaryMovieKey(detailWithCat)
	expectedCatKey := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	if keyWithCat != expectedCatKey {
		t.Errorf("BuildPlaylistPrimaryMovieKey() = %s, expected %s", keyWithCat, expectedCatKey)
	}

	// 2. 带豆瓣 ID 时，优先使用豆瓣 ID 主键
	detailWithDb := model.MovieDetail{
		Pid:  20,
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			DbId: 9999,
		},
	}
	keyWithDb := BuildPlaylistPrimaryMovieKey(detailWithDb)
	expectedDbKey := BuildMovieMatchKeysWithCategory(9999, "仙逆", 20)[0]
	if keyWithDb != expectedDbKey {
		t.Errorf("BuildPlaylistPrimaryMovieKey() with DbId = %s, expected %s", keyWithDb, expectedDbKey)
	}

	// 3. 未知大类时，降级使用裸片名主键
	detailUnmapped := model.MovieDetail{
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			CName: "未映射标签",
		},
	}
	keyUnmapped := BuildPlaylistPrimaryMovieKey(detailUnmapped)
	expectedUnmappedKey := BuildMovieMatchKeys(0, "仙逆")[0]
	if keyUnmapped != expectedUnmappedKey {
		t.Errorf("BuildPlaylistPrimaryMovieKey() unmapped = %s, expected %s", keyUnmapped, expectedUnmappedKey)
	}
}

func TestCrossCategoryPlaylist_NoLeak(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{
		20: 0, // 动漫
		34: 0, // 短剧
	}, map[int64]string{
		20: model.BigCategoryAnimation,
		34: model.BigCategoryShortFilm,
	})

	// 1. 采集写入副站短剧《仙逆》 (pid=34)
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

	// 2. 采集写入副站动漫《仙逆》 (pid=20)
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

	// 3. 采集写入未映射副站《仙逆》 (pid=0)
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

	// 4. 验证数据库中 slave_movie_playlist 的实际记录数：
	// slave_short 只能存 1 条 (pid=34 key)，绝不可存裸片名
	var shortRows []model.SlaveMoviePlaylist
	gdb.Where("source_id = ?", "slave_short").Find(&shortRows)
	if len(shortRows) != 1 {
		t.Fatalf("Expected exactly 1 row for slave_short, got %d", len(shortRows))
	}
	cat34Key := BuildMovieMatchKeysWithCategory(0, "仙逆", 34)[0]
	if shortRows[0].MovieKey != cat34Key {
		t.Fatalf("slave_short movie_key expected %s, got %s", cat34Key, shortRows[0].MovieKey)
	}

	// 5. 模拟主站访问动漫《仙逆》 (pid=20)
	animeMasterKeys := BuildMovieMatchKeysWithCategory(0, "仙逆", 20) // [cat_20, legacy_title]
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

	// 6. 模拟主站访问短剧《仙逆》 (pid=34)
	shortMasterKeys := BuildMovieMatchKeysWithCategory(0, "仙逆", 34) // [cat_34, legacy_title]
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

	primary := BuildMovieMatchKeysWithCategory(0, "独行月球", 20)[0]
	legacy := BuildMovieMatchKeys(0, "独行月球")[0]
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

	// 二次采集新一集必须打在同一主键上，播放时才能读到最新集。
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

func TestUnmappedSlavePlaylist_DoesNotInheritWhenTitleCollides(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{
		20: 0,
		34: 0,
	}, map[int64]string{
		20: model.BigCategoryAnimation,
		34: model.BigCategoryShortFilm,
	})

	animePrimary := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	shortPrimary := BuildMovieMatchKeysWithCategory(0, "仙逆", 34)[0]
	legacy := BuildMovieMatchKeys(0, "仙逆")[0]
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
