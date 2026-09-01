package service

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/notify"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
)

const (
	homeDailyUpdateLimitMax = 12
	homeDailyUpdateCacheTTL = 5 * time.Minute
)

var dailyUpdateSfGroup singleflight.Group

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
	version := filmrepo.GetActiveReadModelVersion()
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
					movies = filmrepo.GetSnapshotMovieListByCategory(version, "pid", cat.Id, 14, 0)
					hotMovies = filmrepo.GetSnapshotHotMovieListByCategory(version, "pid", cat.Id, 14, 0)
				} else {
					movies = filmrepo.GetSnapshotMovieListByCategory(version, "cid", cat.Id, 14, 0)
					hotMovies = filmrepo.GetSnapshotHotMovieListByCategory(version, "cid", cat.Id, 14, 0)
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
		dynamicMovies := filmrepo.GetSnapshotDynamicHotMovieListByCategory(version, field, catID, 14, 50)
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

// HomeDailyUpdates 近 24h 采集变更（还原 beta.3 行为，使用 120 条候选池短缓存）。
// limit<=0（不传）返回候选池全部内容（最多 120 条）；limit>0 时从池中随机取，exclude 排除当前批次。
func (i *IndexService) HomeDailyUpdates(limit int, exclude []int64) []model.MovieBasicInfo {
	return selectDailyUpdates(i.homeDailyUpdatePool(), limit, exclude)
}

func selectDailyUpdates(pool []model.MovieBasicInfo, limit int, exclude []int64) []model.MovieBasicInfo {
	if len(pool) == 0 {
		return []model.MovieBasicInfo{}
	}
	if limit <= 0 {
		out := make([]model.MovieBasicInfo, len(pool))
		copy(out, pool)
		return out
	}
	if limit > homeDailyUpdateLimitMax {
		limit = homeDailyUpdateLimitMax
	}
	return pickRandomMovieInfos(pool, limit, exclude)
}

const homeDailyUpdatePoolCap = 120

func (i *IndexService) WarmupHomeDailyUpdatePool() {
	_ = i.homeDailyUpdatePool()
}

func (i *IndexService) homeDailyUpdatePool() []model.MovieBasicInfo {
	empty := make([]model.MovieBasicInfo, 0)
	cacheKey := config.IndexDailyUpdatesCacheKey

	// 1. 优先直接读取 Redis 缓存（0 数据库查询，耗时 0.2ms）
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var list []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &list) == nil && len(list) > 0 {
				return list
			}
		}
	}

	// 2. 并发合并防击穿构建
	val, err, _ := dailyUpdateSfGroup.Do("homeDailyUpdatePool", func() (any, error) {
		// Double check 缓存
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var list []model.MovieBasicInfo
				if json.Unmarshal([]byte(data), &list) == nil && len(list) > 0 {
					return list, nil
				}
			}
		}

		version := filmrepo.GetActiveReadModelVersion()
		if version == "" {
			return empty, nil
		}

		from, to := notify.Rolling24hWindow(time.Now())
		items, _ := notify.LoadChangeMidsBetween(from, to, homeDailyUpdatePoolCap)
		mids := make([]int64, 0, homeDailyUpdatePoolCap)
		seen := make(map[int64]struct{}, homeDailyUpdatePoolCap)
		for _, it := range items {
			if it.Mid > 0 {
				if _, ok := seen[it.Mid]; !ok {
					seen[it.Mid] = struct{}{}
					mids = append(mids, it.Mid)
				}
			}
		}

		// 若 24h 变更不足 120 部，从活跃快照按最新时间自动补齐至 120 部，保证候选池永远饱满
		if len(mids) < homeDailyUpdatePoolCap && db.Mdb != nil {
			needed := homeDailyUpdatePoolCap - len(mids)
			var fallbackRows []struct {
				Mid int64
			}
			query := db.Mdb.Model(&model.FilmListSnapshot{}).
				Select("mid").
				Where("snapshot_version = ?", version)
			if len(mids) > 0 {
				query = query.Where("mid NOT IN ?", mids)
			}
			_ = query.Order("update_stamp DESC, id DESC").Limit(needed).Scan(&fallbackRows).Error
			for _, r := range fallbackRows {
				if r.Mid > 0 {
					mids = append(mids, r.Mid)
				}
			}
		}

		if len(mids) == 0 {
			storeHomeDailyUpdatesCache(cacheKey, empty)
			return empty, nil
		}

		snaps := filmrepo.GetProjectedSnapshotsByMidsOrdered(version, mids)
		list := filmrepo.BuildMovieBasicInfosFromSnapshots(snaps...)
		if list == nil {
			list = empty
		}
		storeHomeDailyUpdatesCache(cacheKey, list)
		return list, nil
	})

	if err != nil || val == nil {
		return empty
	}
	resList, ok := val.([]model.MovieBasicInfo)
	if !ok || len(resList) == 0 {
		return empty
	}
	return resList
}

