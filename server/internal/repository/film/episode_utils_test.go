package film

import (
	"testing"

	"server/internal/model"
	"server/internal/repository/film/shared"
)

func TestEpisodeCount(t *testing.T) {
	links := []model.MovieUrlInfo{
		{Episode: "01"},
		{Episode: "  "},
		{Episode: "02"},
		{Episode: ""},
		{Episode: "03"},
	}
	if got := shared.EpisodeCount(links); got != 3 {
		t.Fatalf("episodeCount = %d, want 3", got)
	}
	if got := shared.EpisodeCount(nil); got != 0 {
		t.Fatalf("shared.EpisodeCount(nil) = %d, want 0", got)
	}
}

func TestIsEpisodeCountHigher(t *testing.T) {
	// 新片：历史为空，新有 1 集
	if !shared.IsEpisodeCountHigher([]int{1}, nil) {
		t.Errorf("expected true for empty existing")
	}
	// 14 -> 15
	if !shared.IsEpisodeCountHigher([]int{15}, []int{14}) {
		t.Errorf("expected true when 15 > 14")
	}
	// 已有 15，后续源也是 15
	if shared.IsEpisodeCountHigher([]int{15}, []int{15}) {
		t.Errorf("expected false when 15 <= 15")
	}
	// 回退 14 < 15
	if shared.IsEpisodeCountHigher([]int{14}, []int{15}) {
		t.Errorf("expected false when 14 < 15")
	}
	// 多线路：取最大；新最大 16 > 旧最大 15
	if !shared.IsEpisodeCountHigher([]int{10, 16}, []int{15, 12}) {
		t.Errorf("expected true when max 16 > max 15")
	}
	// 新无分集
	if shared.IsEpisodeCountHigher(nil, []int{1}) {
		t.Errorf("expected false for empty new")
	}
}

func TestLoadExistingEpisodeCountsByMIDs_Playlist(t *testing.T) {
	gdb := setupFilmZeroTestDB(t)

	// 插入播放列表（存放在 model.FilmSourcePlaylist）
	if err := gdb.Create(&model.FilmSourcePlaylist{
		Mid:          101,
		SourceId:     "src_1",
		LineKind:     "play",
		GroupIndex:   0,
		EpisodeCount: 2,
		Content:      `[{"episode":"01","link":"http://a/1"},{"episode":"02","link":"http://a/2"}]`,
	}).Error; err != nil {
		t.Fatalf("create playlist 1 failed: %v", err)
	}

	if err := gdb.Create(&model.FilmSourcePlaylist{
		Mid:          101,
		SourceId:     "src_2",
		LineKind:     "play",
		GroupIndex:   0,
		EpisodeCount: 3,
		Content:      `[{"episode":"01","link":"http://b/1"},{"episode":"02","link":"http://b/2"},{"episode":"03","link":"http://b/3"}]`,
	}).Error; err != nil {
		t.Fatalf("create playlist 2 failed: %v", err)
	}

	// 1. 无排除源：应统计出 2 集与 3 集
	countsMap, err := shared.LoadExistingEpisodeCountsByMIDs(gdb, []int64{101}, "")
	if err != nil {
		t.Fatalf("loadExistingEpisodeCountsByMIDs failed: %v", err)
	}
	counts := countsMap[101]
	if len(counts) != 2 {
		t.Fatalf("expected 2 counts, got %d (%v)", len(counts), counts)
	}
	if shared.MaxEpisodeCount(counts) != 3 {
		t.Fatalf("expected max episode count 3, got %d", shared.MaxEpisodeCount(counts))
	}

	// 2. 排除 src_2：应仅剩 src_1 的 2 集
	countsMapExcluded, err := shared.LoadExistingEpisodeCountsByMIDs(gdb, []int64{101}, "src_2")
	if err != nil {
		t.Fatalf("loadExistingEpisodeCountsByMIDs with excludeSourceID failed: %v", err)
	}
	countsExcluded := countsMapExcluded[101]
	if len(countsExcluded) != 1 || countsExcluded[0] != 2 {
		t.Fatalf("expected [2] after excluding src_2, got %v", countsExcluded)
	}
}

func TestLoadExistingEpisodeCountsByMIDs_MultiMid(t *testing.T) {
	gdb := setupFilmZeroTestDB(t)

	gdb.Create(&model.FilmSourcePlaylist{
		Mid:          201,
		SourceId:     "src_1",
		LineKind:     "play",
		GroupIndex:   0,
		EpisodeCount: 5,
		Content:      `[{"episode":"01","link":"http://s/1"}]`,
	})
	gdb.Create(&model.FilmSourcePlaylist{
		Mid:          202,
		SourceId:     "src_2",
		LineKind:     "play",
		GroupIndex:   0,
		EpisodeCount: 4,
		Content:      `[{"episode":"01","link":"http://u/1"}]`,
	})

	countsMap, err := shared.LoadExistingEpisodeCountsByMIDs(gdb, []int64{201, 202}, "")
	if err != nil {
		t.Fatalf("loadExistingEpisodeCountsByMIDs failed: %v", err)
	}

	c201 := countsMap[201]
	c202 := countsMap[202]

	if shared.MaxEpisodeCount(c201) != 5 {
		t.Fatalf("expected mid 201 max count 5, got %d (%v)", shared.MaxEpisodeCount(c201), c201)
	}
	if shared.MaxEpisodeCount(c202) != 4 {
		t.Fatalf("expected mid 202 max count 4, got %d (%v)", shared.MaxEpisodeCount(c202), c202)
	}
}
