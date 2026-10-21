package service

import (
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
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

func categoryHomeMovies(version, sourceId string, cat *model.CategoryTree) (movies, hot []model.MovieBasicInfo, err error) {
	field := "cid"
	if cat != nil && len(cat.Children) > 0 {
		field = "pid"
	}
	id := int64(0)
	if cat != nil {
		id = cat.Id
	}
	movies, err = filmsnapshot.GetSnapshotDynamicHotMovieListByCategoryWithSource(version, sourceId, field, id, 14, 50)
	if err != nil {
		return nil, nil, err
	}
	hot, err = filmsnapshot.GetSnapshotHotMovieListByCategoryWithSource(version, sourceId, field, id, 14, 0)
	if err != nil {
		return nil, nil, err
	}
	if movies == nil {
		movies = []model.MovieBasicInfo{}
	}
	if hot == nil {
		hot = []model.MovieBasicInfo{}
	}
	return movies, hot, nil
}

// IndexPage 首页数据处理。推荐片从热度池抽样，热播按热度排序，每次现算。
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
	tree, err := repository.GetActiveCategoryTree(sourceId)
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, len(tree.Children))
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var pageErr error
	for idx, c := range tree.Children {
		wg.Add(1)
		go func(i int, cat *model.CategoryTree) {
			defer wg.Done()
			movies, hot, err := categoryHomeMovies(version, sourceId, cat)
			if err != nil {
				errMu.Lock()
				if pageErr == nil {
					pageErr = err
				}
				errMu.Unlock()
				return
			}
			list[i] = map[string]any{"nav": cat, "movies": movies, "hot": hot}
		}(idx, c)
	}
	wg.Wait()
	if pageErr != nil {
		return nil, pageErr
	}
	filteredList := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if item == nil {
			continue
		}
		movies, _ := item["movies"].([]model.MovieBasicInfo)
		if len(movies) == 0 {
			continue
		}
		filteredList = append(filteredList, item)
	}
	rawBanners, err := repository.GetBanners()
	if err != nil {
		return nil, err
	}
	banners := overlayBannerLiveRemarks(rawBanners)
	if banners == nil {
		banners = make(model.Banners, 0)
	}
	return map[string]any{
		"category":      tree,
		"content":       filteredList,
		"banners":       banners,
		"currentSource": sourceId,
	}, nil
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
