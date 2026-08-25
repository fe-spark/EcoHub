package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
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
		info["content"] = list
		banners := repository.GetBanners()
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
		return map[string]any{
			"category": repository.GetActiveCategoryTree(),
			"content":  []map[string]any{},
			"banners":  repository.GetBanners(),
		}
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
	return outInfo
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
	live := filmrepo.LiveUpdateRemarksByMIDs(mids)
	if len(live) == 0 {
		return banners
	}
	out := make(model.Banners, len(banners))
	copy(out, banners)
	for i := range out {
		if remark, ok := live[out[i].Mid]; ok {
			out[i].Remark = remark
		}
	}
	return out
}

// DailyUpdateListReq 新每日更新请求参数
type DailyUpdateListReq struct {
	Page    *dto.Page
	Random  bool
	Exclude []int64
}

// DailyUpdatesPaged 新每日更新接口：支持标准分页、随机选项、分批加载
func (i *IndexService) DailyUpdatesPaged(req DailyUpdateListReq) ([]model.MovieBasicInfo, *dto.Page, error) {
	page := filmrepo.EnsurePage(req.Page)
	if page.PageSize > 100 {
		page.PageSize = 100
	}

	from, to := notify.Rolling24hWindow(time.Now())

	// 若开启随机
	if req.Random {
		pool := i.homeDailyUpdatePool()
		if len(pool) == 0 {
			page.Total = 0
			page.PageCount = 1
			return []model.MovieBasicInfo{}, page, nil
		}
		page.Total = len(pool)
		page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
		if page.PageCount <= 0 {
			page.PageCount = 1
		}
		list := pickRandomMovieInfos(pool, page.PageSize, req.Exclude)
		return list, page, nil
	}

	// 正常按时间倒序分页
	items, total, err := notify.LoadChangeMidsBetweenPaged(from, to, page.Current, page.PageSize)
	if err != nil {
		log.Printf("[IndexService] DailyUpdatesPaged load mids: %v", err)
		return []model.MovieBasicInfo{}, page, err
	}
	page.Total = total
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	if len(items) == 0 {
		return []model.MovieBasicInfo{}, page, nil
	}

	version := filmrepo.GetActiveReadModelVersion()
	mids := make([]int64, 0, len(items))
	for _, it := range items {
		mids = append(mids, it.Mid)
	}
	snaps := filmrepo.GetProjectedSnapshotsByMidsOrdered(version, mids)
	list := filmrepo.BuildMovieBasicInfosFromSnapshots(snaps...)
	if list == nil {
		list = make([]model.MovieBasicInfo, 0)
	}
	applyLiveRemarksToMovies(list)
	return list, page, nil
}

// StreamDailyUpdates 流式分批查询近24h每日更新，通过回调 chunkFn 逐批发送数据，避免大批量时在内存积压。
// 接收 ctx 参数，在客户端断开或请求超时时能及时中止后续批次查询。
func (i *IndexService) StreamDailyUpdates(ctx context.Context, batchSize int, chunkFn func(batch []model.MovieBasicInfo, currentBatch int, totalCount int) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if batchSize <= 0 {
		batchSize = 50
	} else if batchSize > 200 {
		batchSize = 200
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	from, to := notify.Rolling24hWindow(time.Now())
	firstItems, total, err := notify.LoadChangeMidsBetweenPaged(from, to, 1, batchSize)
	if err != nil {
		return err
	}
	if total == 0 || len(firstItems) == 0 {
		return nil
	}

	version := filmrepo.GetActiveReadModelVersion()

	// 处理第 1 批
	mids := make([]int64, 0, len(firstItems))
	for _, it := range firstItems {
		mids = append(mids, it.Mid)
	}
	snaps := filmrepo.GetProjectedSnapshotsByMidsOrdered(version, mids)
	list := filmrepo.BuildMovieBasicInfosFromSnapshots(snaps...)
	applyLiveRemarksToMovies(list)
	if err := chunkFn(list, 1, total); err != nil {
		return err
	}

	pageCount := (total + batchSize - 1) / batchSize
	for p := 2; p <= pageCount; p++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, _, err := notify.LoadChangeMidsBetweenPaged(from, to, p, batchSize)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			break
		}
		mids = mids[:0]
		for _, it := range items {
			mids = append(mids, it.Mid)
		}
		snaps = filmrepo.GetProjectedSnapshotsByMidsOrdered(version, mids)
		batchList := filmrepo.BuildMovieBasicInfosFromSnapshots(snaps...)
		applyLiveRemarksToMovies(batchList)
		if err := chunkFn(batchList, p, total); err != nil {
			return err
		}
	}
	return nil
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
