package service

import (
	"encoding/json"
	"fmt"
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

func normalizeIndexPage(page *dto.Page) *dto.Page {
	if page == nil {
		return &dto.Page{Current: 1, PageSize: 20}
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	if page.PageSize <= 0 {
		page.PageSize = 20
	}
	return page
}

// IndexPage 首页数据处理
func (i *IndexService) IndexPage() map[string]any {
	version := filmrepo.GetActiveReadModelVersion()
	ruleVersion := repository.GetRuleVersion()
	// 1. 尝试从 Redis 获取缓存
	cacheKey := fmt.Sprintf("%s:s%s:r%s", repository.GetVersionedIndexPageCacheKey(), version, ruleVersion)
	if version != "" {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			res := make(map[string]any)
			if json.Unmarshal([]byte(data), &res) == nil {
				return res
			}
		}
	}

	Info := make(map[string]any)
	tree := repository.GetActiveCategoryTree()
	Info["category"] = tree
	list := make([]map[string]any, 0)
	for _, c := range tree.Children {
		var movies []model.MovieBasicInfo
		var hotMovies []model.MovieBasicInfo
		if c.Children != nil {
			movies = filmrepo.GetSnapshotMovieListByCategory(version, "pid", c.Id, 14, 0)
			hotMovies = filmrepo.GetSnapshotHotMovieListByCategory(version, "pid", c.Id, 14, 0)
		} else {
			movies = filmrepo.GetSnapshotMovieListByCategory(version, "cid", c.Id, 14, 0)
			hotMovies = filmrepo.GetSnapshotHotMovieListByCategory(version, "cid", c.Id, 14, 0)
		}
		if movies == nil {
			movies = make([]model.MovieBasicInfo, 0)
		}
		if hotMovies == nil {
			hotMovies = make([]model.MovieBasicInfo, 0)
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
	if version != "" {
		if data, err := json.Marshal(Info); err == nil {
			db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Hour*24)
		}
	}

	return Info
}

// GetFilmDetail 影片详情信息页面处理
func (i *IndexService) GetFilmDetail(id int) (model.MovieDetailVo, error) {
	version := filmrepo.GetActiveReadModelVersion()
	snapshot := filmrepo.GetSnapshotByMid(version, int64(id))
	if snapshot == nil {
		return model.MovieDetailVo{}, nil
	}
	movieDetail := filmrepo.GetMovieDetailBySnapshot(*snapshot)
	if movieDetail == nil {
		filmrepo.DeleteActiveSnapshotsByMids(snapshot.Mid)
		return model.MovieDetailVo{}, nil
	}
	res := model.MovieDetailVo{MovieDetail: *movieDetail}
	res.List = multipleSource(snapshot, movieDetail)
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
	version := filmrepo.GetActiveReadModelVersion()
	sl := filmrepo.SearchSnapshotsByKeywordFast(version, key, page)
	return filmrepo.BuildMovieBasicInfosFromSnapshots(sl...)
}

// GetFilmCategory 根据Pid或Cid获取指定的分页数据
func (i *IndexService) GetFilmCategory(id int64, idType string, page *dto.Page) []model.MovieBasicInfo {
	var basicList []model.MovieBasicInfo
	version := filmrepo.GetActiveReadModelVersion()
	page = normalizeIndexPage(page)
	switch idType {
	case "pid":
		basicList = filmrepo.GetSnapshotMovieListByCategoryPage(version, "pid", id, page)
	case "cid":
		basicList = filmrepo.GetSnapshotMovieListByCategoryPage(version, "cid", id, page)
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
	page = normalizeIndexPage(page)
	version := filmrepo.GetActiveReadModelVersion()
	snapshot := filmrepo.GetSnapshotByMid(version, detail.Id)
	if snapshot == nil {
		return []model.MovieBasicInfo{}
	}
	list := filmrepo.ListRelatedSnapshotsReadModel(version, *snapshot, page)
	return filmrepo.BuildMovieBasicInfosFromSnapshots(list...)
}

// SearchTags 整合对应分类的搜索tag
func (i *IndexService) SearchTags(st model.SearchTagsVO) map[string]any {
	return filmrepo.GetFilterOptionSnapshot(filmrepo.GetActiveReadModelVersion(), st.Pid)
}

func multipleSource(snapshot *model.FilmListSnapshot, detail *model.MovieDetail) []model.PlayLinkVo {
	playList := buildPrimaryPlaySources(snapshot, detail)
	names := filmrepo.LoadMovieMatchKeysBySnapshot(snapshot, detail)
	if len(names) == 0 {
		return playList
	}

	slaveSources := repository.GetCollectSourceListByGrade(model.SlaveCollect)
	querySources := make([]model.FilmSource, 0, len(slaveSources))
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

	for _, source := range slaveSources {
		if !source.State {
			continue
		}
		if _, ok := seenSourceIDs[source.Id]; ok {
			continue
		}
		querySources = append(querySources, source)
	}

	groupsBySource := filmrepo.GetMultiplePlayGroupsBySourcesAndKeys(querySources, names)
	for _, source := range querySources {
		groups := groupsBySource[source.Id]
		if len(groups) > 0 {
			playList = append(playList, groups...)
		}
	}

	return playList
}

func buildPrimaryPlaySources(snapshot *model.FilmListSnapshot, detail *model.MovieDetail) []model.PlayLinkVo {
	if detail == nil || len(detail.PlayList) == 0 {
		return make([]model.PlayLinkVo, 0)
	}

	siteName := ""
	if snapshot != nil && snapshot.SourceId != "" {
		if source := repository.FindCollectSourceById(snapshot.SourceId); source != nil {
			siteName = source.Name
		}
	}

	playList := make([]model.PlayLinkVo, 0, len(detail.PlayList))
	sourceID := ""
	if snapshot != nil {
		sourceID = snapshot.SourceId
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
func (i *IndexService) GetFilmsByTags(st model.SearchTagsVO, page *dto.Page) ([]model.MovieBasicInfo, error) {
	page = normalizeIndexPage(page)
	if err := validateReadModelSearchTags(st); err != nil {
		return nil, err
	}
	version := filmrepo.GetActiveReadModelVersion()
	sl := filmrepo.ListFilmSnapshotsByTagsFast(version, st, page)
	return filmrepo.BuildMovieBasicInfosFromSnapshots(sl...), nil
}

// GetFilmClassify 通过Pid返回当前所属分类下的首页展示数据
func (i *IndexService) GetFilmClassify(pid int64, page *dto.Page) map[string]any {
	version := filmrepo.GetActiveReadModelVersion()
	cacheKey := filmrepo.SnapshotClassifyCacheKey(version, pid, page)
	if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
		var cached map[string]any
		if json.Unmarshal([]byte(data), &cached) == nil {
			return cached
		}
	}

	res := make(map[string]any)
	res["news"] = filmrepo.GetSnapshotMovieListBySort(version, 0, pid, page)
	res["top"] = filmrepo.GetSnapshotMovieListBySort(version, 1, pid, page)
	res["recent"] = filmrepo.GetSnapshotMovieListBySort(version, 2, pid, page)
	if data, err := json.Marshal(res); err == nil {
		db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Hour*12)
	}
	return res
}
