package converter

import (
	"fmt"
	"strings"

	"server/internal/model"
	"server/internal/utils"
)

const macCMSGroupSeparator = "$$$"

/*
	处理 不同结构体数据之间的转化
	统一转化为内部结构体
*/

// GenCategoryTreeWithParentHints 在原始 type_pid 缺失时，允许调用方补充父级推断结果。
// 第一层（pid=0）直接作为顶级大类，第二层作为对应大类的子类。
// 当 parentHints 未覆盖时，自动启用统一语义化规则进行智能兜底。
// 忽略资讯/明星等噪音分类。
func GenCategoryTreeWithParentHints(list []model.FilmClass, parentHints map[int64]int64) *model.CategoryTree {
	root := &model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息", Show: true,
		Children: make([]*model.CategoryTree, 0),
	}
	nodes := make(map[int64]*model.CategoryTree)
	nodes[0] = root

	// 噪音分类过滤词
	noiseWords := []string{"资讯", "明星", "新闻", "解说", "站长", "教程"}

	// 统一语义化兜底推断（当源站未自带 pid 且动态探测未命中时使用）
	semanticHints := InferCategoryParentsBySemantic(list)

	// 第一遍：初始化所有节点
	for _, c := range list {
		pid := c.Pid
		if pid == 0 {
			if hintedPid, ok := parentHints[c.ID]; ok && hintedPid != c.ID {
				pid = hintedPid
			} else if hintedPid, ok := semanticHints[c.ID]; ok && hintedPid != c.ID {
				pid = hintedPid
			}
		}
		lowName := strings.ToLower(c.Name)
		show := !utils.ContainsAny(lowName, noiseWords)
		nodes[c.ID] = &model.CategoryTree{
			Id: c.ID, Pid: pid, Name: c.Name, Show: show,
			Children: make([]*model.CategoryTree, 0),
		}
	}

	// 第二遍：建立层级关系（严格按采集站原始 pid 层次）
	for _, c := range list {
		node := nodes[c.ID]
		if !node.Show {
			continue
		}
		parent, ok := nodes[node.Pid]
		if !ok {
			parent = root
		}
		parent.Children = append(parent.Children, node)
	}

	return root
}

