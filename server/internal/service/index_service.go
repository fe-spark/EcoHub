package service

import (
	"encoding/json"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/utils"
)

type IndexService struct{}

var IndexSvc = new(IndexService)

// IndexPage 首页数据处理
func (i *IndexService) IndexPage() map[string]any {
	// 1. 尝试从 Redis 获取缓存
	cacheKey := repository.GetVersionedIndexPageCacheKey()
	if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
		res := make(map[string]any)
		if json.Unmarshal([]byte(data), &res) == nil {
			return res
		}
	}

	Info := make(map[string]any)
	tree := model.CategoryTree{Id: 0, Name: "分类信息"}
	sysTree := repository.GetActiveCategoryTree()
	for _, c := range sysTree.Children {
		if c.Show {
			tree.Children = append(tree.Children, c)
		}
	}
	Info["category"] = tree
	list := make([]map[string]any, 0)
	for _, c := range tree.Children {
		var movies []model.MovieBasicInfo
		var hotMovies []model.SearchInfo
		if c.Children != nil {
			movies = repository.GetMovieListByPidLimit(c.Id, 14, 0)
			hotMovies = repository.GetHotMovieByPidLimit(c.Id, 14, 0)
		} else {
			movies = repository.GetMovieListByCidLimit(c.Id, 14, 0)
			hotMovies = repository.GetHotMovieByCidLimit(c.Id, 14, 0)
		}
		if movies == nil {
			movies = make([]model.MovieBasicInfo, 0)
		}
		if hotMovies == nil {
			hotMovies = make([]model.SearchInfo, 0)
		}
		item := map[string]any{"nav": c, "movies": movies, "hot": hotMovies}
		list = append(list, item)
	}
	Info["content"] = list
	banners := repository.GetBanners()
	if banners == nil {
		banners = make(model.Banners, 0)
	}
	Info["banners"] = banners

	// 2. 写入 Redis 缓存 (设置长 TTL，但依靠 AfterSave 钩子主动刷新)
	if data, err := json.Marshal(Info); err == nil {
		db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Hour*24)
	}

	return Info
}

// GetFilmDetail 影片详情信息页面处理
func (i *IndexService) GetFilmDetail(id int) model.MovieDetailVo {
	search := repository.GetSearchInfoById(int64(id))
	if search == nil {
		return model.MovieDetailVo{List: make([]model.PlayLinkVo, 0)}
	}
	movieDetail := repository.GetMovieDetail(search.Cid, search.Mid)
	if movieDetail == nil {
		return model.MovieDetailVo{List: make([]model.PlayLinkVo, 0)}
	}
	res := model.MovieDetailVo{MovieDetail: *movieDetail}
	res.List = multipleSource(movieDetail)
	return res
}

// GetCategoryInfo 获取活跃大类信息 (动态结构版)
func (i *IndexService) GetCategoryInfo() map[string]any {
	nav := make(map[string]any)
	tree := repository.GetActiveCategoryTree()

	// 定义标准简称映射 (仅用于保持 API 兼容性，如 film, tv 等字段名)
	// 如果需要完全动态，可以考虑在 Category 表增加 Key 字段
	keyMap := map[string]string{
		"电影": "film", "电视剧": "tv", "综艺": "variety", "动漫": "cartoon", "纪录片": "document",
	}

	for _, t := range tree.Children {
		key, ok := keyMap[t.Name]
		if !ok {
			// 后备方案：使用 ID 或 Alias 首项
			key = strings.ToLower(t.Name)
		}
		nav[key] = t
	}
	return nav
}

// GetNavCategory 获取导航分类信息
func (i *IndexService) GetNavCategory() []*model.Category {
	tree := repository.GetActiveCategoryTree()
	cl := make([]*model.Category, 0)
	for _, c := range tree.Children {
		if c.Show {
			cl = append(cl, &model.Category{
				Id:        c.Id,
				Pid:       c.Pid,
				Name:      c.Name,
				Alias:     c.Alias,
				Show:      c.Show,
				Sort:      c.Sort,
				CreatedAt: c.CreatedAt,
				UpdatedAt: c.UpdatedAt,
			})
		}
	}
	return cl
}

// SearchFilmInfo 获取关键字匹配的影片信息
func (i *IndexService) SearchFilmInfo(key string, page *dto.Page) []model.MovieBasicInfo {
	sl := repository.SearchFilmKeyword(key, page)
	var bl []model.MovieBasicInfo
	for _, s := range sl {
		bl = append(bl, repository.GetBasicInfoByKey(s.Cid, s.Mid))
	}
	return bl
}

