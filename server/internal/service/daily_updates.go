package service

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sync/singleflight"
	"log"
	"math/rand"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/notify"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
)

const (
	dailyUpdateDefaultPageSize = 21
	dailyUpdateMaxPageSize     = 100
	dailyUpdateMaxExclude      = 500
	homeDailyUpdateLimitMax    = 12
	homeDailyUpdateCacheTTL    = 5 * time.Minute
	homeDailyUpdatePoolCap     = 120
)

var dailyUpdateSfGroup singleflight.Group

// DailyUpdateListReq V2 每日更新：分类 + 标准分页 + 随机。
type DailyUpdateListReq struct {
	Pid     int64
	Page    *dto.Page
	Random  bool
	Exclude []int64
}

// DailyUpdateCategory 近 24h 分类统计。Pid: 0 全部，-1 其他，>0 导航大类。
type DailyUpdateCategory struct {
	Pid   int64  `json:"pid"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// DailyUpdateResult V2 响应。
type DailyUpdateResult struct {
	List       []model.MovieBasicInfo `json:"list"`
	Page       *dto.Page              `json:"page"`
	Categories []DailyUpdateCategory  `json:"categories"`
}

func normalizeDailyUpdateReq(req DailyUpdateListReq) DailyUpdateListReq {
	page := req.Page
	if page == nil {
		page = &dto.Page{}
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	if page.PageSize <= 0 {
		page.PageSize = dailyUpdateDefaultPageSize
	}
	if page.PageSize > dailyUpdateMaxPageSize {
		page.PageSize = dailyUpdateMaxPageSize
	}
	req.Page = page
	if !req.Random {
		req.Exclude = nil
	} else {
		req.Exclude = notify.ClampDailyUpdateExclude(req.Exclude, dailyUpdateMaxExclude)
	}
	return req
}

func fillDailyUpdatePage(page *dto.Page, total int) *dto.Page {
	page.Total = total
	if page.PageSize <= 0 {
		page.PageSize = dailyUpdateDefaultPageSize
	}
	page.PageCount = (total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	return page
}

// AssembleDailyUpdateCategories 导航顺序输出有片的大类；全部永远第一项。
func AssembleDailyUpdateCategories(nav []model.Category, countByPid map[int64]int, otherCount, total int) []DailyUpdateCategory {
	out := make([]DailyUpdateCategory, 0, len(nav)+2)
	out = append(out, DailyUpdateCategory{Pid: notify.DailyPidAll, Name: "全部", Count: total})
	for _, n := range nav {
		if n.Id <= 0 {
			continue
		}
		if c := countByPid[n.Id]; c > 0 {
			out = append(out, DailyUpdateCategory{Pid: n.Id, Name: n.Name, Count: c})
		}
	}
	if otherCount > 0 {
		out = append(out, DailyUpdateCategory{Pid: notify.DailyPidOther, Name: "其他", Count: otherCount})
	}
	return out
}

// DailyUpdatesV2 近 24h 更新。破坏性契约：无流式、不走首页 120 池。
func (i *IndexService) DailyUpdatesV2(req DailyUpdateListReq) (*DailyUpdateResult, error) {
	req = normalizeDailyUpdateReq(req)
	from, to := notify.Rolling24hWindow(time.Now())

	// 分类树每请求只取一次，复用给列表筛选、分类计数与组装，避免多次全表扫描。
	nav := notify.NavTopCategories()
	navIDs := notify.NavTopCategoryIDs(nav)

	// 非随机且前 5 页支持短缓存（1 分钟）与 Singleflight，避免全量采集后高频刷新冲击数据库
	usePageCache := !req.Random && req.Page.Current <= 5
	pageCacheKey := fmt.Sprintf("%s:p%d:c%d:s%d", config.DailyUpdatesV2CachePrefix, req.Pid, req.Page.Current, req.Page.PageSize)

	if usePageCache && db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, pageCacheKey).Result(); err == nil && data != "" {
			var cachedRes DailyUpdateResult
			if json.Unmarshal([]byte(data), &cachedRes) == nil && len(cachedRes.List) > 0 {
				cachedRes.Categories = i.getDailyUpdateCategories(nav, navIDs, from, to, cachedRes.Page.Total)
				applyLiveRemarksToMovies(cachedRes.List)
				return &cachedRes, nil
			}
		}
	}

	execQuery := func() (*DailyUpdateResult, error) {
		mids, total, err := notify.ListDailyUpdateMids(notify.DailyUpdateListQuery{
			From:     from,
			To:       to,
			Pid:      req.Pid,
			Current:  req.Page.Current,
			PageSize: req.Page.PageSize,
			Random:   req.Random,
			Exclude:  req.Exclude,
			NavIDs:   navIDs,
		})
		if err != nil {
			log.Printf("[IndexService] DailyUpdatesV2 list mids: %v", err)
			return nil, err
		}

		page := fillDailyUpdatePage(req.Page, total)
		list := hydrateDailyUpdateMids(mids)
		if list == nil {
			list = []model.MovieBasicInfo{}
		}

		cats := i.getDailyUpdateCategories(nav, navIDs, from, to, total)
		res := &DailyUpdateResult{List: list, Page: page, Categories: cats}

		if usePageCache && db.Rdb != nil && len(list) > 0 {
			if data, err := json.Marshal(res); err == nil {
				_ = db.Rdb.Set(db.Cxt, pageCacheKey, string(data), 1*time.Minute).Err()
			}
		}
		return res, nil
	}

	if usePageCache {
		val, err, _ := dailyUpdateSfGroup.Do(pageCacheKey, func() (any, error) {
			return execQuery()
		})
		if err != nil {
			return nil, err
		}
		if res, ok := val.(*DailyUpdateResult); ok {
			return res, nil
		}
	}

	return execQuery()
}

func (i *IndexService) getDailyUpdateCategories(nav []model.Category, navIDs []int64, from, to time.Time, fallbackTotal int) []DailyUpdateCategory {
	cacheKey := config.DailyUpdatesV2CatCacheKey
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cats []DailyUpdateCategory
			if json.Unmarshal([]byte(data), &cats) == nil && len(cats) > 0 {
				return cats
			}
		}
	}

	val, err, _ := dailyUpdateSfGroup.Do("DailyUpdateCategories", func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var cats []DailyUpdateCategory
				if json.Unmarshal([]byte(data), &cats) == nil && len(cats) > 0 {
					return cats, nil
				}
			}
		}

		countByPid, otherCount, catTotal, catErr := notify.DailyUpdatePidCounts(from, to, navIDs)
		if catErr != nil {
			log.Printf("[IndexService] DailyUpdatesV2 category counts: %v", catErr)
			return []DailyUpdateCategory{{Pid: notify.DailyPidAll, Name: "全部", Count: fallbackTotal}}, nil
		}
		cats := AssembleDailyUpdateCategories(nav, countByPid, otherCount, catTotal)
		if db.Rdb != nil && len(cats) > 0 {
			if data, err := json.Marshal(cats); err == nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, string(data), 2*time.Minute).Err()
			}
		}
		return cats, nil
	})

	if err == nil && val != nil {
		if cats, ok := val.([]DailyUpdateCategory); ok {
			return cats
		}
	}
	return []DailyUpdateCategory{{Pid: notify.DailyPidAll, Name: "全部", Count: fallbackTotal}}
}

func hydrateDailyUpdateMids(mids []int64) []model.MovieBasicInfo {
	if len(mids) == 0 {
		return []model.MovieBasicInfo{}
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	snaps := filmsnapshot.GetProjectedSnapshotsByMidsOrdered(version, mids)
	list := filmshared.BuildMovieBasicInfosFromSnapshots(snaps...)
	if list == nil {
		return []model.MovieBasicInfo{}
	}
	applyLiveRemarksToMovies(list)
	return list
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

		version := filmsnapshot.GetActiveReadModelVersion()
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

		snaps := filmsnapshot.GetProjectedSnapshotsByMidsOrdered(version, mids)
		list := filmshared.BuildMovieBasicInfosFromSnapshots(snaps...)
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
