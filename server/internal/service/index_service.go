package service

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	filmsnapshot "server/internal/repository/film/snapshot"

	"golang.org/x/sync/singleflight"
)

type IndexService struct{}

var IndexSvc = new(IndexService)

func init() {
	filmsnapshot.RegisterSnapshotPublishedHook(func(version string) {
		startedAt := time.Now()
		_ = IndexSvc.IndexPage()
		log.Printf("[IndexService][Warmup] 快照发布后首页缓存预热完成 version=%s cost=%s", version, time.Since(startedAt))
	})
}

func normalizeIndexPage(page *dto.Page) *dto.Page {
	if page == nil {
		return &dto.Page{Current: 1, PageSize: 20}
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	if page.Current > 50 {
		page.Current = 50
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

var indexPageSfGroup singleflight.Group

// IndexPage 首页数据处理
func (i *IndexService) IndexPage() map[string]any {
	version := filmsnapshot.GetActiveReadModelVersion()
	ruleVersion := repository.GetRuleVersion()
	cacheKey := fmt.Sprintf("%s:s%s:r%s", repository.GetVersionedIndexPageCacheKey(), version, ruleVersion)

	// 1. 尝试从 Redis 获取缓存
	if version != "" && db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			res := make(map[string]any)
			if json.Unmarshal([]byte(data), &res) == nil && res != nil {
				res["banners"] = overlayBannerLiveRemarks(repository.GetBanners())
				overlayDynamicCategoryMovies(version, res)
				return res
			}
		}
	}

	val, err, _ := indexPageSfGroup.Do("IndexPage", func() (any, error) {
		// Double check 缓存
		if version != "" && db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				res := make(map[string]any)
				if json.Unmarshal([]byte(data), &res) == nil && res != nil {
					return res, nil
				}
			}
		}

		info := make(map[string]any)
		tree := repository.GetActiveCategoryTree()
		info["category"] = tree
		list := make([]map[string]any, len(tree.Children))
		var wg sync.WaitGroup
		for idx, c := range tree.Children {
			wg.Add(1)
			go func(i int, cat *model.CategoryTree) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[IndexPage] 加载分类 %d 发生异常: %v", cat.Id, r)
					}
					wg.Done()
				}()
				var movies []model.MovieBasicInfo
				var hotMovies []model.MovieBasicInfo
				if cat.Children != nil {
					movies = filmsnapshot.GetSnapshotMovieListByCategory(version, "pid", cat.Id, 14, 0)
					hotMovies = filmsnapshot.GetSnapshotHotMovieListByCategory(version, "pid", cat.Id, 14, 0)
				} else {
					movies = filmsnapshot.GetSnapshotMovieListByCategory(version, "cid", cat.Id, 14, 0)
					hotMovies = filmsnapshot.GetSnapshotHotMovieListByCategory(version, "cid", cat.Id, 14, 0)
				}
				if movies == nil {
					movies = make([]model.MovieBasicInfo, 0)
				}
				if hotMovies == nil {
					hotMovies = make([]model.MovieBasicInfo, 0)
				}
				list[i] = map[string]any{"nav": cat, "movies": movies, "hot": hotMovies}
			}(idx, c)
		}
		wg.Wait()
		info["content"] = list
		banners := overlayBannerLiveRemarks(repository.GetBanners())
		if banners == nil {
			banners = make(model.Banners, 0)
		}
		info["banners"] = banners

		// 2. 写入 Redis 缓存
		if version != "" && db.Rdb != nil {
			if data, err := json.Marshal(info); err == nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Hour*24).Err()
			}
		}
		return info, nil
	})

	if err != nil || val == nil {
		out := map[string]any{
			"category": repository.GetActiveCategoryTree(),
			"content":  []map[string]any{},
			"banners":  overlayBannerLiveRemarks(repository.GetBanners()),
		}
		overlayDynamicCategoryMovies(version, out)
		return out
	}

	rawInfo, ok := val.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	outInfo := make(map[string]any, len(rawInfo))
	for k, v := range rawInfo {
		outInfo[k] = v
	}
	outInfo["banners"] = overlayBannerLiveRemarks(repository.GetBanners())
	overlayDynamicCategoryMovies(version, outInfo)
	return outInfo
}

func extractCategoryID(nav any) (id int64, isPid bool) {
	if nav == nil {
		return 0, false
	}
	switch item := nav.(type) {
	case model.CategoryTree:
		return item.Id, len(item.Children) > 0
	case *model.CategoryTree:
		if item != nil {
			return item.Id, len(item.Children) > 0
		}
	case map[string]any:
		if v, ok := item["id"]; ok {
			switch n := v.(type) {
			case float64:
				id = int64(n)
			case int64:
				id = n
			case int:
				id = int64(n)
			}
		}
		if children, ok := item["children"]; ok && children != nil {
			switch cList := children.(type) {
			case []any:
				isPid = len(cList) > 0
			case []*model.CategoryTree:
				isPid = len(cList) > 0
			}
		}
	}
	return id, isPid
}

