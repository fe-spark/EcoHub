package film

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

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

func TestStampOnlyRefreshedWhenNotifyWorthy(t *testing.T) {
	const oldStamp int64 = 1_700_000_000
	gdb := openContentKeyTestDB(t)
	old := model.MovieDetail{
		Id: 200, Name: "连载片",
		PlayFrom: []string{"线路1"},
		PlayList: [][]model.MovieUrlInfo{{{Episode: "01", Link: "http://x/1"}}},
		MovieDescriptor: model.MovieDescriptor{Remarks: "更新至01", State: "连载"},
	}
	row := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 200, ContentKey: "vod_200", SourceId: "master"},
		FilmIndexContent:  model.FilmIndexContent{Name: "连载片", UpdateStamp: oldStamp},
	}
	if err := gdb.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	seedDetail(t, gdb, 200, old)

	// 仅备注变化：写库但不刷 stamp
	remarksOnly := old
	remarksOnly.Remarks = "更新至01（修正）"
	infos := []model.FilmIndex{{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 200, ContentKey: "vod_200", SourceId: "master"},
		FilmIndexContent:  model.FilmIndexContent{Name: "连载片", UpdateStamp: time.Now().Unix()},
	}}
	if _, _, _, err := applyMasterBusinessUpdateStampsTx(gdb, infos, map[string]model.MovieDetail{"vod_200": remarksOnly}); err != nil {
		t.Fatal(err)
	}
	if infos[0].UpdateStamp != oldStamp {
		t.Fatalf("remarks-only should keep stamp %d, got %d", oldStamp, infos[0].UpdateStamp)
	}

	// 集数增加：与概要 NotifyMIDs 一致，刷 stamp
	moreEps := old
	moreEps.PlayList = [][]model.MovieUrlInfo{
		{{Episode: "01", Link: "http://x/1"}, {Episode: "02", Link: "http://x/2"}},
	}
	moreEps.Remarks = "更新至02"
	infos2 := []model.FilmIndex{{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 200, ContentKey: "vod_200", SourceId: "master"},
		FilmIndexContent:  model.FilmIndexContent{Name: "连载片", UpdateStamp: time.Now().Unix()},
	}}
	if _, _, _, err := applyMasterBusinessUpdateStampsTx(gdb, infos2, map[string]model.MovieDetail{"vod_200": moreEps}); err != nil {
		t.Fatal(err)
	}
	if infos2[0].UpdateStamp <= oldStamp {
		t.Fatalf("episode increase should bump stamp, got %d", infos2[0].UpdateStamp)
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

func TestPlaylistLastEpisodeChanged(t *testing.T) {
	left := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8?sign=aaa"},{"episode":"02","link":"http://a/2.m3u8"}]`},
	}
	linkOnly := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://cdn/x/1.m3u8?sign=zzz"},{"episode":"02","link":"http://cdn/x/2.m3u8?t=1"}]`},
	}
	if playlistLastEpisodeChanged(left, linkOnly) {
		t.Fatal("仅链接变化（最后一集仍为 02）不应视为更新")
	}
	// 中间集链接变化但最后一项不变 → 不算
	midChanged := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8"},{"episode":"02","link":"http://b/2.m3u8"}]`},
	}
	if playlistLastEpisodeChanged(left, midChanged) {
		t.Fatal("中间集链接/内容变化但最后一项未变不应视为更新")
	}
	epAdded := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8"},{"episode":"02","link":"http://a/2.m3u8"},{"episode":"03","link":"http://a/3.m3u8"}]`},
	}
	if !playlistLastEpisodeChanged(left, epAdded) {
		t.Fatal("增集（最后一集 02→03）应视为更新")
	}
	// 集数回退（02→01）：用户要求「不一样算更新」
	epRegressed := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8"}]`},
	}
	if !playlistLastEpisodeChanged(left, epRegressed) {
		t.Fatal("集数回退（最后一集 02→01）应视为更新")
	}
	// 顺序变化：最后一项从 02 变 01 → 判为变化（实现依赖源站有序返回）
	reordered := []playlistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"02","link":"http://a/2.m3u8"},{"episode":"01","link":"http://a/1.m3u8"}]`},
	}
	if !playlistLastEpisodeChanged(left, reordered) {
		t.Fatal("顺序变化导致最后一项不同，应判为变化")
	}
	// 新增线路 → 变化
	twoLines := append(append([]playlistSignature{}, left...), playlistSignature{GroupIndex: 1, GroupName: "line2", Content: left[0].Content})
	if !playlistLastEpisodeChanged(left, twoLines) {
		t.Fatal("新增线路应视为更新")
	}
	// 线路消失 → 变化
	if !playlistLastEpisodeChanged(twoLines, left) {
		t.Fatal("线路消失应视为更新")
	}
	// diff：链接变化要写库但不 NotifyWorthy；增集 NotifyWorthy
	existing := map[string][]playlistSignature{"k": left}
	incomingLink := map[string][]playlistSignature{"k": linkOnly}
	ch := diffPlaylistMovieKeys(existing, incomingLink, []string{"k"})
	if len(ch) != 1 || ch[0].NotifyWorthy {
		t.Fatalf("仅链接变化应写库且不通知: %+v", ch)
	}
	incomingEp := map[string][]playlistSignature{"k": epAdded}
	ch2 := diffPlaylistMovieKeys(existing, incomingEp, []string{"k"})
	if len(ch2) != 1 || !ch2[0].NotifyWorthy {
		t.Fatalf("增集应写库且通知: %+v", ch2)
	}
	incomingRegressed := map[string][]playlistSignature{"k": epRegressed}
	ch3 := diffPlaylistMovieKeys(existing, incomingRegressed, []string{"k"})
	if len(ch3) != 1 || !ch3[0].NotifyWorthy {
		t.Fatalf("回退应写库且通知: %+v", ch3)
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
	got := filterPlayStructureNotifyMIDs(changed, details, old, nil)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("want [1,3], got %v", got)
	}
}

func TestFilterPlayStructureNotifyMIDsSkipsWhenOtherSourceAlreadyHasCount(t *testing.T) {
	changed := []model.FilmIndex{
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 9, ContentKey: "k9"}},
	}
	details := map[string]model.MovieDetail{
		"k9": {PlayList: [][]model.MovieUrlInfo{{{Episode: "01"}, {Episode: "02"}}}},
	}
	old := map[int64]model.MovieDetail{
		9: {PlayList: [][]model.MovieUrlInfo{{{Episode: "01"}}}},
	}
	// 附属站已有 2 集：主站 1→2 写详情，不重进最近更新
	got := filterPlayStructureNotifyMIDs(changed, details, old, map[int64][]int{9: {2}})
	if len(got) != 0 {
		t.Fatalf("其它源已有相同集数不应通知，got %v", got)
	}
	// 全库仍是 1 集：主站先到 2 应通知
	got = filterPlayStructureNotifyMIDs(changed, details, old, nil)
	if len(got) != 1 || got[0] != 9 {
		t.Fatalf("全库最大还是 1 时主站 1→2 应通知，got %v", got)
	}
}

func TestFilterPlayStructureNotifyMIDsMiddleInsertSameLastLabel(t *testing.T) {
	oldDetail := model.MovieDetail{PlayList: [][]model.MovieUrlInfo{
		{{Episode: "01"}, {Episode: "02"}, {Episode: "完结"}},
	}}
	newDetail := model.MovieDetail{PlayList: [][]model.MovieUrlInfo{
		{{Episode: "01"}, {Episode: "02"}, {Episode: "03"}, {Episode: "完结"}},
	}}
	changed := []model.FilmIndex{{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 4, ContentKey: "k4"}}}
	got := filterPlayStructureNotifyMIDs(changed, map[string]model.MovieDetail{"k4": newDetail}, map[int64]model.MovieDetail{4: oldDetail}, nil)
	if len(got) != 1 {
		t.Fatalf("中间插集且全库最大未到新集数时应通知，got %v", got)
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

// TestDiffPlaylistMovieKeysGrowthRegression 集数新增/回退 → 通知且写库；
// 集数相同仅顺序/链接变化 → 写库但不通知。
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
	if len(ch) != 1 || !ch[0].NotifyWorthy {
		t.Fatalf("16→18 应通知且写库: %+v", ch)
	}
	// 18 → 16（回退）：同样通知 + 写库（用户要求「不一样算更新」）
	ch = diffPlaylistMovieKeys(map[string][]playlistSignature{"k": ep18}, map[string][]playlistSignature{"k": ep16}, []string{"k"})
	if len(ch) != 1 || !ch[0].NotifyWorthy {
		t.Fatalf("18→16 回退应通知且写库: %+v", ch)
	}
	// 16 → 16（顺序打乱，最后一项从 16 变 01）：判为变化 → 通知（依赖源站有序）
	ch = diffPlaylistMovieKeys(map[string][]playlistSignature{"k": ep16}, map[string][]playlistSignature{"k": reordered}, []string{"k"})
	if len(ch) != 1 || !ch[0].NotifyWorthy {
		t.Fatalf("顺序变化导致最后一项不同应通知: %+v", ch)
	}
	// 16 → 16（仅第 1 集链接变化，最后一项仍为 16）：写库但不通知
	midLinkChanged := []playlistSignature{{
		GroupIndex: 0,
		GroupName:  "m3u8",
		Content:    strings.Replace(ep16[0].Content, `"link":"http://a/1.m3u8"`, `"link":"http://b/9.m3u8"`, 1),
	}}
	ch = diffPlaylistMovieKeys(map[string][]playlistSignature{"k": ep16}, map[string][]playlistSignature{"k": midLinkChanged}, []string{"k"})
	if len(ch) != 1 || ch[0].NotifyWorthy {
		t.Fatalf("最后一项相同仅链接变化应写库不通知: %+v", ch)
	}
}

// TestMasterLastEpisodeChanged 主站「任一线路最后一集变化才通知」语义（回退也算）。
func TestMasterLastEpisodeChanged(t *testing.T) {
	detail := func(eps int) model.MovieDetail {
		playlist := make([]model.MovieUrlInfo, 0, eps)
		for i := 1; i <= eps; i++ {
			playlist = append(playlist, model.MovieUrlInfo{Episode: fmt.Sprintf("第%02d集", i), Link: "http://a/1.m3u8"})
		}
		return model.MovieDetail{PlayFrom: []string{"m3u8"}, PlayList: [][]model.MovieUrlInfo{playlist}}
	}
	if !masterLastEpisodeChanged(detail(16), detail(18)) {
		t.Fatal("16→18 应视为最后一集变化")
	}
	if !masterLastEpisodeChanged(detail(18), detail(16)) {
		t.Fatal("18→16 回退也应视为最后一集变化")
	}
	if masterLastEpisodeChanged(detail(16), detail(16)) {
		t.Fatal("最后一集相同不应视为变化")
	}
}

func TestDiffPlaylistCountIncreasedMiddleInsert(t *testing.T) {
	content := func(labels ...string) string {
		urls := make([]model.MovieUrlInfo, 0, len(labels))
		for _, l := range labels {
			urls = append(urls, model.MovieUrlInfo{Episode: l, Link: "http://a/1.m3u8"})
		}
		data, _ := json.Marshal(urls)
		return string(data)
	}
	oldSig := []playlistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: content("01", "02", "完结")}}
	newSig := []playlistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: content("01", "02", "03", "完结")}}
	ch := diffPlaylistMovieKeys(map[string][]playlistSignature{"k": oldSig}, map[string][]playlistSignature{"k": newSig}, []string{"k"})
	if len(ch) != 1 {
		t.Fatalf("中间插集应写库, got %+v", ch)
	}
	if ch[0].NotifyWorthy {
		t.Fatalf("最后一项仍是完结，NotifyWorthy 应为 false: %+v", ch[0])
	}
	if !ch[0].CountIncreased {
		t.Fatalf("集数 3→4 应为 CountIncreased: %+v", ch[0])
	}
	if !slaveShouldBumpStamp(ch[0], nil) {
		t.Fatal("全库还没有 4 集时中间插集应顶最近更新")
	}
	if slaveShouldBumpStamp(ch[0], []int{4}) {
		t.Fatal("其它源已有 4 集时不应重进最近更新")
	}
}

func TestSlaveShouldBumpStamp(t *testing.T) {
	eps := func(n int) []playlistSignature {
		urls := make([]model.MovieUrlInfo, 0, n)
		for i := 1; i <= n; i++ {
			urls = append(urls, model.MovieUrlInfo{Episode: fmt.Sprintf("%02d", i), Link: "http://a/1"})
		}
		data, _ := json.Marshal(urls)
		return []playlistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: string(data)}}
	}
	// 首次写入：全库已有 12 集，本源也是 12 → 不刷屏
	same := playlistChange{FirstInsert: true, NotifyWorthy: true, CountIncreased: true, Signatures: eps(12)}
	if slaveShouldBumpStamp(same, []int{12}) {
		t.Fatal("同集数新源不应顶最近更新")
	}
	// 首次写入：全库 10，本源 12 → 第一个写到 12 的源，stamp=现在
	catchUp := playlistChange{FirstInsert: true, NotifyWorthy: true, CountIncreased: true, Signatures: eps(12)}
	if !slaveShouldBumpStamp(catchUp, []int{10}) {
		t.Fatal("附属站先追集且超过全库最大应顶最近更新")
	}
	// 非首次：仅链接
	linkOnly := playlistChange{NotifyWorthy: false, CountIncreased: false, Signatures: eps(12)}
	if slaveShouldBumpStamp(linkOnly, []int{10}) {
		t.Fatal("仅链接变化不应顶最近更新")
	}
	// 非首次：自己 119→120，但其它源已经 120
	late := playlistChange{NotifyWorthy: true, CountIncreased: true, Signatures: eps(120)}
	if slaveShouldBumpStamp(late, []int{120}) {
		t.Fatal("后到的源追到同一集数不应重进最近更新")
	}
	// 非首次：自己 119→120，全库还是 119
	first := playlistChange{NotifyWorthy: true, CountIncreased: true, PrevMaxCount: 119, Signatures: eps(120)}
	if !slaveShouldBumpStamp(first, []int{119}) {
		t.Fatal("第一个写到 120 的源应顶最近更新")
	}
	// 已领先 120，仅最后一集标签变化：写前全库最大已是自己的 120，不重进
	lead := playlistChange{NotifyWorthy: true, PrevMaxCount: 120, Signatures: eps(120)}
	if slaveShouldBumpStamp(lead, []int{119}) {
		t.Fatal("已领先源仅改最后一集标签不应重进最近更新")
	}
}

// TestLastEpisodeLabel 最后一集 = 源站返回顺序的最后一个非空分集标签原文（不解析数字）。
func TestLastEpisodeLabel(t *testing.T) {
	u := func(ep string) model.MovieUrlInfo { return model.MovieUrlInfo{Episode: ep} }
	// 电视剧递增集数
	if got := lastEpisodeLabel([]model.MovieUrlInfo{u("第01集"), u("第02集"), u("第03集")}); got != "第03集" {
		t.Fatalf("want 第03集, got %q", got)
	}
	// 综艺日期型
	if got := lastEpisodeLabel([]model.MovieUrlInfo{u("第20240107期"), u("第20260809期")}); got != "第20260809期" {
		t.Fatalf("want 第20260809期, got %q", got)
	}
	// 综艺同日分片（上/中/纯享）：取源站顺序最后一个分片
	if got := lastEpisodeLabel([]model.MovieUrlInfo{u("第20260810期上"), u("第20260810期中"), u("第20260810期中纯享")}); got != "第20260810期中纯享" {
		t.Fatalf("want 第20260810期中纯享, got %q", got)
	}
	// 无数字标签（电影 HD/正片）
	if got := lastEpisodeLabel([]model.MovieUrlInfo{u("正片"), u("HD")}); got != "HD" {
		t.Fatalf("want HD, got %q", got)
	}
	// 跳过空标签，取最后一个非空
	if got := lastEpisodeLabel([]model.MovieUrlInfo{u("第01集"), {Episode: "  "}, u("第02集")}); got != "第02集" {
		t.Fatalf("want 第02集, got %q", got)
	}
	// 空列表
	if got := lastEpisodeLabel(nil); got != "" {
		t.Fatalf("空列表 want 空, got %q", got)
	}
}
