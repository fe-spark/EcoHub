package service

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
)

type IndexService struct{}

var IndexSvc = new(IndexService)

func (i *IndexService) BaselineSourceID() string {
	if active := repository.GetActiveCollectSource(); active != nil {
		return strings.TrimSpace(active.Id)
	}
	return ""
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

func categoryHomeField(cat *model.CategoryTree) (string, int64) {
	field := "cid"
	if cat != nil && len(cat.Children) > 0 {
		field = "pid"
	}
	id := int64(0)
	if cat != nil {
		id = cat.Id
	}
	return field, id
}

func infosByMid(version string, mids []int64) (map[int64]model.MovieBasicInfo, error) {
	if len(mids) == 0 {
		return map[int64]model.MovieBasicInfo{}, nil
	}
	snaps, err := filmsnapshot.GetSnapshotsByMidsOrdered(version, mids)
	if err != nil {
		return nil, err
	}
	infos := filmshared.BuildMovieBasicInfosFromSnapshots(snaps...)
	byID := make(map[int64]model.MovieBasicInfo, len(infos))
	for _, info := range infos {
		if info.Id > 0 {
			byID[info.Id] = info
		}
	}
	return byID, nil
}

func pickInfos(byID map[int64]model.MovieBasicInfo, mids []int64) []model.MovieBasicInfo {
	out := make([]model.MovieBasicInfo, 0, len(mids))
	for _, mid := range mids {
		info, ok := byID[mid]
		if !ok {
			continue
		}
		out = append(out, info)
	}
	return out
}

// IndexPage 首页。推荐片从热度池抽样，热播按热度排序。结果缓存 10 分钟，抽样在缓存期内不变。
func (i *IndexService) IndexPage(sourceIdOpt ...string) (map[string]any, error) {
	if err := filmsnapshot.WarmSearchMetaIndex(); err != nil {
		return nil, err
	}
	var sourceId string
	if len(sourceIdOpt) > 0 {
		sourceId = strings.TrimSpace(sourceIdOpt[0])
	}
	if sourceId == "" {
		if defSource := repository.PickPrimarySourceForCategory(); defSource != nil {
			sourceId = defSource.Id
		}
	}
	if sourceId == "" {
		return map[string]any{
			"category": model.CategoryTree{
				Id: 0, Pid: -1, Name: "分类信息", Show: true,
				Children: make([]*model.CategoryTree, 0),
			},
			"content":       []map[string]any{},
			"banners":       make(model.Banners, 0),
			"currentSource": "",
		}, nil
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	cacheKey := fmt.Sprintf("%s:v%s:src_%s:home", config.IndexPageCacheKey, version, sourceId)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached map[string]any
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached, nil
			}
		}
	}
	listGen := filmsnapshot.GetSearchCacheVersion()
	tree, err := repository.GetActiveCategoryTree(sourceId)
	if err != nil {
		return nil, err
	}
	type homeSlot struct {
		nav    *model.CategoryTree
		movies []int64
		hot    []int64
	}
	const maxHomeSections = 12
	children := tree.Children
	if len(children) > maxHomeSections {
		children = children[:maxHomeSections]
	}
	slots := make([]homeSlot, len(children))
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var pageErr error
	for idx, c := range children {
		wg.Add(1)
		go func(i int, cat *model.CategoryTree) {
			defer wg.Done()
			field, id := categoryHomeField(cat)
			movies, hot, err := filmsnapshot.HomeCategoryMids(version, sourceId, field, id, 14, 50)
			if err != nil {
				errMu.Lock()
				if pageErr == nil {
					pageErr = err
				}
				errMu.Unlock()
				return
			}
			slots[i] = homeSlot{nav: cat, movies: movies, hot: hot}
		}(idx, c)
	}
	wg.Wait()
	if pageErr != nil {
		return nil, pageErr
	}
	allMids := make([]int64, 0)
	for _, slot := range slots {
		allMids = append(allMids, slot.movies...)
		allMids = append(allMids, slot.hot...)
	}
	byID, err := infosByMid(version, allMids)
	if err != nil {
		return nil, err
	}
	filteredList := make([]map[string]any, 0, len(slots))
	for _, slot := range slots {
		if slot.nav == nil {
			continue
		}
		movies := pickInfos(byID, slot.movies)
		if movies == nil {
			movies = make([]model.MovieBasicInfo, 0)
		}
		hot := pickInfos(byID, slot.hot)
		if hot == nil {
			hot = make([]model.MovieBasicInfo, 0)
		}
		filteredList = append(filteredList, map[string]any{
			"nav":    slot.nav,
			"movies": movies,
			"hot":    pickInfos(byID, slot.hot),
		})
	}
	rawBanners, err := repository.GetBanners()
	if err != nil {
		return nil, err
	}
	banners := overlayBannerLiveRemarks(rawBanners)
	if banners == nil {
		banners = make(model.Banners, 0)
	}
	res := map[string]any{
		"category":      tree,
		"content":       filteredList,
		"banners":       banners,
		"currentSource": sourceId,
	}
	if db.Rdb != nil {
		if data, err := json.Marshal(res); err == nil {
			filmsnapshot.StoreCacheIfCurrent(cacheKey, string(data), 10*time.Minute, listGen)
		}
	}
	return res, nil
}

// OverlayBannerLiveRemarks 只把片库更新状态补到轮播。图片和简介用轮播行自己的。
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
		if out[i].TmdbID > 0 {
			out[i].IsScraped = true
		}
	}
	return out
}