// InferCategoryParentsBySemantic 当采集源（无论是 JSON 还是 XML）未提供分类层级或详情缺少父级字段时，
// 依据影视行业常见分类命名规范自动推断主类与子类关系，保证分类树结构的完整性。
func InferCategoryParentsBySemantic(classes []model.FilmClass) map[int64]int64 {
	if len(classes) == 0 {
		return nil
	}

	var (
		rootMovieID   int64
		rootTvID      int64
		rootAnimeID   int64
		rootVarietyID int64
		rootDocID     int64
		rootShortID   int64
		rootSportsID  int64
	)

	// 第一遍：识别预设主类 ID
	for _, c := range classes {
		name := strings.TrimSpace(c.Name)
		switch name {
		case "电影", "电影片", "电影区":
			if rootMovieID == 0 {
				rootMovieID = c.ID
			}
		case "电视剧", "连续剧", "剧集", "电视剧集":
			if rootTvID == 0 {
				rootTvID = c.ID
			}
		case "动漫", "动画":
			if rootAnimeID == 0 {
				rootAnimeID = c.ID
			}
		case "综艺", "综艺娱乐", "综艺节目":
			if rootVarietyID == 0 {
				rootVarietyID = c.ID
			}
		case "纪录片", "记录片":
			if rootDocID == 0 {
				rootDocID = c.ID
			}
		case "短剧", "微短剧":
			if rootShortID == 0 {
				rootShortID = c.ID
			}
		case "体育赛事", "体育":
			if rootSportsID == 0 {
				rootSportsID = c.ID
			}
		}
	}

	// 若未匹配到任何主类，不强行推断
	if rootMovieID == 0 && rootTvID == 0 && rootAnimeID == 0 &&
		rootVarietyID == 0 && rootDocID == 0 && rootShortID == 0 && rootSportsID == 0 {
		return nil
	}

	isRootID := func(id int64) bool {
		return id > 0 && (id == rootMovieID || id == rootTvID || id == rootAnimeID ||
			id == rootVarietyID || id == rootDocID || id == rootShortID || id == rootSportsID)
	}

	hints := make(map[int64]int64)

	// 常见短剧题材特征词
	shortGenres := []string{
		"仙侠", "都市", "年代", "总裁", "民国", "反转", "爽剧", "悬疑", "脑洞",
		"战神", "赘婿", "神豪", "甜宠", "虐恋", "逆袭", "恋爱", "言情", "短剧",
	}

	// 常见球类/赛事
	sportsKeywords := []string{"足球", "篮球", "台球", "网球", "排球", "羽毛球", "乒乓球", "其他赛事"}

	// 第二遍：基于语义归属子类
	for _, c := range classes {
		if isRootID(c.ID) || c.Pid > 0 {
			continue
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}

		// 1. 包含“电影”，优先归入电影大类（如“动漫电影”、“微电影”）
		if rootMovieID > 0 && strings.Contains(name, "电影") {
			hints[c.ID] = rootMovieID
			continue
		}

		// 2. 纪录片判定
		if rootDocID > 0 && (strings.Contains(name, "纪录") || strings.Contains(name, "记录")) {
			hints[c.ID] = rootDocID
			continue
		}

		// 3. 体育赛事判定
		if rootSportsID > 0 && (strings.Contains(name, "赛事") || strings.Contains(name, "体育") || utils.ContainsAny(name, sportsKeywords)) {
			hints[c.ID] = rootSportsID
			continue
		}

		// 4. 动漫判定
		if rootAnimeID > 0 && (strings.Contains(name, "动漫") || strings.Contains(name, "动画") || strings.Contains(name, "二次元") || strings.Contains(name, "漫剧") || name == "剧场版") {
			hints[c.ID] = rootAnimeID
			continue
		}

		// 5. 综艺判定
		if rootVarietyID > 0 && (strings.Contains(name, "综艺") || strings.Contains(name, "真人秀") || strings.Contains(name, "脱口秀") || strings.Contains(name, "相声小品")) {
			hints[c.ID] = rootVarietyID
			continue
		}

		// 6. 明确短剧判定（含“短剧”、“微短”、以“爽剧”结尾）
		if rootShortID > 0 && (strings.Contains(name, "短剧") || strings.Contains(name, "微短") || strings.HasSuffix(name, "爽剧")) {
			hints[c.ID] = rootShortID
			continue
		}

		// 7. 电影判定（以“片”结尾，如动作片、喜剧片、悬疑片等，或含“影院”）
		if rootMovieID > 0 && (strings.HasSuffix(name, "片") || strings.Contains(name, "影院")) {
			hints[c.ID] = rootMovieID
			continue
		}

		// 8. 电视剧判定（以“剧”结尾，如韩剧、美剧、港剧、日剧、大陆剧、都市剧、悬疑剧等，或含“电视剧”、“连续剧”、“剧集”）
		if rootTvID > 0 && (strings.HasSuffix(name, "剧") || strings.Contains(name, "电视剧") || strings.Contains(name, "连续剧") || strings.Contains(name, "剧集")) {
			hints[c.ID] = rootTvID
			continue
		}

		// 9. 短剧题材词兜底（此时已排除“片”与“剧”标准后缀，如“古装仙侠”、“现代都市”、“战神”、“赘婿”等纯短剧题材词）
		if rootShortID > 0 && utils.ContainsAny(name, shortGenres) {
			hints[c.ID] = rootShortID
			continue
		}
	}

	if len(hints) == 0 {
		return nil
	}
	return hints
}

// ConvertCategoryList 将分类树形数据平滑展开为列表，支持深度嵌套
func ConvertCategoryList(tree *model.CategoryTree) []model.Category {
	var list []model.Category
	if tree == nil {
		return list
	}
	// 不保存虚拟根节点 0 本身到列表（通常数据库不需要这个占位符）
	if tree.Id != 0 {
		list = append(list, model.Category{
			Id:        tree.Id,
			Pid:       tree.Pid,
			Name:      tree.Name,
			Alias:     tree.Alias,
			Show:      tree.Show,
			Sort:      tree.Sort,
			CreatedAt: tree.CreatedAt,
			UpdatedAt: tree.UpdatedAt,
		})
	}
	for _, child := range tree.Children {
		list = append(list, ConvertCategoryList(child)...)
	}
	return list
}

// ConvertFilmDetails 批量处理影片详情信息
func ConvertFilmDetails(details []model.FilmDetail) []model.MovieDetail {
	var dl []model.MovieDetail
	for _, d := range details {
		// 跳过片名为空的无效数据，防止数据库出现空记录
		if strings.TrimSpace(d.VodName) == "" {
			continue
		}
		dl = append(dl, ConvertFilmDetail(d))
	}
	return dl
}

