package portal

import (
	"encoding/json"
	"errors"
	"fmt"
	"server/internal/film/catalog"
	"strings"
	"sync"
	"time"

	filmshared "server/internal/film/shared"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
)

// GetCategoryInfo 获取活跃大类信息 (动态结构版)
func (i *IndexService) GetCategoryInfo() map[string]any {
	nav := make(map[string]any)
	tree := catalog.GetCategoryTree()

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

// ErrNavCategorySourceUnavailable 指定的采集站不存在或已停用。
var ErrNavCategorySourceUnavailable = errors.New("采集站不存在或已停用")

// GetNavCategory 获取导航分类信息 (按当前生效站点隔离)
func (i *IndexService) GetNavCategory(sourceIdOpt ...string) ([]*model.Category, error) {
	var sourceId string
	if len(sourceIdOpt) > 0 {
		sourceId = strings.TrimSpace(sourceIdOpt[0])
	}
	if sourceId != "" {
		// 显式传站点时（公开接口 /categories?sourceId=）先校验站点存在且启用，
		// 不存在的值不进分类树缓存，避免任意参数刷出大量缓存键，也不暴露停用站点的分类。
		if src := sourcecatalog.FindCollectSourceById(sourceId); src == nil || !src.State {
			return nil, ErrNavCategorySourceUnavailable
		}
	} else if active := sourcecatalog.CurrentPrimarySource(); active != nil {
		sourceId = active.Id
	}
	tree, err := catalog.GetActiveCategoryTree(sourceId)
	if err != nil {
		return nil, err
	}
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
	return cl, nil
}

// GetPidCategory 获取pid对应的分类信息
func (i *IndexService) GetPidCategory(pid int64, sourceIdOpt ...string) *model.CategoryTree {
	var sourceId string
	if len(sourceIdOpt) > 0 {
		sourceId = strings.TrimSpace(sourceIdOpt[0])
	}
	if sourceId == "" {
		if active := sourcecatalog.CurrentPrimarySource(); active != nil {
			sourceId = active.Id
		}
	}
	if sourceId != "" {
		return catalog.LookupRootSourceType(sourceId, pid)
	}
	return nil
}

// SearchTags 整合对应分类的搜索tag
func (i *IndexService) SearchTags(st model.SearchTagsVO) (map[string]any, error) {
	sourceId := strings.TrimSpace(st.SourceId)
	if sourceId == "" {
		if active := sourcecatalog.CurrentPrimarySource(); active != nil {
			sourceId = active.Id
		}
	}
	if sourceId == "" {
		return map[string]any{"tags": map[string]any{}, "titles": map[string]any{}}, nil
	}
	return filmsnapshot.GetFilterOptionSnapshot(filmsnapshot.GetActiveReadModelVersion(), st.Pid, sourceId)
}

// GetFilmsByTags 通过searchTag 返回满足条件的分页影片信息
func (i *IndexService) GetFilmsByTags(st model.SearchTagsVO, page *dto.Page) ([]model.MovieBasicInfo, error) {
	page = normalizeIndexPage(page)
	if err := validateReadModelSearchTags(st); err != nil {
		return nil, err
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	sl, err := filmsnapshot.ListFilmSnapshotsByTagsFast(version, st, page)
	if err != nil {
		return nil, err
	}
	return filmshared.BuildMovieBasicInfosFromSnapshots(sl...), nil
}

// IsSearchIndexNotReadyError 判断错误是否为数据库可用但检索索引尚未就绪（如空库尚未采集）。
func IsSearchIndexNotReadyError(err error) bool {
	return db.Mdb != nil && errors.Is(err, filmsnapshot.ErrSearchIndexMissing)
}

// GetFilmClassify 通过Pid返回当前所属分类下的首页展示数据
func (i *IndexService) GetFilmClassify(pid int64, page *dto.Page, sourceIdOpt ...string) (map[string]any, error) {
	var sourceId string
	if len(sourceIdOpt) > 0 {
		sourceId = strings.TrimSpace(sourceIdOpt[0])
	}
	// 仅在未指定采集站（默认本地分类ID）时用本地可见性校验；指定站点的 pid 是源站 type_id，由源站分类树决定可见性。
	if sourceId == "" && pid > 0 && !taxonomy.IsCategoryVisible(0, pid) {
		return map[string]any{
			"news":   []model.MovieBasicInfo{},
			"top":    []model.MovieBasicInfo{},
			"recent": []model.MovieBasicInfo{},
		}, nil
	}
	if sourceId == "" {
		if active := sourcecatalog.CurrentPrimarySource(); active != nil {
			sourceId = active.Id
		}
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	if version == "" {
		version = filmsnapshot.GetActiveSnapshotVersion()
	}
	if version == "" {
		return nil, filmsnapshot.ErrSearchIndexMissing
	}
	cacheKey := filmsnapshot.SnapshotClassifyCacheKey(version, pid, page)
	if sourceId != "" {
		cacheKey = fmt.Sprintf("%s:src_%s:st3", cacheKey, sourceId)
	}
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached map[string]any
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached, nil
			}
		}
	}

	limit := 24
	if page != nil && page.PageSize > 0 {
		limit = page.PageSize
	}
	sfKey := fmt.Sprintf("%s:%d", cacheKey, limit)
	val, err, _ := indexClassSF.Do(sfKey, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var cached map[string]any
				if json.Unmarshal([]byte(data), &cached) == nil {
					return cached, nil
				}
			}
		}
		listGen := filmsnapshot.GetSearchCacheVersion()

		var (
			newsMovies   []model.MovieBasicInfo
			topMovies    []model.MovieBasicInfo
			recentMovies []model.MovieBasicInfo
			wg           sync.WaitGroup
			errMu        sync.Mutex
			pageErr      error
		)
		setPageErr := func(err error) {
			if err == nil {
				return
			}
			errMu.Lock()
			if pageErr == nil {
				pageErr = err
			}
			errMu.Unlock()
		}

		wg.Add(3)
		go func() {
			defer wg.Done()
			list, err := filmsnapshot.GetSnapshotTopMoviesBySortFastWithSource(version, sourceId, 0, pid, limit)
			setPageErr(err)
			newsMovies = list
		}()
		go func() {
			defer wg.Done()
			list, err := filmsnapshot.GetSnapshotTopMoviesBySortFastWithSource(version, sourceId, 1, pid, limit)
			setPageErr(err)
			topMovies = list
		}()
		go func() {
			defer wg.Done()
			list, err := filmsnapshot.GetSnapshotTopMoviesBySortFastWithSource(version, sourceId, 2, pid, limit)
			setPageErr(err)
			recentMovies = list
		}()
		wg.Wait()
		if pageErr != nil {
			return nil, pageErr
		}

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
				filmsnapshot.StoreCacheIfCurrent(cacheKey, string(data), time.Hour*12, listGen)
			}
		}
		return res, nil
	})
	if err != nil {
		return nil, err
	}
	return val.(map[string]any), nil
}
