package writer

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/repository/film/shared"
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
		PlayFrom:        []string{"线路1"},
		PlayList:        [][]model.MovieUrlInfo{{{Episode: "01", Link: "http://x/1"}}},
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
	if _, _, _, err := applyMasterBusinessUpdateStampsTx(gdb, infos, map[string]model.MovieDetail{"vod_200": remarksOnly}, false); err != nil {
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
	if _, _, _, err := applyMasterBusinessUpdateStampsTx(gdb, infos2, map[string]model.MovieDetail{"vod_200": moreEps}, false); err != nil {
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

// TestMasterLastEpisodeChanged 主站「任一线路最后一集变化才通知」语义（回退也算）。
func TestMasterLastEpisodeChanged(t *testing.T) {
	detail := func(eps int) model.MovieDetail {
		links := make([]model.MovieUrlInfo, 0, eps)
		for i := 1; i <= eps; i++ {
			links = append(links, model.MovieUrlInfo{Episode: fmt.Sprintf("第%02d集", i), Link: "http://a/1.m3u8"})
		}
		return model.MovieDetail{PlayFrom: []string{"m3u8"}, PlayList: [][]model.MovieUrlInfo{links}}
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

func TestPosterPreservationInMasterWrite(t *testing.T) {
	// 验证 masterBusinessSignature 在海报一致时正确识别业务一致
	detailA := model.MovieDetail{
		Id:       101,
		Name:     "测试电影",
		Picture:  "https://high-quality.cdn/poster.jpg",
		PlayFrom: []string{"test_m3u8"},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "01", Link: "http://test/1.m3u8"}},
		},
		MovieDescriptor: model.MovieDescriptor{
			Remarks: "HD",
			State:   "正片",
		},
	}

	detailB := detailA
	detailB.Picture = "https://high-quality.cdn/poster.jpg?timestamp=12345"

	if !sameStoredMasterDetail(detailA, detailB) {
		t.Fatal("忽略 URL query 参数后，相同海报应被判定为业务无变化")
	}
}

func TestCustomPictureStateChangeTriggersUpdate(t *testing.T) {
	detailA := model.MovieDetail{
		Name:            "完美世界",
		Picture:         "https://wrong.cdn/wrong.jpg",
		IsCustomPicture: true,
	}
	detailB := detailA
	detailB.IsCustomPicture = false

	if sameStoredMasterDetail(detailA, detailB) {
		t.Fatal("IsCustomPicture 状态从 true 切换为 false 应被判定为实质变更")
	}
}

func TestBuildMovieDetailInfosPreservesPosterFromInfo(t *testing.T) {
	detail := model.MovieDetail{
		Id:      202,
		Name:    "测试海报同步",
		Picture: "https://low-quality.cdn/master_low.jpg",
	}
	contentKey := shared.BuildContentKey(detail)
	info := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:        202,
			ContentKey: contentKey,
		},
		FilmIndexContent: model.FilmIndexContent{
			Name:         detail.Name,
			Picture:      "https://high-quality.cdn/poster_hd.jpg",
			PictureSlide: "https://high-quality.cdn/slide_hd.jpg",
		},
	}
	infoByKey := map[string]model.FilmIndex{
		contentKey: info,
	}
	keyToMid := map[string]int64{
		contentKey: 202,
	}

	detailInfos := buildMovieDetailInfos("src_1", []model.MovieDetail{detail}, infoByKey, keyToMid)
	if len(detailInfos) != 1 {
		t.Fatalf("buildMovieDetailInfos 返回数量错误: %d, 期望 1", len(detailInfos))
	}

	var parsed model.MovieDetail
	if err := json.Unmarshal([]byte(detailInfos[0].Content), &parsed); err != nil {
		t.Fatalf("解析 detail info content 失败: %v", err)
	}

	if parsed.Picture != info.Picture {
		t.Fatalf("Picture 未从 info 同步保留: got %q, want %q", parsed.Picture, info.Picture)
	}
	if parsed.PictureSlide != info.PictureSlide {
		t.Fatalf("PictureSlide 未从 info 同步保留: got %q, want %q", parsed.PictureSlide, info.PictureSlide)
	}
}