// ConvertFilmDetail 将影片详情数据处理转化为 model.MovieDetail
func ConvertFilmDetail(detail model.FilmDetail) model.MovieDetail {
	md := model.MovieDetail{
		Id:           detail.VodID,
		RawCid:       detail.TypeID,
		RawPid:       detail.TypeID1,
		Cid:          detail.TypeID,
		Pid:          detail.TypeID1,
		Name:         detail.VodName,
		Picture:      detail.VodPic,
		PictureSlide: detail.VodPicSlide,
		DownFrom:     detail.VodDownFrom,
		MovieDescriptor: model.MovieDescriptor{
			SubTitle:    detail.VodSub,
			CName:       detail.TypeName,
			EnName:      detail.VodEn,
			Initial:     detail.VodLetter,
			ClassTag:    detail.VodClass,
			Actor:       detail.VodActor,
			Director:    detail.VodDirector,
			Writer:      detail.VodWriter,
			Blurb:       detail.VodBlurb,
			Remarks:     detail.VodRemarks,
			ReleaseDate: detail.VodPubDate,
			Area:        detail.VodArea,
			Language:    detail.VodLang,
			Year:        detail.VodYear,
			State:       detail.VodState,
			UpdateTime:  detail.VodTime,
			AddTime:     detail.VodTimeAdd,
			DbId:        detail.VodDouBanID,
			DbScore:     detail.VodDouBanScore,
			Hits:        detail.VodHits,
			Content:     detail.VodContent,
		},
	}
	playSeparator := resolvePlayGroupSeparator(detail.VodPlayNote, detail.VodPlayFrom, detail.VodPlayURL)
	downSeparator := resolvePlayGroupSeparator(detail.VodDownNote, detail.VodDownFrom, detail.VodDownURL)
	md.PlayFrom = splitPlaySources(detail.VodPlayFrom, playSeparator)
	// v2 只保留m3u8播放源
	md.PlayList = GenFilmPlayList(detail.VodPlayURL, playSeparator)
	md.DownloadList = GenFilmPlayList(detail.VodDownURL, downSeparator)

	return md
}

func resolvePlayGroupSeparator(note, playFrom, playURL string) string {
	note = strings.TrimSpace(note)
	if note != "" {
		return note
	}
	if strings.Contains(playFrom, macCMSGroupSeparator) || strings.Contains(playURL, macCMSGroupSeparator) {
		return macCMSGroupSeparator
	}
	return ""
}

func splitPlaySources(playFrom, separator string) []string {
	playFrom = strings.TrimSpace(playFrom)
	if playFrom == "" {
		return []string{}
	}
	if separator == "" {
		return []string{playFrom}
	}

	parts := make([]string, 0)
	for item := range strings.SplitSeq(playFrom, separator) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts = append(parts, item)
	}
	if len(parts) == 0 {
		return []string{playFrom}
	}
	return parts
}

// GenFilmPlayList 处理影片播放地址数据, 保留播放链接,生成playList
// 只 append 有效（非空）的播放列表，防止 ConvertPlayUrl("") 产生 nil inner slice → JSON [null]
func GenFilmPlayList(playUrl, separator string) [][]model.MovieUrlInfo {
	var res [][]model.MovieUrlInfo
	if separator != "" {
		// 1. 通过分隔符切分播放源地址
		for l := range strings.SplitSeq(playUrl, separator) {
			// 只保留解析出有效链接的播放源
			if pl := ConvertPlayUrl(l); len(pl) > 0 {
				res = append(res, pl)
			}
		}
	} else {
		if pl := ConvertPlayUrl(playUrl); len(pl) > 0 {
			res = append(res, pl)
		}
	}
	return res
}

// parseEpisode 从单个片段解析集数名和播放链接，支持以下格式：
//
//	"集名$URL"  → episode=集名, link=URL
//	"URL"       → episode="",  link=URL  (无集名，调用方自动补全)
//	"$URL"      → episode="",  link=URL  (部分采集站数据以 $ 开头)
//	"集名$"     → ok=false              (link 缺失，无效)
func parseEpisode(seg string) (episode, link string, ok bool) {
	ep, lk, hasDollar := strings.Cut(seg, "$")
	ep, lk = strings.TrimSpace(ep), strings.TrimSpace(lk)
	switch {
	case !hasDollar:
		return "", ep, ep != "" // 整条是 URL
	case lk != "":
		return ep, lk, true // 正常 "集名$URL"
	case strings.HasPrefix(ep, "http"):
		return "", ep, true // "$URL" 形式，ep 实为 URL
	default:
		return "", "", false // "集名$"，link 为空
	}
}

// isVideoURL 判断是否为视频直链，过滤 share/ 等网页链接
func isVideoURL(link string) bool {
	lower := strings.ToLower(link)
	return strings.Contains(lower, ".m3u8") ||
		strings.Contains(lower, ".mp4") ||
		strings.Contains(lower, ".flv")
}

// ConvertPlayUrl 将单条 playFrom 地址字符串解析为播放列表
// 片段格式：集名$URL，多集以 # 分隔
func ConvertPlayUrl(playUrl string) []model.MovieUrlInfo {
	var result []model.MovieUrlInfo
	for seg := range strings.SplitSeq(playUrl, "#") {
		episode, link, ok := parseEpisode(strings.TrimSpace(seg))
		if !ok || !isVideoURL(link) {
			continue
		}
		if episode == "" {
			episode = fmt.Sprintf("第%d集", len(result)+1)
		}
		result = append(result, model.MovieUrlInfo{Episode: episode, Link: link})
	}
	return result
}
