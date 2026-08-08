package film

import (
	"testing"

	"server/internal/model"
)

func TestSameStoredMasterDetailIgnoresVolatileFields(t *testing.T) {
	base := model.MovieDetail{
		Id:       100,
		Name:     "测试片",
		PlayFrom: []string{"ffm3u8"},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "01", Link: "http://a/1.m3u8"}},
		},
		MovieDescriptor: model.MovieDescriptor{
			Remarks:    "更新至01集",
			State:      "连载",
			Hits:       100,
			UpdateTime: "2024-01-01 12:00:00",
			AddTime:    1700000000,
			DbScore:    "7.5",
		},
	}
	// 仅热度/时间/封面 query 变化 → 不算业务更新
	noisy := base
	noisy.Id = 999 // 源站 id 与全局 mid 不同也应忽略
	noisy.Hits = 99999
	noisy.UpdateTime = "2026-08-07 16:00:00"
	noisy.AddTime = 1800000000
	noisy.DbScore = "8.1"
	noisy.Picture = "http://cdn.example.com/a.jpg?sign=xyz"
	base.Picture = "http://cdn.example.com/a.jpg?sign=abc"
	if !sameStoredMasterDetail(base, noisy) {
		t.Fatal("hits/time/score/封面query 变化不应视为内容更新")
	}

	// 剧集变化 → 算更新
	episodeChanged := base
	episodeChanged.PlayList = [][]model.MovieUrlInfo{
		{{Episode: "01", Link: "http://a/1.m3u8"}, {Episode: "02", Link: "http://a/2.m3u8"}},
	}
	episodeChanged.Remarks = "更新至02集"
	if sameStoredMasterDetail(base, episodeChanged) {
		t.Fatal("剧集/备注变化应视为内容更新")
	}

	// 片名变化 → 算更新
	nameChanged := base
	nameChanged.Name = "测试片（改名）"
	if sameStoredMasterDetail(base, nameChanged) {
		t.Fatal("片名变化应视为内容更新")
	}
}

func TestMasterBusinessSignatureStableEmptySlices(t *testing.T) {
	a := model.MovieDetail{Name: "x", PlayFrom: nil}
	b := model.MovieDetail{Name: "x", PlayFrom: []string{}}
	if masterBusinessSignature(a) != masterBusinessSignature(b) {
		t.Fatal("nil 与 empty playFrom 应等价")
	}
}

func TestMasterSignatureIgnoresPlaylistLinkQuery(t *testing.T) {
	base := model.MovieDetail{
		Name:     "测试片",
		PlayFrom: []string{"ffm3u8"},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "01", Link: "http://a/1.m3u8?sign=aaa"}},
		},
	}
	noisy := base
	noisy.PlayList = [][]model.MovieUrlInfo{
		{{Episode: "01", Link: "http://a/1.m3u8?sign=bbb"}},
	}
	if masterBusinessSignature(base) != masterBusinessSignature(noisy) {
		t.Fatal("播放链接 query（签名）变化不应视为内容更新")
	}
	realChanged := base
	realChanged.PlayList = [][]model.MovieUrlInfo{
		{{Episode: "01", Link: "http://b/2.m3u8?sign=ccc"}},
	}
	if masterBusinessSignature(base) == masterBusinessSignature(realChanged) {
		t.Fatal("播放链接地址变化应视为内容更新")
	}
}

func TestSamePlaylistSignaturesIgnoresLinkQuery(t *testing.T) {
	left := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8?sign=aaa"}]`},
	}
	right := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8?sign=bbb"}]`},
	}
	if !samePlaylistSignatures(left, right) {
		t.Fatal("播放链接 query（签名）变化不应视为播放源实质变更")
	}
	realChanged := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://b/2.m3u8?sign=ccc"}]`},
	}
	if samePlaylistSignatures(left, realChanged) {
		t.Fatal("播放链接地址变化应视为播放源实质变更")
	}
}

