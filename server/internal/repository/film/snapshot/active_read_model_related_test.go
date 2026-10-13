package snapshot

import (
	"testing"

	"server/internal/model"
	"server/internal/model/dto"
)

func TestRelatedSnapshotCandidates_MultiStageFunnel(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)

	// 插入首选站源
	srcID := "1208629981"
	gdb.Create(&model.FilmSource{Id: srcID, Name: "首选站", State: true})

	// 插入目标影片：仙逆 (mid=441, pid=0, cid=0, 只有 root_category_key="dongman")
	currentFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 441, FirstSourceId: srcID},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid:             0,
			Cid:             0,
			RootCategoryKey: "dongman",
			CategoryKey:     "dongman:guoman",
			CName:           "国漫",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "仙逆",
			Hits: 100,
		},
	}
	if err := gdb.Create(&currentFilm).Error; err != nil {
		t.Fatalf("create current film: %v", err)
	}
	// 为仙逆添加首选站播放线路
	gdb.Create(&model.FilmSourcePlaylist{
		Mid: 441, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h1",
	})

	// 场景 1：同站其他动漫（也是 pid=0, 但 root_category_key="dongman"），测试大类键匹配
	peerAnime := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 442, FirstSourceId: srcID},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid:             0,
			Cid:             0,
			RootCategoryKey: "dongman",
			CategoryKey:     "dongman:xuanhuan",
			CName:           "玄幻",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "完美世界",
			Hits: 90,
		},
	}
	gdb.Create(&peerAnime)
	gdb.Create(&model.FilmSourcePlaylist{
		Mid: 442, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h2",
	})

	// 场景 2：同站热播电影（分类无关，用于测试热播兜底）
	peerMovie := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 443, FirstSourceId: srcID},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid: 1, Cid: 11, RootCategoryKey: "dianying", CategoryKey: "dianying:dongzuo", CName: "动作片",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "流浪地球",
			Hits: 80,
		},
	}
	gdb.Create(&peerMovie)
	gdb.Create(&model.FilmSourcePlaylist{
		Mid: 443, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h3",
	})

	// 场景 3：其他站的影片（在首选站无线路，不应被召回）
	otherSrcMovie := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 444, FirstSourceId: "other_src"},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid:             0,
			Cid:             0,
			RootCategoryKey: "dongman",
			CategoryKey:     "dongman:guoman",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "斗破苍穹",
			Hits: 200,
		},
	}
	gdb.Create(&otherSrcMovie)
	gdb.Create(&model.FilmSourcePlaylist{
		Mid: 444, SourceId: "other_src", LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h4",
	})

	// 测试 1：仙逆在首选站下召回
	snap := buildFilmListSnapshot("live", currentFilm)
	page := dto.Page{Current: 0, PageSize: 14}
	res := ListRelatedSnapshotsReadModel("live", snap, &page, srcID)

	if len(res) == 0 {
		t.Fatalf("expected non-empty related films for 仙逆, got 0")
	}

	// 验证召回结果必须包含同类动漫 442 和兜底热播片 443
	mids := make(map[int64]bool)
	for _, r := range res {
		mids[r.Mid] = true
		if r.Mid == 444 {
			t.Fatalf("film 444 from other source must not be returned in source %s", srcID)
		}
		if r.Mid == 441 {
			t.Fatalf("current film 441 itself must be excluded from related results")
		}
	}

	if !mids[442] {
		t.Errorf("expected peer anime 442 to be recalled via root_category_key")
	}
	if mids[443] {
		t.Errorf("peer movie 443 must be strictly excluded from anime 441 recommendations")
	}
}

func TestRelatedSnapshotCandidates_IsolatedNoCategoryFallback(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)

	srcID := "1208629981"
	gdb.Create(&model.FilmSource{Id: srcID, Name: "首选站", State: true})

	// 孤立影片（无任何分类信息）
	currentFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 501, FirstSourceId: srcID},
		FilmIndexContent:  model.FilmIndexContent{Name: "孤立片", Hits: 10},
	}
	gdb.Create(&currentFilm)
	gdb.Create(&model.FilmSourcePlaylist{
		Mid: 501, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h501",
	})

	// 首选站热播片
	hotFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 502, FirstSourceId: srcID},
		FilmIndexContent:  model.FilmIndexContent{Name: "首选热片", Hits: 100},
	}
	gdb.Create(&hotFilm)
	gdb.Create(&model.FilmSourcePlaylist{
		Mid: 502, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h502",
	})

	snap := buildFilmListSnapshot("live", currentFilm)
	page := dto.Page{Current: 0, PageSize: 14}
	res := ListRelatedSnapshotsReadModel("live", snap, &page, srcID)

	if len(res) == 0 {
		t.Fatalf("expected popularity fallback to provide recommendations for isolated film, got 0")
	}
	if res[0].Mid != 502 {
		t.Fatalf("expected hotFilm 502 as first recommendation, got mid=%d", res[0].Mid)
	}
}

