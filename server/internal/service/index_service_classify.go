package service

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
)

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

// GetFilmCategory 根据Pid或Cid获取指定的分页数据
func (i *IndexService) GetFilmCategory(id int64, idType string, page *dto.Page) []model.MovieBasicInfo {
	var basicList []model.MovieBasicInfo
	version := filmsnapshot.GetActiveReadModelVersion()
	page = normalizeIndexPage(page)
	switch idType {
	case "pid":
		basicList = filmsnapshot.GetSnapshotMovieListByCategoryPage(version, "pid", id, page)
	case "cid":
		basicList = filmsnapshot.GetSnapshotMovieListByCategoryPage(version, "cid", id, page)
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

// SearchTags 整合对应分类的搜索tag
func (i *IndexService) SearchTags(st model.SearchTagsVO) map[string]any {
	return filmsnapshot.GetFilterOptionSnapshot(filmsnapshot.GetActiveReadModelVersion(), st.Pid)
}

// GetFilmsByTags 通过searchTag 返回满足条件的分页影片信息
func (i *IndexService) GetFilmsByTags(st model.SearchTagsVO, page *dto.Page) ([]model.MovieBasicInfo, error) {
	page = normalizeIndexPage(page)
	if err := validateReadModelSearchTags(st); err != nil {
		return nil, err
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	sl := filmsnapshot.ListFilmSnapshotsByTagsFast(version, st, page)
	return filmshared.BuildMovieBasicInfosFromSnapshots(sl...), nil
}

// GetFilmClassify 通过Pid返回当前所属分类下的首页展示数据
func (i *IndexService) GetFilmClassify(pid int64, page *dto.Page) map[string]any {
	version := filmsnapshot.GetActiveReadModelVersion()
	if version == "" {
		version = filmsnapshot.GetActiveSnapshotVersion()
	}
	if version == "" {
		return map[string]any{
			"news":   []model.MovieBasicInfo{},
			"top":    []model.MovieBasicInfo{},
			"recent": []model.MovieBasicInfo{},
		}
	}
	cacheKey := filmsnapshot.SnapshotClassifyCacheKey(version, pid, page)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached map[string]any
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	limit := 21
	if page != nil && page.PageSize > 0 {
		limit = page.PageSize
	}

	var (
		newsMovies   []model.MovieBasicInfo
		topMovies    []model.MovieBasicInfo
		recentMovies []model.MovieBasicInfo
		wg           sync.WaitGroup
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		newsMovies = filmsnapshot.GetSnapshotTopMoviesBySortFast(version, 0, pid, limit)
	}()
	go func() {
		defer wg.Done()
		topMovies = filmsnapshot.GetSnapshotTopMoviesBySortFast(version, 1, pid, limit)
	}()
	go func() {
		defer wg.Done()
		recentMovies = filmsnapshot.GetSnapshotTopMoviesBySortFast(version, 2, pid, limit)
	}()
	wg.Wait()

	if newsMovies == nil {
		newsMovies = []model.MovieBasicInfo{}
	}
	if topMovies == nil {
		topMovies = []model.MovieBasicInfo{}
	}
	if recentMovies == nil {
		recentMovies = []model.MovieBasicInfo{}
	}

	res := make(map[string]any, 3)
	res["news"] = newsMovies
	res["top"] = topMovies
	res["recent"] = recentMovies
	if db.Rdb != nil {
		if data, err := json.Marshal(res); err == nil {
			db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Hour*12)
		}
	}
	return res
}