func TestPlaylistEpisodeStructureIgnoresLinkOnly(t *testing.T) {
	left := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8?sign=aaa"},{"episode":"02","link":"http://a/2.m3u8"}]`},
	}
	linkOnly := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://cdn/x/1.m3u8?sign=zzz"},{"episode":"02","link":"http://cdn/x/2.m3u8?t=1"}]`},
	}
	if !samePlaylistEpisodeStructure(left, linkOnly) {
		t.Fatal("仅链接变化不应视为剧集结构变更")
	}
	epAdded := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8"},{"episode":"02","link":"http://a/2.m3u8"},{"episode":"03","link":"http://a/3.m3u8"}]`},
	}
	if samePlaylistEpisodeStructure(left, epAdded) {
		t.Fatal("增集应视为剧集结构变更")
	}
	// diff：链接变化要写库但不 NotifyWorthy；增集 NotifyWorthy
	existing := map[string][]playlistSignature{"k": left}
	incomingLink := map[string][]playlistSignature{"k": linkOnly}
	ch := diffPlaylistMovieKeys(existing, incomingLink, []string{"k"})
	if len(ch) != 1 || ch[0].NotifyWorthy {
		t.Fatalf("链接变化应写库且不通知: %+v", ch)
	}
	incomingEp := map[string][]playlistSignature{"k": epAdded}
	ch2 := diffPlaylistMovieKeys(existing, incomingEp, []string{"k"})
	if len(ch2) != 1 || !ch2[0].NotifyWorthy {
		t.Fatalf("增集应写库且通知: %+v", ch2)
	}
}

func TestPickBestMidForMatchKeySingle(t *testing.T) {
	if got := pickBestMidForMatchKey([]int64{0, 42, 42}); got != 42 {
		t.Fatalf("want 42, got %d", got)
	}
	if got := pickBestMidForMatchKey(nil); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}

func TestMasterSignatureIgnoresNamePunctNoise(t *testing.T) {
	base := model.MovieDetail{Name: "烬九州：第四季"}
	noisy := base
	noisy.Name = "烬九州第四季"
	if masterBusinessSignature(base) != masterBusinessSignature(noisy) {
		t.Fatal("片名标点/空白差异不应视为内容更新")
	}
	renamed := base
	renamed.Name = "烬九州第五季"
	if masterBusinessSignature(base) == masterBusinessSignature(renamed) {
		t.Fatal("片名实质变化应视为内容更新")
	}
}

func TestSamePlayStructureIgnoresMetaAndLinkNoise(t *testing.T) {
	base := model.MovieDetail{
		Name:     "烬九州：第四季",
		PlayFrom: []string{"ffm3u8"},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "01", Link: "http://a/1.m3u8?sign=aaa"}, {Episode: "02", Link: "http://a/2.m3u8?sign=aaa"}},
		},
		MovieDescriptor: model.MovieDescriptor{Remarks: "更新至02集"},
	}
	// 片名标点 + 备注 + 链接签名变化 → 播放结构相同
	noisy := base
	noisy.Name = "烬九州第四季"
	noisy.Remarks = "更新至第02集"
	noisy.PlayList = [][]model.MovieUrlInfo{
		{{Episode: "01", Link: "http://a/1.m3u8?sign=bbb"}, {Episode: "02", Link: "http://a/2.m3u8?sign=ccc"}},
	}
	if !samePlayStructure(base, noisy) {
		t.Fatal("元数据/链接噪声不应视为播放结构变更")
	}
	// 增集 → 结构变化
	episodeAdded := base
	episodeAdded.PlayList = [][]model.MovieUrlInfo{
		{
			{Episode: "01", Link: "http://a/1.m3u8"},
			{Episode: "02", Link: "http://a/2.m3u8"},
			{Episode: "03", Link: "http://a/3.m3u8"},
		},
	}
	if samePlayStructure(base, episodeAdded) {
		t.Fatal("新增集数应视为播放结构变更")
	}
}

func TestFilterPlayStructureNotifyMIDs(t *testing.T) {
	changed := []model.FilmIndex{
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, ContentKey: "k1"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, ContentKey: "k2"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, ContentKey: "k3"}},
	}
	details := map[string]model.MovieDetail{
		"k1": {PlayFrom: []string{"a"}, PlayList: [][]model.MovieUrlInfo{{{Episode: "01", Link: "http://x/1"}}}},
		"k2": {PlayFrom: []string{"a"}, PlayList: [][]model.MovieUrlInfo{{{Episode: "01", Link: "http://x/1"}}}},
		"k3": {PlayFrom: []string{"a"}, PlayList: [][]model.MovieUrlInfo{{{Episode: "01", Link: "http://x/1"}, {Episode: "02", Link: "http://x/2"}}}},
	}
	old := map[int64]model.MovieDetail{
		// mid=1 无旧详情 → 新片，应通知
		// mid=2 结构相同（仅链接不同）→ 不通知
		2: {PlayFrom: []string{"a"}, PlayList: [][]model.MovieUrlInfo{{{Episode: "01", Link: "http://old/1?s=1"}}}},
		// mid=3 多一集 → 通知
		3: {PlayFrom: []string{"a"}, PlayList: [][]model.MovieUrlInfo{{{Episode: "01", Link: "http://old/1"}}}},
	}
	got := filterPlayStructureNotifyMIDs(changed, details, old)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("want [1,3], got %v", got)
	}
}
