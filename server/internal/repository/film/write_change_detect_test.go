package film

import (
	"encoding/json"
	"fmt"
	"sort"
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

// TestDedupePlaylistRowsKeepsLastPerKeyGroup 同一 (movie_key, group_index) 多行（同片多条目
// 共享匹配键）只保留最后一行，与落库唯一键后写覆盖语义一致。
func TestDedupePlaylistRowsKeepsLastPerKeyGroup(t *testing.T) {
	rows := []model.MoviePlaylist{
		{MovieKey: "K", GroupIndex: 0, GroupName: "a", Content: `[{"episode":"01","link":"http://a/1.m3u8"}]`},
		{MovieKey: "K", GroupIndex: 0, GroupName: "a", Content: `[{"episode":"01","link":"http://b/2.m3u8"}]`},
		{MovieKey: "K", GroupIndex: 1, GroupName: "b", Content: `[{"episode":"01","link":"http://c/3.m3u8"}]`},
	}
	// 入参需已排序（生产在 saveGroupedPlaylists 中先排序再去重）
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].MovieKey == rows[j].MovieKey {
			return rows[i].GroupIndex < rows[j].GroupIndex
		}
		return rows[i].MovieKey < rows[j].MovieKey
	})
	got := dedupePlaylistRows(rows)
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	if got[0].GroupIndex != 0 || got[0].Content != `[{"episode":"01","link":"http://b/2.m3u8"}]` {
		t.Fatalf("K/0 应保留最后一行（后写覆盖）: %+v", got[0])
	}
	if got[1].GroupIndex != 1 {
		t.Fatalf("K/1 不应被误去重: %+v", got[1])
	}
}