func processDynamicRecommendSection(secMap map[string]any, version string) map[string]any {
	itemCopy := make(map[string]any, len(secMap))
	for k, v := range secMap {
		itemCopy[k] = v
	}
	catID, isPid := extractCategoryID(itemCopy["nav"])
	if catID > 0 {
		field := "cid"
		if isPid {
			field = "pid"
		}
		dynamicMovies := filmsnapshot.GetSnapshotDynamicHotMovieListByCategory(version, field, catID, 14, 50)
		if len(dynamicMovies) > 0 {
			itemCopy["movies"] = dynamicMovies
		}
	}
	return itemCopy
}

func overlayDynamicCategoryMovies(version string, outInfo map[string]any) {
	rawContent, ok := outInfo["content"]
	if !ok || rawContent == nil {
		return
	}

	switch list := rawContent.(type) {
	case []map[string]any:
		newList := make([]map[string]any, len(list))
		var wg sync.WaitGroup
		for i, section := range list {
			wg.Add(1)
			go func(idx int, sec map[string]any) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[overlayDynamicCategoryMovies] 抽样板块发生异常: %v", r)
					}
					wg.Done()
				}()
				newList[idx] = processDynamicRecommendSection(sec, version)
			}(i, section)
		}
		wg.Wait()
		outInfo["content"] = newList
	case []any:
		newList := make([]any, len(list))
		var wg sync.WaitGroup
		for i, rawSec := range list {
			wg.Add(1)
			go func(idx int, raw any) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[overlayDynamicCategoryMovies] 抽样板块发生异常: %v", r)
					}
					wg.Done()
				}()
				if secMap, ok := raw.(map[string]any); ok {
					newList[idx] = processDynamicRecommendSection(secMap, version)
				} else {
					newList[idx] = raw
				}
			}(i, rawSec)
		}
		wg.Wait()
		outInfo["content"] = newList
	}
}

func applyLiveRemarksToMovies(list []model.MovieBasicInfo) {
	if len(list) == 0 {
		return
	}
	mids := make([]int64, 0, len(list))
	for _, item := range list {
		if item.Id > 0 {
			mids = append(mids, item.Id)
		}
	}
	live := filmrepo.LiveUpdateRemarksByMIDs(mids)
	if len(live) == 0 {
		return
	}
	for i := range list {
		if remark, ok := live[list[i].Id]; ok {
			list[i].Remarks = remark
		}
	}
}

// OverlayBannerLiveRemarks 实时叠加片库最新状态、海报图源高清封面与幻灯图
func OverlayBannerLiveRemarks(banners model.Banners) model.Banners {
	return overlayBannerLiveRemarks(banners)
}

func overlayBannerLiveRemarks(banners model.Banners) model.Banners {
	if banners == nil {
		return make(model.Banners, 0)
	}
	if len(banners) == 0 {
		return banners
	}
	mids := make([]int64, 0, len(banners))
	for _, b := range banners {
		if b.Mid > 0 {
			mids = append(mids, b.Mid)
		}
	}
	if len(mids) == 0 {
		return banners
	}
	liveData := filmrepo.LiveBannerSnapshotsByMIDs(mids)
	if len(liveData) == 0 {
		return banners
	}
	out := make(model.Banners, len(banners))
	copy(out, banners)
	for i := range out {
		snap, ok := liveData[out[i].Mid]
		if !ok {
			continue
		}
		if snap.Remarks != "" {
			out[i].Remark = snap.Remarks
		}
		if snap.Area != "" {
			out[i].Area = snap.Area
		}
		if snap.ClassTag != "" {
			out[i].ClassTag = snap.ClassTag
		}
		if snap.Actor != "" {
			out[i].Actor = snap.Actor
		}
		if snap.Director != "" {
			out[i].Director = snap.Director
		}
		if snap.Blurb != "" {
			out[i].Blurb = snap.Blurb
		}
		if snap.Score > 0 {
			out[i].Score = snap.Score
		}
		if snap.Hits > 0 {
			out[i].Hits = snap.Hits
		}
		// 核心优先级：若该轮播项已由管理员手动自定义修改 (IsCustomPic == true)，严格展示用户的自定义图片（优先 CustomPicture，兼容历史 Picture 字段）
		customPic := strings.TrimSpace(out[i].CustomPicture)
		if customPic == "" {
			customPic = strings.TrimSpace(out[i].Picture)
		}
		if out[i].IsCustomPic && customPic != "" {
			out[i].Picture = customPic
			out[i].Poster = customPic
			if strings.TrimSpace(out[i].PictureSlide) == "" {
				out[i].PictureSlide = customPic
			}
		} else {
			dispPic := snap.DisplayPicture()
			if dispPic != "" {
				out[i].Picture = dispPic
				out[i].Poster = dispPic
			}
			dispSlide := snap.DisplayPictureSlide()
			if dispSlide != "" {
				out[i].PictureSlide = dispSlide
			} else if dispPic != "" {
				out[i].PictureSlide = dispPic
			}
		}
	}
	return out
}