func pickRandomMovieInfos(src []model.MovieBasicInfo, n int, exclude []int64) []model.MovieBasicInfo {
	if n <= 0 || len(src) == 0 {
		return []model.MovieBasicInfo{}
	}
	skip := make(map[int64]struct{}, len(exclude))
	for _, id := range exclude {
		if id > 0 {
			skip[id] = struct{}{}
		}
	}
	pool := src
	if len(skip) > 0 {
		left := make([]model.MovieBasicInfo, 0, len(src))
		for _, item := range src {
			if _, hit := skip[item.Id]; hit {
				continue
			}
			left = append(left, item)
		}
		if len(left) > 0 {
			pool = left
		}
	}
	if len(pool) <= n {
		out := make([]model.MovieBasicInfo, len(pool))
		copy(out, pool)
		return out
	}
	perm := rand.Perm(len(pool))
	out := make([]model.MovieBasicInfo, n)
	for i := 0; i < n; i++ {
		out[i] = pool[perm[i]]
	}
	return out
}

func storeHomeDailyUpdatesCache(cacheKey string, list []model.MovieBasicInfo) {
	if db.Rdb == nil {
		return
	}
	if raw, err := json.Marshal(list); err == nil {
		_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), homeDailyUpdateCacheTTL).Err()
	}
}

// GetFilmDetail 影片详情信息页面处理
func (i *IndexService) GetFilmDetail(id int) (model.MovieDetailVo, error) {
	startedAt := time.Now()
	version := filmrepo.GetActiveReadModelVersion()
	snapshotStartedAt := time.Now()
	snapshot := filmrepo.GetSnapshotByMid(version, int64(id))
	logSlowIndexServiceStep("GetFilmDetail.snapshot", snapshotStartedAt, "id", id)
	if snapshot == nil {
		return model.MovieDetailVo{}, nil
	}
	detailStartedAt := time.Now()
	movieDetail, localUpdateTime := filmrepo.GetMovieDetailBySnapshot(*snapshot)
	logSlowIndexServiceStep("GetFilmDetail.detail", detailStartedAt, "id", id)
	if movieDetail == nil {
		filmrepo.DeleteActiveSnapshotsByMids(snapshot.Mid)
		return model.MovieDetailVo{}, nil
	}
	res := model.MovieDetailVo{MovieDetail: *movieDetail, LocalUpdateTime: localUpdateTime}
	multipleStartedAt := time.Now()
	res.List = multipleSource(snapshot, movieDetail)
	logSlowIndexServiceStep("GetFilmDetail.multipleSource", multipleStartedAt, "id", id)
	logSlowIndexServiceStep("GetFilmDetail.total", startedAt, "id", id)
	return res, nil
}

