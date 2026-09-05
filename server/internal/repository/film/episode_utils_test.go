package film

import (
	"testing"

	"server/internal/model"
)

func TestEpisodeCount(t *testing.T) {
	links := []model.MovieUrlInfo{
		{Episode: "01"},
		{Episode: "  "},
		{Episode: "02"},
		{Episode: ""},
		{Episode: "03"},
	}
	if got := episodeCount(links); got != 3 {
		t.Fatalf("episodeCount = %d, want 3", got)
	}
	if got := episodeCount(nil); got != 0 {
		t.Fatalf("episodeCount(nil) = %d, want 0", got)
	}
}

func TestIsEpisodeCountHigher(t *testing.T) {
	// 新片：历史为空，新有 1 集
	if !isEpisodeCountHigher([]int{1}, nil) {
		t.Errorf("expected true for empty existing")
	}
	// 14 -> 15
	if !isEpisodeCountHigher([]int{15}, []int{14}) {
		t.Errorf("expected true when 15 > 14")
	}
	// 已有 15，后续源也是 15
	if isEpisodeCountHigher([]int{15}, []int{15}) {
		t.Errorf("expected false when 15 <= 15")
	}
	// 回退 14 < 15
	if isEpisodeCountHigher([]int{14}, []int{15}) {
		t.Errorf("expected false when 14 < 15")
	}
	// 多线路：取最大；新最大 16 > 旧最大 15
	if !isEpisodeCountHigher([]int{10, 16}, []int{15, 12}) {
		t.Errorf("expected true when max 16 > max 15")
	}
	// 新无分集
	if isEpisodeCountHigher(nil, []int{1}) {
		t.Errorf("expected false for empty new")
	}
}

func TestLoadExistingEpisodeCountsByMIDs_SlaveMoviePlaylist_Chunking(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	// 为 mid 101 插入 600 个 match keys（超过 500 分块大小）
	const keyCount = 600
	matchKeys := make([]model.MovieMatchKey, 0, keyCount)
	for i := 1; i <= keyCount; i++ {
		matchKeys = append(matchKeys, model.MovieMatchKey{
			Mid:      101,
			MatchKey: "key_" + string(rune('a'+(i%26))) + "_" + string(rune('0'+(i%10))) + "_" + string(rune(i)),
		})
	}
	if err := gdb.CreateInBatches(matchKeys, 300).Error; err != nil {
		t.Fatalf("create match keys failed: %v", err)
	}

	// 插入附属站播放列表（存放在 model.SlaveMoviePlaylist）
	// 第 1 条属于 slave_1（2 集）
	if err := gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "slave_1",
		MovieKey: matchKeys[0].MatchKey,
		Content:  `[{"episode":"01","link":"http://a/1"},{"episode":"02","link":"http://a/2"}]`,
	}).Error; err != nil {
		t.Fatalf("create slave playlist 1 failed: %v", err)
	}
	// 第 550 条属于 slave_2（3 集，验证分块第二批 500~600）
	if err := gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "slave_2",
		MovieKey: matchKeys[550].MatchKey,
		Content:  `[{"episode":"01","link":"http://b/1"},{"episode":"02","link":"http://b/2"},{"episode":"03","link":"http://b/3"}]`,
	}).Error; err != nil {
		t.Fatalf("create slave playlist 2 failed: %v", err)
	}

	// 1. 无排除源：应统计出 2 集与 3 集
	countsMap, err := loadExistingEpisodeCountsByMIDs(gdb, []int64{101}, "")
	if err != nil {
		t.Fatalf("loadExistingEpisodeCountsByMIDs failed: %v", err)
	}
	counts := countsMap[101]
	if len(counts) != 2 {
		t.Fatalf("expected 2 counts returned across chunks, got %d (%v)", len(counts), counts)
	}
	if maxEpisodeCount(counts) != 3 {
		t.Fatalf("expected max episode count 3, got %d", maxEpisodeCount(counts))
	}

	// 2. 排除 slave_2：应仅剩 slave_1 的 2 集
	countsMapExcluded, err := loadExistingEpisodeCountsByMIDs(gdb, []int64{101}, "slave_2")
	if err != nil {
		t.Fatalf("loadExistingEpisodeCountsByMIDs with excludeSourceID failed: %v", err)
	}
	countsExcluded := countsMapExcluded[101]
	if len(countsExcluded) != 1 || countsExcluded[0] != 2 {
		t.Fatalf("expected [2] after excluding slave_2, got %v", countsExcluded)
	}
}

func TestLoadExistingEpisodeCountsByMIDs_MultiMidSharedKey(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	// mid 201 与 mid 202 共享同一个 matchKey "shared_key"
	gdb.Create(&model.MovieMatchKey{Mid: 201, MatchKey: "shared_key"})
	gdb.Create(&model.MovieMatchKey{Mid: 202, MatchKey: "shared_key"})
	// 且 mid 201 自身有重复 key 记录
	gdb.Create(&model.MovieMatchKey{Mid: 201, MatchKey: "unique_201"})

	gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "slave_shared",
		MovieKey: "shared_key",
		Content:  `[{"episode":"01","link":"http://s/1"},{"episode":"02","link":"http://s/2"},{"episode":"03","link":"http://s/3"},{"episode":"04","link":"http://s/4"},{"episode":"05","link":"http://s/5"}]`,
	})
	gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "slave_shared",
		MovieKey: "unique_201",
		Content:  `[{"episode":"01","link":"http://u/1"}]`,
	})

	countsMap, err := loadExistingEpisodeCountsByMIDs(gdb, []int64{201, 202}, "")
	if err != nil {
		t.Fatalf("loadExistingEpisodeCountsByMIDs failed: %v", err)
	}

	// 验证 201 和 202 两个 mid 都正确获得了 shared_key 的 5 集数据
	c201 := countsMap[201]
	c202 := countsMap[202]

	if maxEpisodeCount(c201) != 5 {
		t.Fatalf("expected mid 201 max count 5, got %d (%v)", maxEpisodeCount(c201), c201)
	}
	if maxEpisodeCount(c202) != 5 {
		t.Fatalf("expected mid 202 max count 5, got %d (%v)", maxEpisodeCount(c202), c202)
	}
}
