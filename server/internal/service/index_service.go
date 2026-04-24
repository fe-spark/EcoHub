package service

import (
	"encoding/json"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
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
	tree := model.CategoryTree{Id: 0, Name: "分类信息", Children: make([]*model.CategoryTree, 0)}
	sysTree := repository.GetCategoryTree()
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
			movies = filmrepo.GetMovieListByPidLimit(c.Id, 14, 0)
			hotMovies = filmrepo.GetHotMovieByPidLimit(c.Id, 14, 0)
		} else {
			movies = filmrepo.GetMovieListByCidLimit(c.Id, 14, 0)
			hotMovies = filmrepo.GetHotMovieByCidLimit(c.Id, 14, 0)
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
func (i *IndexService) GetFilmDetail(id int) (model.MovieDetailVo, error) {
	search := filmrepo.GetSearchInfoById(int64(id))
	if search == nil {
		return model.MovieDetailVo{}, nil
	}
	movieDetail := filmrepo.GetMovieDetail(search.Cid, search.Mid)
	if movieDetail == nil {
		if err := filmrepo.DelFilmSearch(search.Mid); err != nil {
			return model.MovieDetailVo{}, err
		}
		return model.MovieDetailVo{}, nil
	}
	res := model.MovieDetailVo{MovieDetail: *movieDetail}
	res.List = multipleSource(search, movieDetail)
	return res, nil
}

// GetCategoryInfo 获取活跃大类信息 (动态结构版)
func (i *IndexService) GetCategoryInfo() map[string]any {
	nav := make(map[string]any)
	tree := repository.GetCategoryTree()

	for _, t := range tree.Children {
		if !t.Show {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(t.Alias))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(t.Name))
		}
		if key == "" {
			continue
		}
		nav[key] = t
	}
	return nav
}

// GetNavCategory 获取导航分类信息
func (i *IndexService) GetNavCategory() []*model.Category {
	tree := repository.GetCategoryTree()
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
	sl := filmrepo.SearchFilmKeyword(key, page)
	return filmrepo.GetBasicInfoBySearchInfos(sl...)
}

// GetFilmCategory 根据Pid或Cid获取指定的分页数据
func (i *IndexService) GetFilmCategory(id int64, idType string, page *dto.Page) []model.MovieBasicInfo {
	var basicList []model.MovieBasicInfo
	switch idType {
	case "pid":
		basicList = filmrepo.GetMovieListByPid(id, page)
	case "cid":
		basicList = filmrepo.GetMovieListByCid(id, page)
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
	search := filmrepo.GetSearchInfoById(detail.Id)
	if search == nil {
		return []model.MovieBasicInfo{}
	}
	return filmrepo.GetRelateMovieBasicInfo(*search, page)
}

// SearchTags 整合对应分类的搜索tag
func (i *IndexService) SearchTags(st model.SearchTagsVO) map[string]any {
	return filmrepo.GetSearchTag(st)
}

func multipleSource(search *model.SearchInfo, detail *model.MovieDetail) []model.PlayLinkVo {
	playList := buildPrimaryPlaySources(search, detail)
	names := filmrepo.LoadMovieMatchKeys(search, detail)
	if len(names) == 0 {
		return playList
	}

	sc := repository.GetCollectSourceListByGrade(model.SlaveCollect)
	seenSourceIDs := make(map[string]struct{}, len(playList))
	for _, item := range playList {
		sourceID := strings.TrimSpace(item.SourceId)
		if sourceID == "" {
			sourceID = strings.TrimSpace(item.Id)
		}
		if sourceID == "" {
			continue
		}
		seenSourceIDs[sourceID] = struct{}{}
	}

	for _, source := range sc {
		if !source.State {
			continue
		}
		if _, ok := seenSourceIDs[source.Id]; ok {
			continue
		}
		groups := filmrepo.GetMultiplePlayGroupsByKeys(source.Id, source.Name, names)
		if len(groups) > 0 {
			playList = append(playList, groups...)
		}
	}

	return playList
}

func buildPrimaryPlaySources(search *model.SearchInfo, detail *model.MovieDetail) []model.PlayLinkVo {
	if detail == nil || len(detail.PlayList) == 0 {
		return make([]model.PlayLinkVo, 0)
	}

	siteName := ""
	if search != nil && search.SourceId != "" {
		if source := repository.FindCollectSourceById(search.SourceId); source != nil {
			siteName = source.Name
		}
	}

	playList := make([]model.PlayLinkVo, 0, len(detail.PlayList))
	sourceID := ""
	if search != nil {
		sourceID = search.SourceId
	}
	for index, links := range detail.PlayList {
		if len(links) == 0 {
			continue
		}

		rawName := strings.TrimSpace(resolvePrimarySourceName(detail.PlayFrom, index))
		sourceName := filmrepo.BuildDisplaySourceName(siteName, rawName, index, len(detail.PlayList))
		groupID := filmrepo.BuildPlayGroupID(sourceID, rawName, index, len(detail.PlayList))

		playList = append(playList, model.PlayLinkVo{
			Id:       groupID,
			SourceId: sourceID,
			Name:     sourceName,
			LinkList: links,
		})
	}

	return playList
}

func resolvePrimarySourceName(playFrom []string, index int) string {
	if index < 0 || index >= len(playFrom) {
		return ""
	}
	return playFrom[index]
}

// GetFilmsByTags 通过searchTag 返回满足条件的分页影片信息
func (i *IndexService) GetFilmsByTags(st model.SearchTagsVO, page *dto.Page) []model.MovieBasicInfo {
	sl := filmrepo.GetSearchInfosByTags(st, page)
	return filmrepo.GetBasicInfoBySearchInfos(sl...)
}

// GetFilmClassify 通过Pid返回当前所属分类下的首页展示数据
func (i *IndexService) GetFilmClassify(pid int64, page *dto.Page) map[string]any {
	res := make(map[string]any)
	res["news"] = filmrepo.GetMovieListBySort(0, pid, page)
	res["top"] = filmrepo.GetMovieListBySort(1, pid, page)
	res["recent"] = filmrepo.GetMovieListBySort(2, pid, page)
	return res
}