// GetFilmDetailOnly 读取影片详情主体，不聚合附属站播放源。
func (i *IndexService) GetFilmDetailOnly(id int) (model.MovieDetail, error) {
	startedAt := time.Now()
	version := filmrepo.GetActiveReadModelVersion()
	snapshotStartedAt := time.Now()
	snapshot := filmrepo.GetSnapshotByMid(version, int64(id))
	logSlowIndexServiceStep("GetFilmDetailOnly.snapshot", snapshotStartedAt, "id", id)
	if snapshot == nil {
		return model.MovieDetail{}, nil
	}
	detailStartedAt := time.Now()
	movieDetail, _ := filmrepo.GetMovieDetailBySnapshot(*snapshot)
	logSlowIndexServiceStep("GetFilmDetailOnly.detail", detailStartedAt, "id", id)
	if movieDetail == nil {
		filmrepo.DeleteActiveSnapshotsByMids(snapshot.Mid)
		return model.MovieDetail{}, nil
	}
	logSlowIndexServiceStep("GetFilmDetailOnly.total", startedAt, "id", id)
	return *movieDetail, nil
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

// RelateMovie 根据当前影片快照匹配相关的影片
func (i *IndexService) RelateMovie(mid int64, page *dto.Page) []model.MovieBasicInfo {
	startedAt := time.Now()
	page = normalizeIndexPage(page)
	version := filmrepo.GetActiveReadModelVersion()
	snapshotStartedAt := time.Now()
	snapshot := filmrepo.GetSnapshotByMid(version, mid)
	logSlowIndexServiceStep("RelateMovie.snapshot", snapshotStartedAt, "id", mid)
	if snapshot == nil {
		return []model.MovieBasicInfo{}
	}
	if !filmrepo.HasMovieDetail(snapshot.Mid) {
		filmrepo.DeleteActiveSnapshotsByMids(snapshot.Mid)
		return []model.MovieBasicInfo{}
	}
	listStartedAt := time.Now()
	list := filmrepo.ListRelatedSnapshotsReadModel(version, *snapshot, page)
	logSlowIndexServiceStep("RelateMovie.list", listStartedAt, "id", mid)
	buildStartedAt := time.Now()
	result := filmrepo.BuildMovieBasicInfosFromSnapshots(list...)
	logSlowIndexServiceStep("RelateMovie.build", buildStartedAt, "id", mid)
	logSlowIndexServiceStep("RelateMovie.total", startedAt, "id", mid)
	return result
}

// SearchTags 整合对应分类的搜索tag
func (i *IndexService) SearchTags(st model.SearchTagsVO) map[string]any {
	return filmrepo.GetFilterOptionSnapshot(filmrepo.GetActiveReadModelVersion(), st.Pid)
}

func multipleSource(snapshot *model.FilmListSnapshot, detail *model.MovieDetail) []model.PlayLinkVo {
	startedAt := time.Now()
	primaryStartedAt := time.Now()
	playList := buildPrimaryPlaySources(snapshot, detail)
	logSlowIndexServiceStep("multipleSource.primary", primaryStartedAt, "id", snapshot.Mid)
	keysStartedAt := time.Now()
	names := filmrepo.LoadMovieMatchKeysBySnapshot(snapshot, detail)
	logSlowIndexServiceStep("multipleSource.matchKeys", keysStartedAt, "id", snapshot.Mid)
	if len(names) == 0 {
		return playList
	}

	sourcesStartedAt := time.Now()
	slaveSources := repository.GetCollectSourceListByGrade(model.SlaveCollect)
	logSlowIndexServiceStep("multipleSource.sources", sourcesStartedAt, "id", snapshot.Mid)
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

	groupsStartedAt := time.Now()
	groupsBySource := filmrepo.GetMultiplePlayGroupsBySourcesAndKeys(querySources, names)
	logSlowIndexServiceStep("multipleSource.playlists", groupsStartedAt, "id", snapshot.Mid, "sources", len(querySources), "keys", len(names))
	for _, source := range querySources {
		groups := groupsBySource[source.Id]
		if len(groups) > 0 {
			playList = append(playList, groups...)
		}
	}

	logSlowIndexServiceStep("multipleSource.total", startedAt, "id", snapshot.Mid, "sources", len(querySources), "keys", len(names))
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
