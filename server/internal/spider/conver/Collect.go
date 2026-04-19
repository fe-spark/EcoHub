package conver

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

// GenCategoryTree 将采集站分类列表直接构建为两层树形结构
// 第一层（pid=0）直接作为顶级大类，第二层作为对应大类的子类。
// 忽略资讯/明星等噪音分类。
func GenCategoryTree(list []model.FilmClass) *model.CategoryTree {
	root := &model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息", Show: true,
		Children: make([]*model.CategoryTree, 0),
	}
	nodes := make(map[int64]*model.CategoryTree)
	nodes[0] = root

	// 噪音分类过滤词
	noiseWords := []string{"资讯", "明星", "新闻", "解说", "站长", "教程"}

	// 第一遍：初始化所有节点
	for _, c := range list {
		lowName := strings.ToLower(c.Name)
		show := !utils.ContainsAny(lowName, noiseWords)
		nodes[c.ID] = &model.CategoryTree{
			Id: c.ID, Pid: c.Pid, Name: c.Name, Show: show,
			Children: make([]*model.CategoryTree, 0),
		}
	}

	// 第二遍：建立层级关系（严格按采集站原始 pid 层次）
	for _, c := range list {
		node := nodes[c.ID]
		if !node.Show {
			continue
		}
		parent, ok := nodes[c.Pid]
		if !ok {
			parent = root
		}
		parent.Children = append(parent.Children, node)
	}

	return root
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
		Id:       detail.VodID,
		Cid:      detail.TypeID,
		Pid:      detail.TypeID1,
		Name:     detail.VodName,
		Picture:  detail.VodPic,
		DownFrom: detail.VodDownFrom,
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

// GenAllFilmPlayList 处理影片播放地址数据, 保留全部播放链接,生成playList
func GenAllFilmPlayList(playUrl, separator string) [][]model.MovieUrlInfo {
	var res [][]model.MovieUrlInfo
	if separator != "" {
		// 1. 通过分隔符切分播放源地址
		for l := range strings.SplitSeq(playUrl, separator) {
			if pl := ConvertPlayUrl(l); len(pl) > 0 {
				res = append(res, pl)
			}
		}
		return res
	}
	if pl := ConvertPlayUrl(playUrl); len(pl) > 0 {
		res = append(res, pl)
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

// ConvertVirtualPicture 将影片详情信息转化为虚拟图片信息
func ConvertVirtualPicture(details []model.MovieDetail) []model.VirtualPicture {
	var l []model.VirtualPicture
	for _, d := range details {
		if len(d.Picture) > 0 {
			l = append(l, model.VirtualPicture{Id: d.Id, Link: d.Picture})
		}
	}
	return l
}