// TestDiffPlaylistMovieKeysEmptyIncomingNotNotify key 在库中有内容但本次无 incoming
// （源站改名/条目消失的残留）→ 不进更新列表，避免同一 mid 每批反复上报。
func TestDiffPlaylistMovieKeysEmptyIncomingNotNotify(t *testing.T) {
	existing := map[string][]playlistSignature{
		"k": {{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8"}]`}},
	}
	ch := diffPlaylistMovieKeys(existing, map[string][]playlistSignature{"k": nil}, []string{"k"})
	if len(ch) != 1 {
		t.Fatalf("库内残留应产生变更记录（供写库清理），got %d", len(ch))
	}
	if ch[0].NotifyWorthy {
		t.Fatalf("空 incoming 不应通知: %+v", ch[0])
	}
	if ch[0].FirstInsert {
		t.Fatalf("库中已有内容不应视为首次写入: %+v", ch[0])
	}
}

// TestSharedKeyMultiEntryNoFalseNotify 同一影片在源站有多个条目（如「XXX英语」「XXX国语」
// 共享豆瓣匹配键）：页面列表拼接后经排序去重，签名应与库内一致，不再把「多条目并存」
// 误判为剧集结构变化 → 不通知。
func TestSharedKeyMultiEntryNoFalseNotify(t *testing.T) {
	build := func(link string) []model.MoviePlaylist {
		return []model.MoviePlaylist{
			{MovieKey: "douban", GroupIndex: 0, GroupName: "feifan",
				Content: `[{"episode":"第01集","link":"` + link + `"}]`},
			{MovieKey: "title", GroupIndex: 0, GroupName: "feifan",
				Content: `[{"episode":"第01集","link":"` + link + `"}]`},
		}
	}
	// 同一页同时返回英语/国语两个条目，共享 douban 匹配键
	var page []model.MoviePlaylist
	page = append(page, build("http://a/1.m3u8")...)
	page = append(page, build("http://b/2.m3u8")...)
	sort.Slice(page, func(i, j int) bool {
		if page[i].MovieKey == page[j].MovieKey {
			return page[i].GroupIndex < page[j].GroupIndex
		}
		return page[i].MovieKey < page[j].MovieKey
	})
	incoming := buildPlaylistSignatures(dedupePlaylistRows(page))

	// 库内：上次后写覆盖保留的「国语」内容（一行）
	existing := buildPlaylistSignatures([]model.MoviePlaylist{
		{MovieKey: "douban", GroupIndex: 0, GroupName: "feifan",
			Content: `[{"episode":"第01集","link":"http://b/2.m3u8"}]`},
		{MovieKey: "title", GroupIndex: 0, GroupName: "feifan",
			Content: `[{"episode":"第01集","link":"http://b/2.m3u8"}]`},
	})
	changes := diffPlaylistMovieKeys(existing, incoming, []string{"douban", "title"})
	if len(changes) != 0 {
		t.Fatalf("多条目共享 key 去重后应与库内一致，不应产生变更: %+v", changes)
	}
}

// TestPlaylistStructureGrew 仅「新增集数/新增线路」视为值得通知的结构变化；
// 集数相同（顺序/链接变化）与集数回退（源站抖动）都不是新增。
func TestPlaylistStructureGrew(t *testing.T) {
	sig := func(labels ...string) playlistSignature {
		content := make([]model.MovieUrlInfo, 0, len(labels))
		for _, l := range labels {
			content = append(content, model.MovieUrlInfo{Episode: l, Link: "http://a/1.m3u8"})
		}
		data, _ := json.Marshal(content)
		return playlistSignature{GroupIndex: 0, GroupName: "m3u8", Content: string(data)}
	}

	ep16 := []playlistSignature{sig("第01集", "第02集", "第03集", "第04集", "第05集", "第06集", "第07集", "第08集", "第09集", "第10集", "第11集", "第12集", "第13集", "第14集", "第15集", "第16集")}
	ep18 := []playlistSignature{sig("第01集", "第02集", "第03集", "第04集", "第05集", "第06集", "第07集", "第08集", "第09集", "第10集", "第11集", "第12集", "第13集", "第14集", "第15集", "第16集", "第17集", "第18集")}

	if !playlistStructureGrew(ep16, ep18) {
		t.Fatal("16→18 应视为新增集数")
	}
	if playlistStructureGrew(ep18, ep16) {
		t.Fatal("18→16 集数回退不应视为新增")
	}
	// 集数相同但顺序打乱 / 链接变化 → 不是新增
	reordered := []playlistSignature{sig("第16集", "第15集", "第14集", "第13集", "第12集", "第11集", "第10集", "第09集", "第08集", "第07集", "第06集", "第05集", "第04集", "第03集", "第02集", "第01集")}
	if playlistStructureGrew(ep16, reordered) {
		t.Fatal("集数相同仅顺序变化不应视为新增")
	}
	// 新增线路
	twoLines := append(append([]playlistSignature{}, ep16...), playlistSignature{GroupIndex: 1, GroupName: "line2", Content: ep16[0].Content})
	if !playlistStructureGrew(ep16, twoLines) {
		t.Fatal("新增线路应视为结构变化")
	}
	// 线路消失 → 回退
	if playlistStructureGrew(twoLines, ep16) {
		t.Fatal("线路消失不应视为新增")
	}
}

// TestDiffPlaylistMovieKeysGrowthRegression 集数新增 → 通知且写库；
// 集数回退 → 不通知且 SkipWrite（保护已存最大集数）；集数相同仅顺序变化 → 写库但不通知。
func TestDiffPlaylistMovieKeysGrowthRegression(t *testing.T) {
	content := func(labels ...string) string {
		urls := make([]model.MovieUrlInfo, 0, len(labels))
		for _, l := range labels {
			urls = append(urls, model.MovieUrlInfo{Episode: l, Link: "http://a/1.m3u8"})
		}
		data, _ := json.Marshal(urls)
		return string(data)
	}
	ep16 := []playlistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: content("01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12", "13", "14", "15", "16")}}
	ep18 := []playlistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: content("01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12", "13", "14", "15", "16", "17", "18")}}
	reordered := []playlistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: content("16", "15", "14", "13", "12", "11", "10", "09", "08", "07", "06", "05", "04", "03", "02", "01")}}

	// 16 → 18：通知 + 写库
	ch := diffPlaylistMovieKeys(map[string][]playlistSignature{"k": ep16}, map[string][]playlistSignature{"k": ep18}, []string{"k"})
	if len(ch) != 1 || !ch[0].NotifyWorthy || ch[0].SkipWrite {
		t.Fatalf("16→18 应通知且写库: %+v", ch)
	}
	// 18 → 16（源站抖动回退）：不通知 + 不写库
	ch = diffPlaylistMovieKeys(map[string][]playlistSignature{"k": ep18}, map[string][]playlistSignature{"k": ep16}, []string{"k"})
	if len(ch) != 1 || ch[0].NotifyWorthy || !ch[0].SkipWrite {
		t.Fatalf("18→16 回退应不通知且不写库: %+v", ch)
	}
	// 16 → 16（仅顺序变化）：写库但不通知
	ch = diffPlaylistMovieKeys(map[string][]playlistSignature{"k": ep16}, map[string][]playlistSignature{"k": reordered}, []string{"k"})
	if len(ch) != 1 || ch[0].NotifyWorthy || ch[0].SkipWrite {
		t.Fatalf("集数相同仅顺序变化应写库不通知: %+v", ch)
	}
}

// TestMasterStructureGrew 主站「新增集数才通知」语义。
func TestMasterStructureGrew(t *testing.T) {
	detail := func(eps int) model.MovieDetail {
		playlist := make([]model.MovieUrlInfo, 0, eps)
		for i := 1; i <= eps; i++ {
			playlist = append(playlist, model.MovieUrlInfo{Episode: fmt.Sprintf("第%02d集", i), Link: "http://a/1.m3u8"})
		}
		return model.MovieDetail{PlayFrom: []string{"m3u8"}, PlayList: [][]model.MovieUrlInfo{playlist}}
	}
	if !masterStructureGrew(detail(16), detail(18)) {
		t.Fatal("16→18 应视为新增集数")
	}
	if masterStructureGrew(detail(18), detail(16)) {
		t.Fatal("18→16 回退不应视为新增")
	}
	if masterStructureGrew(detail(16), detail(16)) {
		t.Fatal("集数相同不应视为新增")
	}
	if masterStructureRegressed(detail(16), detail(18)) {
		t.Fatal("16→18 不应视为回退")
	}
	if !masterStructureRegressed(detail(18), detail(16)) {
		t.Fatal("18→16 应视为回退")
	}
}
