package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmquery "server/internal/repository/film/query"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"
	"server/internal/utils"
	"golang.org/x/sync/singleflight"
)

var provideSnapshotsSf singleflight.Group

func ListProvideSnapshotsReadModel(version string, st model.SearchTagsVO, keyword string, recentHours int, page *dto.Page) ([]model.FilmListSnapshot, error) {
	startedAt := time.Now()
	page = shared.EnsurePage(page)
	st = shared.NormalizeSearchTagsVO(st)
	st.Sort = utils.NormalizeSearchSortField(st.Sort)
	keyword = strings.TrimSpace(keyword)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" {
		return nil, ErrSearchIndexMissing
	}

	if len([]rune(keyword)) > 64 || strings.HasPrefix(keyword, "http://") || strings.HasPrefix(keyword, "https://") {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}, nil
	}

	if st.Cid > 0 && !support.IsCategoryVisible(st.Cid, st.Pid) {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}, nil
	}
	if st.Pid > 0 && !support.IsCategoryVisible(0, st.Pid) {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}, nil
	}
	keywordOnly := keyword != "" && recentHours == 0 && st.Plot == "" && st.Area == "" && st.Language == "" && st.Year == ""
	if keywordOnly {
		snaps, err := memoryKeywordSnapshots(version, st.SourceId, keyword, st.Sort, st.Pid, st.Cid, page)
		if err != nil {
			return nil, err
		}
		log.Printf("[ProvideVod] 内存检索完成 pid=%d cid=%d keyword=%q total=%d page=%d size=%d cost=%s",
			st.Pid, st.Cid, keyword, page.Total, page.Current, len(snaps), time.Since(startedAt))
		return snaps, nil
	}

	// 1. 尝试从 Redis 读 Provide 缓存
	var cacheKey, sfKey string
	if st.SourceId != "" {
		cacheKey = fmt.Sprintf("%s:v%s:src_%s:%d:%d:%s:%s:%s:%s:%s:k%s:h%d:p%d:s%d",
			config.ProvideListKey, version, st.SourceId, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, keyword, recentHours, page.Current, page.PageSize)
		sfKey = fmt.Sprintf("v%s:src_%s:%d:%d:%s:%s:%s:%s:%s:k%s:h%d:p%d:s%d",
			version, st.SourceId, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, keyword, recentHours, page.Current, page.PageSize)
	} else {
		cacheKey = fmt.Sprintf("%s:v%s:%d:%d:%s:%s:%s:%s:%s:k%s:h%d:p%d:s%d",
			config.ProvideListKey, version, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, keyword, recentHours, page.Current, page.PageSize)
		sfKey = fmt.Sprintf("v%s:%d:%d:%s:%s:%s:%s:%s:k%s:h%d:p%d:s%d",
			version, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, keyword, recentHours, page.Current, page.PageSize)
	}
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var item searchCacheItem
			if json.Unmarshal([]byte(data), &item) == nil {
				page.Total = item.Total
				page.PageCount = item.PageCount
				log.Printf(
					"[ProvideVod] 命中缓存 pid=%d cid=%d keyword=%q total=%d page=%d size=%d cost=%s",
					st.Pid, st.Cid, keyword, page.Total, page.Current, len(item.Snapshots), time.Since(startedAt),
				)
				return item.Snapshots, nil
			}
		}
	}

	// 2. 并发防击穿：相同参数的 ProvideVod 请求合并执行
	val, err, _ := provideSnapshotsSf.Do(sfKey, func() (any, error) {
		// 二次双检 Redis 缓存
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var item searchCacheItem
				if json.Unmarshal([]byte(data), &item) == nil {
					return item, nil
				}
			}
		}

		listGen := GetSearchCacheVersion()
		if db.Mdb == nil {
			return nil, ErrSearchIndexMissing
		}

		query := applySourceMembership(liveFilmQuery(), st.SourceId)
		if disabledCatIDs := support.GetDisabledCategoryIDs(); len(disabledCatIDs) > 0 {
			query = query.Where("cid NOT IN ? AND pid NOT IN ?", disabledCatIDs, disabledCatIDs)
		}
		if st.SourceId != "" {
			if st.Cid > 0 {
				query = filmquery.ApplySourceTypeMatchIDs(query, st.SourceId, "cid", repository.PublicSourceTypeIDs(st.SourceId, "cid", st.Cid))
			} else if st.Pid > 0 {
				query = filmquery.ApplySourceTypeMatchIDs(query, st.SourceId, "pid", repository.PublicSourceTypeIDs(st.SourceId, "pid", st.Pid))
			}
		} else if st.Cid > 0 {
			query = filmquery.ApplyLiveCategoryMatch(query, "cid", st.Cid)
		} else if st.Pid > 0 {
			query = filmquery.ApplyLiveCategoryMatch(query, "pid", st.Pid)
		}
		query, err := applyTagSearchFilter(query, version, st)
		if err != nil {
			return nil, err
		}
		if keyword != "" {
			query = applyNameLikeFilter(query, keyword)
		}
		if recentHours > 0 {
			timeLimit := time.Now().Add(-time.Duration(recentHours) * time.Hour).Unix()
			query = query.Where("update_stamp >= ?", timeLimit)
		}

		var total int64
		if err := query.Count(&total).Error; err != nil {
			return nil, err
		}
		calcTotal := int(total)
		calcPageCount := (calcTotal + page.PageSize - 1) / page.PageSize
		if calcPageCount <= 0 {
			calcPageCount = 1
		}

		orderClause := liveTieOrder(snapshotSortOrderClause(st.Sort, keyword != ""))
		offset := shared.PageOffset(page)

		snapshots, findErr := findListPage(query, orderClause, offset, page.PageSize)
		if findErr != nil {
			return nil, findErr
		}
		filteredSnapshots := make([]model.FilmListSnapshot, 0, len(snapshots))
		for _, s := range snapshots {
			if support.IsCategoryVisible(s.Cid, s.Pid) {
				filteredSnapshots = append(filteredSnapshots, s)
			}
		}
		snapshots = filteredSnapshots
		item := searchCacheItem{
			Total:     calcTotal,
			PageCount: calcPageCount,
			Snapshots: snapshots,
		}

		if raw, err := json.Marshal(item); err == nil {
			ttl := 3 * time.Minute
			if len(snapshots) == 0 {
				ttl = emptyListCacheTTL
			}
			writeListCache(cacheKey, raw, ttl, listGen)
		}

		log.Printf(
			"[ProvideVod] 筛选完成 pid=%d cid=%d keyword=%q total=%d page=%d size=%d cost=%s",
			st.Pid,
			st.Cid,
			keyword,
			calcTotal,
			page.Current,
			len(snapshots),
			time.Since(startedAt),
		)
		return item, nil
	})

	if err != nil {
		return nil, err
	}
	item, ok := val.(searchCacheItem)
	if !ok {
		return nil, errors.New("检索索引损坏")
	}
	page.Total = item.Total
	page.PageCount = item.PageCount
	return item.Snapshots, nil
}
