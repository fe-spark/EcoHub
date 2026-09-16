package playlist

import (
	"testing"

	"server/internal/model"
)

func TestScoreSlaveCandidate_UniversalScenarios(t *testing.T) {
	// 场景 1：同名《仙逆》消歧（动漫 vs 短剧）
	// 主站影片 A：动漫《仙逆》，配音陈奕雯、史泽鲲，2023年，分类“国产动漫”
	masterAnime := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101},
		FilmIndexCategory: model.FilmIndexCategory{CName: "国产动漫"},
		FilmIndexContent: model.FilmIndexContent{
			Name:     "仙逆",
			Actor:    "陈奕雯, 史泽鲲",
			Director: "石头",
			Year:     2023,
			Remarks:  "更新至85集",
		},
	}

	// 主站影片 B：短剧《仙逆》，演员张三、李四，2024年，分类“古装短剧”
	masterShort := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 102},
		FilmIndexCategory: model.FilmIndexCategory{CName: "古装短剧"},
		FilmIndexContent: model.FilmIndexContent{
			Name:     "仙逆",
			Actor:    "张三, 李四",
			Director: "王导",
			Year:     2024,
			Remarks:  "全80集",
		},
	}

	infoMap := map[int64]model.FilmIndex{
		101: masterAnime,
		102: masterShort,
	}

	// 附属站采集来一部《仙逆》短剧，分类仅叫“爽文短剧”（未配置根大类），演员张三、王五，年份 2024
	slaveShort := model.MovieDetail{
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			Actor: "张三, 王五",
			Year:  "2024",
			CName: "爽文短剧",
		},
		PlayList: make([][]model.MovieUrlInfo, 1),
	}
	slaveShort.PlayList[0] = make([]model.MovieUrlInfo, 80)

	scoreToAnime := ScoreSlaveCandidate(slaveShort, masterAnime)
	scoreToShort := ScoreSlaveCandidate(slaveShort, masterShort)

	if scoreToShort <= scoreToAnime {
		t.Fatalf("短剧附属站应该归属于短剧主站！scoreToShort=%d, scoreToAnime=%d", scoreToShort, scoreToAnime)
	}

	pickedMid := pickBestMidByScore(slaveShort, []int64{101, 102}, infoMap)
	if pickedMid != 102 {
		t.Fatalf("消歧器应选出短剧 mid 102，实际返回 %d", pickedMid)
	}

	// 场景 2：附属站采集来一部《仙逆》动漫，分类叫“动漫番剧”，配音“陈奕雯”，年份 2023
	slaveAnime := model.MovieDetail{
		Name: "仙逆",
		MovieDescriptor: model.MovieDescriptor{
			Actor: "陈奕雯, 某声优",
			Year:  "2023",
			CName: "动漫番剧",
		},
		PlayList: make([][]model.MovieUrlInfo, 1),
	}
	slaveAnime.PlayList[0] = make([]model.MovieUrlInfo, 85)

	pickedAnimeMid := pickBestMidByScore(slaveAnime, []int64{101, 102}, infoMap)
	if pickedAnimeMid != 101 {
		t.Fatalf("消歧器应选出动漫 mid 101，实际返回 %d", pickedAnimeMid)
	}
}

func TestScoreSlaveCandidate_AmbiguityIsolation(t *testing.T) {
	// 场景：两部主站影片完全没有足够信息（如全为空），且分差极小
	masterA := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 201},
		FilmIndexCategory: model.FilmIndexCategory{CName: "未知1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "神秘同名"},
	}
	masterB := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 202},
		FilmIndexCategory: model.FilmIndexCategory{CName: "未知2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "神秘同名"},
	}

	slave := model.MovieDetail{
		Name: "神秘同名",
		MovieDescriptor: model.MovieDescriptor{
			CName: "未知3",
		},
	}

	infoMap := map[int64]model.FilmIndex{
		201: masterA,
		202: masterB,
	}

	// 低置信度且无分差，绝不强行认主
	picked := pickBestMidByScore(slave, []int64{201, 202}, infoMap)
	if picked != 0 {
		t.Fatalf("信息不足时应返回 0 进行隔离防串，实际错误认主: %d", picked)
	}
}

func TestComputeTextSimilarity(t *testing.T) {
	// 完全相同
	if sim := computeTextSimilarity("动作片", "动作片"); sim != 1.0 {
		t.Fatalf("同词相似度应为 1.0，实际为 %f", sim)
	}
	// 部分重合
	if sim := computeTextSimilarity("都市短剧", "爽文短剧"); sim <= 0 {
		t.Fatalf("短剧两词有交集，相似度应大于 0，实际为 %f", sim)
	}
	// 完全无关
	if sim := computeTextSimilarity("动作片", "动漫"); sim != 0 {
		t.Fatalf("无关分类相似度应为 0，实际为 %f", sim)
	}
}