func TestRelatedSnapshotCandidates_GenreAndRegionRelevance(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)

	srcID := "1208629981"
	gdb.Create(&model.FilmSource{Id: srcID, Name: "首选站", State: true})

	// 当前片：仙逆 (国漫修真古装)
	currentFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 441, FirstSourceId: srcID},
		FilmIndexCategory: model.FilmIndexCategory{CName: "动漫", RootCategoryKey: "dongman"},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙逆", Area: "中国大陆", ClassTag: "剧情,动画,古装", Hits: 50},
	}
	gdb.Create(&currentFilm)
	gdb.Create(&model.FilmSourcePlaylist{Mid: 441, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h441"})

	// 候选 1：凡人修仙传 (同为大陆古装/玄幻动漫，Hits=60)
	peerXianxia := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1001, FirstSourceId: srcID},
		FilmIndexCategory: model.FilmIndexCategory{CName: "国产动漫", RootCategoryKey: "dongman"},
		FilmIndexContent:  model.FilmIndexContent{Name: "凡人修仙传", Area: "中国大陆", ClassTag: "古装,玄幻,动画", Hits: 60},
	}
	gdb.Create(&peerXianxia)
	gdb.Create(&model.FilmSourcePlaylist{Mid: 1001, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h1001"})

	// 候选 2：超级小恐龙 (欧美少儿动画，虽然 Hits 极高=500，但题材和地区不匹配)
	dinosaurKids := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1002, FirstSourceId: srcID},
		FilmIndexCategory: model.FilmIndexCategory{CName: "欧美动漫", RootCategoryKey: "dongman"},
		FilmIndexContent:  model.FilmIndexContent{Name: "超级小恐龙", Area: "欧美", ClassTag: "少儿,益智", Hits: 500},
	}
	gdb.Create(&dinosaurKids)
	gdb.Create(&model.FilmSourcePlaylist{Mid: 1002, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h1002"})

	// 候选 3：战狼 (电影，Hits=999，大类完全无关)
	actionMovie := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1003, FirstSourceId: srcID},
		FilmIndexCategory: model.FilmIndexCategory{CName: "动作片", RootCategoryKey: "dianying", Pid: 1},
		FilmIndexContent:  model.FilmIndexContent{Name: "战狼", Area: "中国大陆", ClassTag: "动作,战争", Hits: 999},
	}
	gdb.Create(&actionMovie)
	gdb.Create(&model.FilmSourcePlaylist{Mid: 1003, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h1003"})

	snap := buildFilmListSnapshot("live", currentFilm)
	page := dto.Page{Current: 0, PageSize: 14}
	res := ListRelatedSnapshotsReadModel("live", snap, &page, srcID)

	if len(res) == 0 {
		t.Fatalf("expected recommendations, got 0")
	}

	// 关键断言：同题材同地区的国漫《凡人修仙传》(1001) 必须排在首位！
	if res[0].Mid != 1001 {
		t.Fatalf("expected top recommendation to be Xianxia anime 1001 (凡人修仙传), got mid=%d name=%q", res[0].Mid, res[0].Name)
	}

	// 动作电影《战狼》(1003) 绝对不能出现在动漫相关推荐中！
	for _, item := range res {
		if item.Mid == 1003 {
			t.Fatalf("movie 1003 (战狼) must never be recommended for anime 441")
		}
	}
}

func TestRelatedSnapshotCandidates_SameFranchiseTheatricalVersions(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)

	srcID := "1208629981"
	gdb.Create(&model.FilmSource{Id: srcID, Name: "首选站", State: true})

	// 当前片：仙逆 (TV 动漫，SeriesKey 随意)
	currentFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 441, FirstSourceId: srcID},
		FilmIndexCategory: model.FilmIndexCategory{CName: "动漫", RootCategoryKey: "dongman", CategoryKey: "dongman:tv"},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙逆", Area: "中国大陆", Hits: 50, SeriesKey: "series_main"},
	}
	gdb.Create(&currentFilm)
	gdb.Create(&model.FilmSourcePlaylist{Mid: 441, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h441"})

	// 候选 1：仙逆剧场版 弑仙之战 (不同 series_key，不同 category_key，Hits=0，但在当前源可播)
	theatrical1 := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 440, FirstSourceId: srcID},
		FilmIndexCategory: model.FilmIndexCategory{CName: "动漫电影", RootCategoryKey: "dianying", CategoryKey: "dianying:dongman"},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙逆剧场版 弑仙之战", Area: "中国大陆", Hits: 0, SeriesKey: "series_movie_1"},
	}
	gdb.Create(&theatrical1)
	gdb.Create(&model.FilmSourcePlaylist{Mid: 440, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h440"})

	// 候选 2：仙逆剧场版神临之战 (又一不同 series_key，Hits=100)
	theatrical2 := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 38861, FirstSourceId: srcID},
		FilmIndexCategory: model.FilmIndexCategory{CName: "动画片", RootCategoryKey: "dianying"},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙逆剧场版神临之战", Area: "中国大陆", Hits: 100, SeriesKey: "series_movie_2"},
	}
	gdb.Create(&theatrical2)
	gdb.Create(&model.FilmSourcePlaylist{Mid: 38861, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h38861"})

	// 候选 3：同分类但完全不同作品的爆款动漫（Hits=99999）
	popularPeer := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 555, FirstSourceId: srcID},
		FilmIndexCategory: model.FilmIndexCategory{CName: "动漫", RootCategoryKey: "dongman", CategoryKey: "dongman:tv"},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙武传", Area: "中国大陆", Hits: 99999},
	}
	gdb.Create(&popularPeer)
	gdb.Create(&model.FilmSourcePlaylist{Mid: 555, SourceId: srcID, LineKind: "play", GroupIndex: 0, GroupName: "默认", EpisodeCount: 1, ContentHash: "h555"})

	snap := buildFilmListSnapshot("live", currentFilm)
	page := dto.Page{Current: 0, PageSize: 14}
	res := ListRelatedSnapshotsReadModel("live", snap, &page, srcID)

	if len(res) < 2 {
		t.Fatalf("expected at least 2 recommendations, got %d", len(res))
	}

	// 关键断言：两部仙逆剧场版必须被核心片名索引召回，并在打分精排中占据前两位！
	top2Mids := map[int64]bool{res[0].Mid: true, res[1].Mid: true}
	if !top2Mids[440] || !top2Mids[38861] {
		t.Fatalf("expected both theatrical versions (440, 38861) to occupy top 2 positions, got [0]=%d, [1]=%d", res[0].Mid, res[1].Mid)
	}
}