// GetFilmCategory 根据Pid或Cid获取指定的分页数据
func (i *IndexService) GetFilmCategory(id int64, idType string, page *dto.Page) []model.MovieBasicInfo {
	var basicList []model.MovieBasicInfo
	switch idType {
	case "pid":
		basicList = repository.GetMovieListByPid(id, page)
	case "cid":
		basicList = repository.GetMovieListByCid(id, page)
	}
	return basicList
}

// GetPidCategory 获取pid对应的分类信息
func (i *IndexService) GetPidCategory(pid int64) *model.CategoryTree {
	pid = repository.ResolveCategoryID(pid)
	tree := repository.GetCategoryTree()
	for _, t := range tree.Children {
		if t.Id == pid {
			return &model.CategoryTree{
				Id:        t.Id,
				Pid:       t.Pid,
				Name:      t.Name,
				Alias:     t.Alias,
				Show:      t.Show,
				Sort:      t.Sort,
				CreatedAt: t.CreatedAt,
				UpdatedAt: t.UpdatedAt,
				Children:  t.Children,
			}
		}
	}
	return nil
}

// RelateMovie 根据当前影片信息匹配相关的影片
func (i *IndexService) RelateMovie(detail model.MovieDetail, page *dto.Page) []model.MovieBasicInfo {
	// 关键修复：从数据库获取规范化后的 SearchInfo，而不是直接使用 detail 中不可信的 Cid/Pid
	search := repository.GetSearchInfoById(detail.Id)
	if search == nil {
		// 备选方案：如果 SearchInfo 暂无，则构造一个简易的
		search = &model.SearchInfo{
			Cid:      detail.Cid,
			Pid:      detail.Pid,
			Name:     detail.Name,
			ClassTag: detail.ClassTag,
			Area:     detail.Area,
			Language: detail.Language,
		}
	}
	return repository.GetRelateMovieBasicInfo(*search, page)
}

// SearchTags 整合对应分类的搜索tag
func (i *IndexService) SearchTags(st model.SearchTagsVO) map[string]any {
	return repository.GetSearchTag(st)
}

func multipleSource(detail *model.MovieDetail) []model.PlayLinkVo {
	master := repository.GetCollectSourceListByGrade(model.MasterCollect)
	if len(master) == 0 || len(detail.PlayList) == 0 {
		return make([]model.PlayLinkVo, 0)
	}
	firstList := detail.PlayList[0]
	if firstList == nil {
		firstList = []model.MovieUrlInfo{}
	}
	playList := []model.PlayLinkVo{{Id: master[0].Id, Name: master[0].Name, LinkList: firstList}}

	names := make([]string, 0, 8)
	seenKeys := make(map[string]struct{}, 8)
	appendKey := func(k string) {
		if k == "" {
			return
		}
		if _, ok := seenKeys[k]; ok {
			return
		}
		seenKeys[k] = struct{}{}
		names = append(names, k)
	}
	if detail.DbId > 0 {
		appendKey(utils.GenerateHashKey(detail.DbId))
	}
	for _, v := range utils.NormalizeTitleCandidates(detail.Name) {
		appendKey(utils.GenerateHashKey(v))
	}

	if len(detail.SubTitle) > 0 && strings.Contains(detail.SubTitle, ",") {
		for v := range strings.SplitSeq(detail.SubTitle, ",") {
			for _, c := range utils.NormalizeTitleCandidates(v) {
				appendKey(utils.GenerateHashKey(c))
			}
		}
	}
	if len(detail.SubTitle) > 0 && strings.Contains(detail.SubTitle, "/") {
		for v := range strings.SplitSeq(detail.SubTitle, "/") {
			for _, c := range utils.NormalizeTitleCandidates(v) {
				appendKey(utils.GenerateHashKey(c))
			}
		}
	}
	sc := repository.GetCollectSourceListByGrade(model.SlaveCollect)
	for _, s := range sc {
		pl := repository.GetMultiplePlayByKeys(s.Id, names)
		if len(pl) > 0 {
			playList = append(playList, model.PlayLinkVo{Id: s.Id, Name: s.Name, LinkList: pl})
		}
	}

	return playList
}

// GetFilmsByTags 通过searchTag 返回满足条件的分页影片信息
func (i *IndexService) GetFilmsByTags(st model.SearchTagsVO, page *dto.Page) []model.MovieBasicInfo {
	sl := repository.GetSearchInfosByTags(st, page)
	return repository.GetBasicInfoBySearchInfos(sl...)
}

// GetFilmClassify 通过Pid返回当前所属分类下的首页展示数据
func (i *IndexService) GetFilmClassify(pid int64, page *dto.Page) map[string]any {
	res := make(map[string]any)
	res["news"] = repository.GetMovieListBySort(0, pid, page)
	res["top"] = repository.GetMovieListBySort(1, pid, page)
	res["recent"] = repository.GetMovieListBySort(2, pid, page)
	return res
}
