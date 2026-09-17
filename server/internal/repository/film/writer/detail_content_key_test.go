package writer

import (
	"fmt"
	"testing"

	"server/internal/model"
)

func TestDetailMapByContentKeyKeepsDistinctVods(t *testing.T) {
	list := []model.MovieDetail{
		{Id: 87682, Name: "烬九州第四季", PlayList: [][]model.MovieUrlInfo{makeEpisodes(145)}},
		{Id: 87676, Name: "烬九州：第四季", PlayList: [][]model.MovieUrlInfo{makeEpisodes(91)}},
		{Id: 87677, Name: "烬九州：第五季", PlayList: [][]model.MovieUrlInfo{makeEpisodes(91)}},
		{Id: 87683, Name: "烬九州第五季", PlayList: [][]model.MovieUrlInfo{makeEpisodes(110)}},
	}
	m := detailMapByContentKey(list)
	if len(m) != 4 {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		t.Fatalf("4 个不同 vod 应保留 4 条, got %d keys=%v", len(m), keys)
	}
}

func TestDetailMapByContentKeySkipsEmptyKey(t *testing.T) {
	list := []model.MovieDetail{
		{}, // 无 id 无片名 → 空 ContentKey，应丢弃
		{Id: 1, Name: "有片"},
	}
	m := detailMapByContentKey(list)
	if len(m) != 1 {
		t.Fatalf("空 key 应跳过, want 1 got %d", len(m))
	}
	if _, ok := m["vod_1"]; !ok {
		t.Fatalf("want vod_1, got %#v", m)
	}
}

func makeEpisodes(n int) []model.MovieUrlInfo {
	out := make([]model.MovieUrlInfo, n)
	for i := 0; i < n; i++ {
		out[i] = model.MovieUrlInfo{
			Episode: fmt.Sprintf("第%02d集", i+1),
			Link:    fmt.Sprintf("http://x/%d.m3u8", i+1),
		}
	}
	return out
}
