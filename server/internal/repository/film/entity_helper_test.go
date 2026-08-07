package film

import (
	"fmt"
	"strings"
	"testing"

	"server/internal/model"
)

func TestBuildContentKeyPrefersSourceVodID(t *testing.T) {
	// 源站并存：片名规范化后相同，但 vod_id / 集数不同，必须得到不同 ContentKey
	a := model.MovieDetail{
		Id:   87682,
		Name: "烬九州第四季",
		PlayList: [][]model.MovieUrlInfo{
			makeEpisodes(145),
		},
	}
	b := model.MovieDetail{
		Id:   87676,
		Name: "烬九州：第四季",
		PlayList: [][]model.MovieUrlInfo{
			makeEpisodes(91),
		},
	}
	ka, kb := BuildContentKey(a), BuildContentKey(b)
	if ka == "" || kb == "" {
		t.Fatalf("content key empty: a=%q b=%q", ka, kb)
	}
	if ka == kb {
		t.Fatalf("不同 vod_id 不应共用 ContentKey: both=%q", ka)
	}
	if ka != "vod_87682" || kb != "vod_87676" {
		t.Fatalf("want vod_{vod_id}, got a=%q b=%q", ka, kb)
	}
}

func TestBuildContentKeyFallsBackToNameWithoutID(t *testing.T) {
	// 无源站 ID（手工等）仍回退 name hash；标点差异应得到同一 key
	a := model.MovieDetail{Name: "烬九州：第四季"}
	b := model.MovieDetail{Name: "烬九州第四季"}
	ka, kb := BuildContentKey(a), BuildContentKey(b)
	if ka == "" || !strings.HasPrefix(ka, "name_") {
		t.Fatalf("无 id 应回退 name_* , got %q", ka)
	}
	if ka != kb {
		t.Fatalf("无 id 时规范化片名应同 key: %q vs %q", ka, kb)
	}
}

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

func TestBuildContentKeyPrefersVodOverDouban(t *testing.T) {
	d := model.MovieDetail{
		Id:              99,
		Name:            "某剧",
		MovieDescriptor: model.MovieDescriptor{DbId: 123456},
	}
	if got := BuildContentKey(d); got != "vod_99" {
		t.Fatalf("want vod_99, got %q", got)
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
