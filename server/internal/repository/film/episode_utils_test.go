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
