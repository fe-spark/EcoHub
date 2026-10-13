package service

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sync/singleflight"
	"log"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/repository/film"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
)

const (
	dailyUpdateDefaultPageSize = 21
	dailyUpdateMaxPageSize     = 100
	dailyUpdateMaxExclude      = 500
)

var dailyUpdateSfGroup singleflight.Group

// DailyUpdateListReq 每日更新列表请求：分类 + 标准分页 + 随机。
type DailyUpdateListReq struct {
	Pid      int64
	Page     *dto.Page
	Random   bool
	Exclude  []int64
	SourceId string
}

// DailyUpdateCategory 近 24h 分类统计。Pid: 0 全部，-1 其他，>0 导航大类。
type DailyUpdateCategory struct {
	Pid   int64  `json:"pid"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// DailyUpdateResult 每日更新响应。
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
		req.Exclude = film.ClampDailyUpdateExclude(req.Exclude, dailyUpdateMaxExclude)
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
	out = append(out, DailyUpdateCategory{Pid: film.DailyPidAll, Name: "全部", Count: total})
	for _, n := range nav {
		if n.Id <= 0 {
			continue
		}
		if c := countByPid[n.Id]; c > 0 {
			out = append(out, DailyUpdateCategory{Pid: n.Id, Name: n.Name, Count: c})
		}
	}
	if otherCount > 0 {
		out = append(out, DailyUpdateCategory{Pid: film.DailyPidOther, Name: "其他", Count: otherCount})
	}
	return out
}

// DailyUpdates 近 24h 更新列表与分类聚合。
func (i *IndexService) DailyUpdates(req DailyUpdateListReq) (*DailyUpdateResult, error) {
	req = normalizeDailyUpdateReq(req)
	if req.SourceId == "" {
		if active := repository.GetActiveCollectSource(); active != nil {
			req.SourceId = active.Id
		}
	}
	from, to := film.Rolling24hWindow(time.Now())

	// 分类树每请求只取一次，复用给列表筛选、分类计数与组装，避免多次全表扫描。
	nav := film.NavTopCategories(req.SourceId)
	navIDs := film.NavTopCategoryIDs(nav)

	// 非随机且前 5 页支持短缓存（1 分钟）与 Singleflight，避免全量采集后高频刷新冲击数据库
	usePageCache := !req.Random && req.Page.Current <= 5
	var pageCacheKey string
	if req.SourceId != "" {
		pageCacheKey = fmt.Sprintf("%s:src_%s:p%d:c%d:s%d", config.DailyUpdatesCachePrefix, req.SourceId, req.Pid, req.Page.Current, req.Page.PageSize)
	} else {
		pageCacheKey = fmt.Sprintf("%s:p%d:c%d:s%d", config.DailyUpdatesCachePrefix, req.Pid, req.Page.Current, req.Page.PageSize)
	}

	if usePageCache && db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, pageCacheKey).Result(); err == nil && data != "" {
			var cachedRes DailyUpdateResult
			if json.Unmarshal([]byte(data), &cachedRes) == nil && len(cachedRes.List) > 0 {
				cachedRes.Categories = i.getDailyUpdateCategories(nav, navIDs, from, to, cachedRes.Page.Total, req.SourceId)
				applyLiveRemarksToMovies(cachedRes.List)
				return &cachedRes, nil
			}
		}
	}

	execQuery := func() (*DailyUpdateResult, error) {
		mids, total, err := film.ListDailyUpdateMids(film.DailyUpdateListQuery{
			From:     from,
			To:       to,
			Pid:      req.Pid,
			Current:  req.Page.Current,
			PageSize: req.Page.PageSize,
			Random:   req.Random,
			Exclude:  req.Exclude,
			NavIDs:   navIDs,
			SourceId: req.SourceId,
		})
		if err != nil {
			log.Printf("[IndexService] DailyUpdates list mids: %v", err)
			return nil, err
		}

		page := fillDailyUpdatePage(req.Page, total)
		list := hydrateDailyUpdateMids(mids)
		if list == nil {
			list = []model.MovieBasicInfo{}
		}

		cats := i.getDailyUpdateCategories(nav, navIDs, from, to, total, req.SourceId)
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

func (i *IndexService) getDailyUpdateCategories(nav []model.Category, navIDs []int64, from, to time.Time, fallbackTotal int, sourceID string) []DailyUpdateCategory {
	cacheKey := config.DailyUpdatesCatCacheKey
	if strings.TrimSpace(sourceID) != "" {
		cacheKey = cacheKey + ":src_" + sourceID
	}
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cats []DailyUpdateCategory
			if json.Unmarshal([]byte(data), &cats) == nil && len(cats) > 0 {
				return cats
			}
		}
	}

	val, err, _ := dailyUpdateSfGroup.Do("DailyUpdateCategories:"+sourceID, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var cats []DailyUpdateCategory
				if json.Unmarshal([]byte(data), &cats) == nil && len(cats) > 0 {
					return cats, nil
				}
			}
		}

		countByPid, otherCount, catTotal, catErr := film.DailyUpdatePidCounts(from, to, navIDs, sourceID)
		if catErr != nil {
			log.Printf("[IndexService] DailyUpdates category counts: %v", catErr)
			return []DailyUpdateCategory{{Pid: film.DailyPidAll, Name: "全部", Count: fallbackTotal}}, nil
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
	return []DailyUpdateCategory{{Pid: film.DailyPidAll, Name: "全部", Count: fallbackTotal}}
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
