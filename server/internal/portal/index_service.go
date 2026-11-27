package portal

import (
	"encoding/json"
	"fmt"
	"log"
	"server/internal/banner"
	"server/internal/film/catalog"
	"strings"
	"time"

	"server/internal/config"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/recommend"
	"server/internal/repobase"
	"server/internal/sourcecatalog"

	"golang.org/x/sync/singleflight"
)

type IndexService struct{}

var (
	IndexSvc     = new(IndexService)
	indexPageSF  singleflight.Group
	indexClassSF singleflight.Group
)

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
	if page.PageSize > 48 {
		page.PageSize = 48
	}
	return page
}

func logSlowIndexServiceStep(name string, startedAt time.Time, fields ...any) {
	cost := time.Since(startedAt)
	if cost < 500*time.Millisecond {
		return
	}
	args := append([]any{"[IndexService][Slow]", name, "cost", cost}, fields...)
	log.Println(args...)
}

// IndexPage 首页。所有推荐与分类展示楼层均按后台推荐编排输出，结果缓存 10 分钟。
func (i *IndexService) IndexPage(sourceIdOpt ...string) (map[string]any, error) {
	if err := filmsnapshot.WarmSearchMetaIndex(); err != nil {
		return nil, err
	}
	var sourceId string
	if len(sourceIdOpt) > 0 {
		sourceId = strings.TrimSpace(sourceIdOpt[0])
	}
	if sourceId == "" {
		if defSource := sourcecatalog.CurrentPrimarySource(); defSource != nil {
			sourceId = defSource.Id
		}
	}
	if sourceId == "" {
		return map[string]any{
			"category": model.CategoryTree{
				Id: 0, Pid: -1, Name: "分类信息", Show: true,
				Children: make([]*model.CategoryTree, 0),
			},
			"content":       make([]map[string]any, 0),
			"banners":       make(model.Banners, 0),
			"recommends":    []model.HomeRecommendSection{},
			"currentSource": "",
		}, nil
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	cacheKey := fmt.Sprintf("%s:v%s:src_%s:home:v2", config.IndexPageCacheKey, version, sourceId)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached map[string]any
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached, nil
			}
		}
	}
	val, err, _ := indexPageSF.Do(cacheKey, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var cached map[string]any
				if json.Unmarshal([]byte(data), &cached) == nil {
					return cached, nil
				}
			}
		}
		return i.buildIndexPage(sourceId, version, cacheKey)
	})
	if err != nil {
		return nil, err
	}
	return val.(map[string]any), nil
}

func (i *IndexService) buildIndexPage(sourceId, version, cacheKey string) (map[string]any, error) {
	listGen := filmsnapshot.GetSearchCacheVersion()
	pageGen := repobase.IndexPageGeneration()
	tree, err := catalog.GetActiveCategoryTree(sourceId)
	if err != nil {
		return nil, err
	}
	// 轮播读失败时首页照常返回（轮播为空），但这次结果不进缓存，避免空轮播被缓存 10 分钟。
	cacheable := true
	banners, err := banner.BannerSvc.HomeBanners(sourceId)
	if err != nil {
		log.Printf("[Index] 读取首页轮播失败: %v", err)
		banners = make(model.Banners, 0)
		cacheable = false
	}
	recommends, recCacheable := recommend.HomeRecommendSvc.HomeSections(sourceId)
	if !recCacheable {
		cacheable = false
	}
	res := map[string]any{
		"category":      tree,
		"content":       buildLegacyHomeContent(recommends),
		"banners":       banners,
		"recommends":    recommends,
		"currentSource": sourceId,
	}
	if db.Rdb != nil && cacheable && repobase.IndexPageGeneration() == pageGen {
		if data, err := json.Marshal(res); err == nil {
			filmsnapshot.StoreCacheIfCurrent(cacheKey, string(data), 10*time.Minute, listGen)
			if repobase.IndexPageGeneration() != pageGen {
				_ = db.Rdb.Del(db.Cxt, cacheKey).Err()
			}
		}
	}
	return res, nil
}

// buildLegacyHomeContent 从统一楼层中提取站点分类分组，兼容旧版 Android/鸿蒙客户端对 content 的读取。
func buildLegacyHomeContent(recommends []model.HomeRecommendSection) []map[string]any {
	content := make([]map[string]any, 0, len(recommends))
	for _, sec := range recommends {
		if sec.Kind != "category" || sec.Nav == nil {
			continue
		}
		movies := make([]model.MovieBasicInfo, 0, len(sec.Items))
		for _, it := range sec.Items {
			movies = append(movies, it.MovieBasicInfo)
		}
		hot := sec.Hot
		if hot == nil {
			hot = make([]model.MovieBasicInfo, 0)
		}
		content = append(content, map[string]any{
			"nav":    sec.Nav,
			"movies": movies,
			"hot":    hot,
		})
	}
	return content
}
