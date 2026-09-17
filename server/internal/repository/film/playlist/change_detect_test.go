package playlist

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"server/internal/model"
)

func TestSamePlaylistSignaturesIgnoresLinkQuery(t *testing.T) {
	left := []PlaylistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8?sign=aaa"}]`},
	}
	right := []PlaylistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8?sign=bbb"}]`},
	}
	if !SamePlaylistSignatures(left, right) {
		t.Fatal("播放链接 query（签名）变化不应视为播放源实质变更")
	}
	realChanged := []PlaylistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://b/2.m3u8?sign=ccc"}]`},
	}
	if SamePlaylistSignatures(left, realChanged) {
		t.Fatal("播放链接地址变化应视为播放源实质变更")
	}
}

func TestPlaylistLastEpisodeChanged(t *testing.T) {
	left := []PlaylistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8?sign=aaa"},{"episode":"02","link":"http://a/2.m3u8"}]`},
	}
	linkOnly := []PlaylistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://cdn/x/1.m3u8?sign=zzz"},{"episode":"02","link":"http://cdn/x/2.m3u8?t=1"}]`},
	}
	if PlaylistLastEpisodeChanged(left, linkOnly) {
		t.Fatal("仅链接变化（最后一集仍为 02）不应视为更新")
	}
	// 中间集链接变化但最后一项不变 → 不算
	midChanged := []PlaylistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8"},{"episode":"02","link":"http://b/2.m3u8"}]`},
	}
	if PlaylistLastEpisodeChanged(left, midChanged) {
		t.Fatal("中间集链接/内容变化但最后一项未变不应视为更新")
	}
	epAdded := []PlaylistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8"},{"episode":"02","link":"http://a/2.m3u8"},{"episode":"03","link":"http://a/3.m3u8"}]`},
	}
	if !PlaylistLastEpisodeChanged(left, epAdded) {
		t.Fatal("增集（最后一集 02→03）应视为更新")
	}
	// 集数回退（02→01）：用户要求「不一样算更新」
	epRegressed := []PlaylistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8"}]`},
	}
	if !PlaylistLastEpisodeChanged(left, epRegressed) {
		t.Fatal("集数回退（最后一集 02→01）应视为更新")
	}
	// 顺序变化：最后一项从 02 变 01 → 判为变化（实现依赖源站有序返回）
	reordered := []PlaylistSignature{
		{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"02","link":"http://a/2.m3u8"},{"episode":"01","link":"http://a/1.m3u8"}]`},
	}
	if !PlaylistLastEpisodeChanged(left, reordered) {
		t.Fatal("顺序变化导致最后一项不同，应判为变化")
	}
	// 新增线路 → 变化
	twoLines := append(append([]PlaylistSignature{}, left...), PlaylistSignature{GroupIndex: 1, GroupName: "line2", Content: left[0].Content})
	if !PlaylistLastEpisodeChanged(left, twoLines) {
		t.Fatal("新增线路应视为更新")
	}
	// 线路消失 → 变化
	if !PlaylistLastEpisodeChanged(twoLines, left) {
		t.Fatal("线路消失应视为更新")
	}
	// diff：链接变化要写库但不 NotifyWorthy；增集 NotifyWorthy
	existing := map[string][]PlaylistSignature{"k": left}
	incomingLink := map[string][]PlaylistSignature{"k": linkOnly}
	ch := diffPlaylistMovieKeys(existing, incomingLink, []string{"k"})
	if len(ch) != 1 || ch[0].NotifyWorthy {
		t.Fatalf("仅链接变化应写库且不通知: %+v", ch)
	}
	incomingEp := map[string][]PlaylistSignature{"k": epAdded}
	ch2 := diffPlaylistMovieKeys(existing, incomingEp, []string{"k"})
	if len(ch2) != 1 || !ch2[0].NotifyWorthy {
		t.Fatalf("增集应写库且通知: %+v", ch2)
	}
	incomingRegressed := map[string][]PlaylistSignature{"k": epRegressed}
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

// TestDedupePlaylistRowsKeepsLastPerKeyGroup 同一 (movie_key, group_index) 多行（同片多条目
// 共享匹配键）只保留最后一行，与落库唯一键后写覆盖语义一致。
func TestDedupePlaylistRowsKeepsLastPerKeyGroup(t *testing.T) {
	rows := []model.SlaveMoviePlaylist{
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
	got := DedupePlaylistRows(rows)
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

// TestDiffPlaylistMovieKeysEmptyIncomingNotNotify key 在库中有内容但本次无 incoming
// （源站改名/条目消失的残留）→ 不进更新列表，避免同一 mid 每批反复上报。
func TestDiffPlaylistMovieKeysEmptyIncomingNotNotify(t *testing.T) {
	existing := map[string][]PlaylistSignature{
		"k": {{GroupIndex: 0, GroupName: "m3u8", Content: `[{"episode":"01","link":"http://a/1.m3u8"}]`}},
	}
	ch := diffPlaylistMovieKeys(existing, map[string][]PlaylistSignature{"k": nil}, []string{"k"})
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

// TestSharedKeyMultiEntryNoFalseNotify 同一影片在源站有多个条目（如「XXX英语」「XXX国语」
// 共享豆瓣匹配键）：页面列表拼接后经排序去重，签名应与库内一致，不再把「多条目并存」
// 误判为剧集结构变化 → 不通知。
func TestSharedKeyMultiEntryNoFalseNotify(t *testing.T) {
	build := func(link string) []model.SlaveMoviePlaylist {
		return []model.SlaveMoviePlaylist{
			{MovieKey: "douban", GroupIndex: 0, GroupName: "feifan",
				Content: `[{"episode":"第01集","link":"` + link + `"}]`},
			{MovieKey: "title", GroupIndex: 0, GroupName: "feifan",
				Content: `[{"episode":"第01集","link":"` + link + `"}]`},
		}
	}
	// 同一页同时返回英语/国语两个条目，共享 douban 匹配键
	var page []model.SlaveMoviePlaylist
	page = append(page, build("http://a/1.m3u8")...)
	page = append(page, build("http://b/2.m3u8")...)
	sort.Slice(page, func(i, j int) bool {
		if page[i].MovieKey == page[j].MovieKey {
			return page[i].GroupIndex < page[j].GroupIndex
		}
		return page[i].MovieKey < page[j].MovieKey
	})
	incoming := buildPlaylistSignatures(DedupePlaylistRows(page))

	// 库内：上次后写覆盖保留的「国语」内容（一行）
	existing := buildPlaylistSignatures([]model.SlaveMoviePlaylist{
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
	ep16 := []PlaylistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: content("01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12", "13", "14", "15", "16")}}
	ep18 := []PlaylistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: content("01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12", "13", "14", "15", "16", "17", "18")}}
	reordered := []PlaylistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: content("16", "15", "14", "13", "12", "11", "10", "09", "08", "07", "06", "05", "04", "03", "02", "01")}}

	// 16 → 18：通知 + 写库
	ch := diffPlaylistMovieKeys(map[string][]PlaylistSignature{"k": ep16}, map[string][]PlaylistSignature{"k": ep18}, []string{"k"})
	if len(ch) != 1 || !ch[0].NotifyWorthy {
		t.Fatalf("16→18 应通知且写库: %+v", ch)
	}
	// 18 → 16（回退）：同样通知 + 写库（用户要求「不一样算更新」）
	ch = diffPlaylistMovieKeys(map[string][]PlaylistSignature{"k": ep18}, map[string][]PlaylistSignature{"k": ep16}, []string{"k"})
	if len(ch) != 1 || !ch[0].NotifyWorthy {
		t.Fatalf("18→16 回退应通知且写库: %+v", ch)
	}
	// 16 → 16（顺序打乱，最后一项从 16 变 01）：判为变化 → 通知（依赖源站有序）
	ch = diffPlaylistMovieKeys(map[string][]PlaylistSignature{"k": ep16}, map[string][]PlaylistSignature{"k": reordered}, []string{"k"})
	if len(ch) != 1 || !ch[0].NotifyWorthy {
		t.Fatalf("顺序变化导致最后一项不同应通知: %+v", ch)
	}
	// 16 → 16（仅第 1 集链接变化，最后一项仍为 16）：写库但不通知
	midLinkChanged := []PlaylistSignature{{
		GroupIndex: 0,
		GroupName:  "m3u8",
		Content:    strings.Replace(ep16[0].Content, `"link":"http://a/1.m3u8"`, `"link":"http://b/9.m3u8"`, 1),
	}}
	ch = diffPlaylistMovieKeys(map[string][]PlaylistSignature{"k": ep16}, map[string][]PlaylistSignature{"k": midLinkChanged}, []string{"k"})
	if len(ch) != 1 || ch[0].NotifyWorthy {
		t.Fatalf("最后一项相同仅链接变化应写库不通知: %+v", ch)
	}
}

// TestMasterLastEpisodeChanged 主站「任一线路最后一集变化才通知」语义（回退也算）。

func TestDiffPlaylistCountIncreasedMiddleInsert(t *testing.T) {
	content := func(labels ...string) string {
		urls := make([]model.MovieUrlInfo, 0, len(labels))
		for _, l := range labels {
			urls = append(urls, model.MovieUrlInfo{Episode: l, Link: "http://a/1.m3u8"})
		}
		data, _ := json.Marshal(urls)
		return string(data)
	}
	oldSig := []PlaylistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: content("01", "02", "完结")}}
	newSig := []PlaylistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: content("01", "02", "03", "完结")}}
	ch := diffPlaylistMovieKeys(map[string][]PlaylistSignature{"k": oldSig}, map[string][]PlaylistSignature{"k": newSig}, []string{"k"})
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
	eps := func(n int) []PlaylistSignature {
		urls := make([]model.MovieUrlInfo, 0, n)
		for i := 1; i <= n; i++ {
			urls = append(urls, model.MovieUrlInfo{Episode: fmt.Sprintf("%02d", i), Link: "http://a/1"})
		}
		data, _ := json.Marshal(urls)
		return []PlaylistSignature{{GroupIndex: 0, GroupName: "m3u8", Content: string(data)}}
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

// TestLastEpisodeLabel 最后一集 = 源站返回顺序的最后一个非空分集标签原文（不解析数字）。
func TestLastEpisodeLabel(t *testing.T) {
	u := func(ep string) model.MovieUrlInfo { return model.MovieUrlInfo{Episode: ep} }
	// 电视剧递增集数
	if got := LastEpisodeLabel([]model.MovieUrlInfo{u("第01集"), u("第02集"), u("第03集")}); got != "第03集" {
		t.Fatalf("want 第03集, got %q", got)
	}
	// 综艺日期型
	if got := LastEpisodeLabel([]model.MovieUrlInfo{u("第20240107期"), u("第20260809期")}); got != "第20260809期" {
		t.Fatalf("want 第20260809期, got %q", got)
	}
	// 综艺同日分片（上/中/纯享）：取源站顺序最后一个分片
	if got := LastEpisodeLabel([]model.MovieUrlInfo{u("第20260810期上"), u("第20260810期中"), u("第20260810期中纯享")}); got != "第20260810期中纯享" {
		t.Fatalf("want 第20260810期中纯享, got %q", got)
	}
	// 无数字标签（电影 HD/正片）
	if got := LastEpisodeLabel([]model.MovieUrlInfo{u("正片"), u("HD")}); got != "HD" {
		t.Fatalf("want HD, got %q", got)
	}
	// 跳过空标签，取最后一个非空
	if got := LastEpisodeLabel([]model.MovieUrlInfo{u("第01集"), {Episode: "  "}, u("第02集")}); got != "第02集" {
		t.Fatalf("want 第02集, got %q", got)
	}
	// 空列表
	if got := LastEpisodeLabel(nil); got != "" {
		t.Fatalf("空列表 want 空, got %q", got)
	}
}

// TestSaveGroupedPlaylists_NoOpShortCircuitAndPartialUpdate 验证无变更短路与局部更新机制：
// 1. 无变更时直接短路返回，不开启写事务、不变更自增 ID；
// 2. 有局部变更时，仅针对变更 key 开启事务执行局部重写，未变更的 key 保持原样不动。

// TestSaveGroupedPlaylists_NoOpShortCircuitAndPartialUpdate 验证无变更短路与局部更新机制：
// 1. 无变更时直接短路返回，不开启写事务、不变更自增 ID；
// 2. 有局部变更时，仅针对变更 key 开启事务执行局部重写，未变更的 key 保持原样不动。
func TestSaveGroupedPlaylists_NoOpShortCircuitAndPartialUpdate(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	keysMap := map[string]struct{}{
		"k1": {},
		"k2": {},
		"k3": {},
	}
	playlists := []model.SlaveMoviePlaylist{
		{SourceId: "slave_test", MovieKey: "k1", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k1/1"}]`},
		{SourceId: "slave_test", MovieKey: "k2", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k2/1"}]`},
		{SourceId: "slave_test", MovieKey: "k3", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k3/1"}]`},
	}

	// 1. 首次全量写入
	changes, err := saveGroupedPlaylists("slave_test", playlists, keysMap)
	if err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes on first insert, got %d", len(changes))
	}

	var rowsBefore []model.SlaveMoviePlaylist
	if err := gdb.Where("source_id = ?", "slave_test").Order("movie_key ASC").Find(&rowsBefore).Error; err != nil {
		t.Fatalf("query rows before: %v", err)
	}
	if len(rowsBefore) != 3 {
		t.Fatalf("expected 3 rows in db, got %d", len(rowsBefore))
	}
	id1 := rowsBefore[0].ID
	id2 := rowsBefore[1].ID
	id3 := rowsBefore[2].ID

	// 2. 无变更再次调用 -> 无变更短路 (No-op short-circuit)
	changes2, err := saveGroupedPlaylists("slave_test", playlists, keysMap)
	if err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	if len(changes2) != 0 {
		t.Fatalf("expected 0 changes on no-op save, got %d", len(changes2))
	}

	var rowsAfterNoop []model.SlaveMoviePlaylist
	if err := gdb.Where("source_id = ?", "slave_test").Order("movie_key ASC").Find(&rowsAfterNoop).Error; err != nil {
		t.Fatalf("query rows after noop: %v", err)
	}
	if rowsAfterNoop[0].ID != id1 || rowsAfterNoop[1].ID != id2 || rowsAfterNoop[2].ID != id3 {
		t.Fatalf("expected row IDs [%d, %d, %d] to remain completely untouched, got [%d, %d, %d]",
			id1, id2, id3, rowsAfterNoop[0].ID, rowsAfterNoop[1].ID, rowsAfterNoop[2].ID)
	}

	// 3. 仅修改 k2 的集数 -> 局部更新 (Partial update)
	playlistsWithChange := []model.SlaveMoviePlaylist{
		{SourceId: "slave_test", MovieKey: "k1", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k1/1"}]`},
		{SourceId: "slave_test", MovieKey: "k2", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k2/1"},{"episode":"02","link":"http://k2/2"}]`},
		{SourceId: "slave_test", MovieKey: "k3", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k3/1"}]`},
	}
	changes3, err := saveGroupedPlaylists("slave_test", playlistsWithChange, keysMap)
	if err != nil {
		t.Fatalf("partial save failed: %v", err)
	}
	if len(changes3) != 1 || changes3[0].MovieKey != "k2" {
		t.Fatalf("expected exactly 1 change for k2, got %+v", changes3)
	}

	var rowsAfterPartial []model.SlaveMoviePlaylist
	if err := gdb.Where("source_id = ?", "slave_test").Order("movie_key ASC").Find(&rowsAfterPartial).Error; err != nil {
		t.Fatalf("query rows after partial: %v", err)
	}
	// k1 / k3 不得动；k2 原地 upsert，ID 保持不变。
	if rowsAfterPartial[0].ID != id1 {
		t.Fatalf("expected k1 ID to remain %d, got %d", id1, rowsAfterPartial[0].ID)
	}
	if rowsAfterPartial[1].ID != id2 {
		t.Fatalf("expected k2 ID to remain %d after in-place upsert, got %d", id2, rowsAfterPartial[1].ID)
	}
	if rowsAfterPartial[2].ID != id3 {
		t.Fatalf("expected k3 ID to remain %d, got %d", id3, rowsAfterPartial[2].ID)
	}

	// 4. 减少线路时只删消失的 group，保留仍在的行
	playlistsDropGroup := []model.SlaveMoviePlaylist{
		{SourceId: "slave_test", MovieKey: "k1", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k1/1"}]`},
		{SourceId: "slave_test", MovieKey: "k2", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k2/1"},{"episode":"02","link":"http://k2/2"}]`},
		{SourceId: "slave_test", MovieKey: "k2", GroupIndex: 1, GroupName: "备用", Content: `[{"episode":"01","link":"http://k2b/1"}]`},
		{SourceId: "slave_test", MovieKey: "k3", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k3/1"}]`},
	}
	if _, err := saveGroupedPlaylists("slave_test", playlistsDropGroup, keysMap); err != nil {
		t.Fatalf("add group save failed: %v", err)
	}
	playlistsDropGroup = []model.SlaveMoviePlaylist{
		{SourceId: "slave_test", MovieKey: "k1", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k1/1"}]`},
		{SourceId: "slave_test", MovieKey: "k2", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k2/1"},{"episode":"02","link":"http://k2/2"}]`},
		{SourceId: "slave_test", MovieKey: "k3", GroupIndex: 0, GroupName: "默认", Content: `[{"episode":"01","link":"http://k3/1"}]`},
	}
	if _, err := saveGroupedPlaylists("slave_test", playlistsDropGroup, keysMap); err != nil {
		t.Fatalf("drop group save failed: %v", err)
	}
	var rowsAfterDrop []model.SlaveMoviePlaylist
	if err := gdb.Where("source_id = ?", "slave_test").Order("movie_key ASC, group_index ASC").Find(&rowsAfterDrop).Error; err != nil {
		t.Fatalf("query rows after drop: %v", err)
	}
	if len(rowsAfterDrop) != 3 {
		t.Fatalf("expected vanished group deleted, got %d rows", len(rowsAfterDrop))
	}
	if rowsAfterDrop[1].MovieKey != "k2" || rowsAfterDrop[1].GroupIndex != 0 {
		t.Fatalf("expected k2 group 0 kept, got %+v", rowsAfterDrop[1])
	}
}