func TestScoreSlaveCandidate_RemakeMovieYears(t *testing.T) {
	// 场景 3：同名电影翻拍消歧（2003版 vs 2023版）
	masterOld := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 301},
		FilmIndexCategory: model.FilmIndexCategory{CName: "恐怖片"},
		FilmIndexContent: model.FilmIndexContent{
			Name:     "咒怨",
			Actor:    "奥菜惠, 伊东美咲",
			Director: "清水崇",
			Year:     2003,
			Remarks:  "HD",
		},
	}
	masterNew := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 302},
		FilmIndexCategory: model.FilmIndexCategory{CName: "恐怖片"},
		FilmIndexContent: model.FilmIndexContent{
			Name:     "咒怨",
			Actor:    "佐佐木希, 青柳翔",
			Director: "落合正幸",
			Year:     2014,
			Remarks:  "1080P",
		},
	}

	infoMap := map[int64]model.FilmIndex{
		301: masterOld,
		302: masterNew,
	}

	slave2014 := model.MovieDetail{
		Name: "咒怨",
		MovieDescriptor: model.MovieDescriptor{
			Actor:    "佐佐木希",
			Director: "落合正幸",
			Year:     "2014",
			CName:    "恐怖片",
		},
		PlayList: make([][]model.MovieUrlInfo, 1),
	}
	slave2014.PlayList[0] = make([]model.MovieUrlInfo, 1)

	pickedMid := pickBestMidByScore(slave2014, []int64{301, 302}, infoMap)
	if pickedMid != 302 {
		t.Fatalf("应精准匹配到 2014 版电影 mid 302，实际返回 %d", pickedMid)
	}
}

func TestScoreSlaveCandidate_TVSeriesVsMovie(t *testing.T) {
	// 场景 4：同名电视剧 vs 电影消歧（形态差异与演职员）
	masterTV := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 401},
		FilmIndexCategory: model.FilmIndexCategory{CName: "国产剧"},
		FilmIndexContent: model.FilmIndexContent{
			Name:     "三体",
			Actor:    "张鲁一, 于和伟, 陈瑾",
			Director: "杨磊",
			Year:     2023,
			Remarks:  "全30集",
		},
	}
	masterMovie := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 402},
		FilmIndexCategory: model.FilmIndexCategory{CName: "科幻片"},
		FilmIndexContent: model.FilmIndexContent{
			Name:     "三体",
			Actor:    "冯绍峰, 张静初",
			Director: "张番番",
			Year:     2018,
			Remarks:  "HD",
		},
	}

	infoMap := map[int64]model.FilmIndex{
		401: masterTV,
		402: masterMovie,
	}

	slaveTV := model.MovieDetail{
		Name: "三体",
		MovieDescriptor: model.MovieDescriptor{
			Actor: "于和伟, 张鲁一",
			Year:  "2023",
			CName: "电视剧",
		},
		PlayList: make([][]model.MovieUrlInfo, 1),
	}
	slaveTV.PlayList[0] = make([]model.MovieUrlInfo, 30)

	pickedMid := pickBestMidByScore(slaveTV, []int64{401, 402}, infoMap)
	if pickedMid != 401 {
		t.Fatalf("应精准匹配到电视剧版三体 mid 401，实际返回 %d", pickedMid)
	}
}

func TestCountMasterEpisodes_ResolutionsAreNotEpisodes(t *testing.T) {
	// 验证 "1080P"、"4K" 等电影分辨率不会被误作为 1080 集
	if ep := countMasterEpisodes(model.FilmIndex{FilmIndexContent: model.FilmIndexContent{Remarks: "1080P"}}); ep != 1 {
		t.Fatalf("1080P 电影形态集数应为 1，实际返回 %d", ep)
	}
	if ep := countMasterEpisodes(model.FilmIndex{FilmIndexContent: model.FilmIndexContent{Remarks: "4K蓝光原盘"}}); ep != 1 {
		t.Fatalf("4K 电影形态集数应为 1，实际返回 %d", ep)
	}
	if ep := countMasterEpisodes(model.FilmIndex{FilmIndexContent: model.FilmIndexContent{Remarks: "更新至80集"}}); ep != 80 {
		t.Fatalf("更新至80集 集数应为 80，实际返回 %d", ep)
	}
	if ep := countMasterEpisodes(model.FilmIndex{FilmIndexContent: model.FilmIndexContent{Remarks: "全12期"}}); ep != 12 {
		t.Fatalf("全12期 集数应为 12，实际返回 %d", ep)
	}
}

func TestScoreSlaveCandidate_SingleCandidateNegativeScoreIsolation(t *testing.T) {
	// 场景：库里虽然只有 1 部同名影片（例如电影），但附属站是一部 80 集短剧，年份也差很多
	masterOnlyMovie := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 501},
		FilmIndexCategory: model.FilmIndexCategory{CName: "动作片"},
		FilmIndexContent: model.FilmIndexContent{
			Name:    "同名片",
			Year:    2015,
			Remarks: "1080P",
		},
	}

	slaveShort := model.MovieDetail{
		Name: "同名片",
		MovieDescriptor: model.MovieDescriptor{
			Year:  "2024",
			CName: "微短剧",
		},
		PlayList: make([][]model.MovieUrlInfo, 1),
	}
	slaveShort.PlayList[0] = make([]model.MovieUrlInfo, 80)

	infoMap := map[int64]model.FilmIndex{
		501: masterOnlyMovie,
	}

	// 此时单候选得分应为严重负分（跨年代 -50，形态互斥 -35，分类异质 -30）
	score := ScoreSlaveCandidate(slaveShort, masterOnlyMovie)
	if score >= -20 {
		t.Fatalf("严重冲突场景得分应低于 -20，实际得分 %d", score)
	}

	// pickBestMidByScore 绝不能把短剧认主到 10 年前的动作电影上
	picked := pickBestMidByScore(slaveShort, []int64{501}, infoMap)
	if picked != 0 {
		t.Fatalf("单候选严重冲突时应返回 0 进行隔离，实际错误认主: %d", picked)
	}
}
