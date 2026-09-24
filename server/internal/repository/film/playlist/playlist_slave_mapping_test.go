package playlist

import (
	"testing"

	"server/internal/model"
)

func TestBuildSlaveSourceMappingsSplitsSameTitleByYear(t *testing.T) {
	details := []model.MovieDetail{
		{Id: 46485, Name: "仙逆"},
		{Id: 129138, Name: "仙逆"},
	}
	got := buildSlaveSourceMappings("1016684692", details, []int64{47014, 126574})
	bySid := map[int64]int64{}
	for _, row := range got {
		bySid[row.SourceMid] = row.GlobalMid
	}
	if bySid[46485] != 47014 {
		t.Fatalf("速博动漫 vod 应映射到 47014, got %d", bySid[46485])
	}
	if bySid[129138] != 126574 {
		t.Fatalf("速博短剧 vod 应映射到 126574, got %d", bySid[129138])
	}
}

func TestBuildSlaveSourceMappingsSkipsUnmatchedDetail(t *testing.T) {
	details := []model.MovieDetail{
		{Id: 46485, Name: "仙逆"},
		{Id: 129138, Name: "仙逆"},
	}
	got := buildSlaveSourceMappings("1016684692", details, []int64{0, 126574})
	if len(got) != 1 || got[0].SourceMid != 129138 || got[0].GlobalMid != 126574 {
		t.Fatalf("没挂上线路的详情不得写 mapping, got %+v", got)
	}
}
