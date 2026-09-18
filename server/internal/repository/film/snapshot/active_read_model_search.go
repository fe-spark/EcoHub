package snapshot

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/film/shared"
	"server/internal/utils"
	"strconv"

	"golang.org/x/sync/singleflight"
)

const (
	tagSearchCacheTTL    = 2 * time.Hour
	snapshotSelectFields = "id, snapshot_version, mid, pid, cid, c_name, name, score, hits, update_stamp, remarks, state, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture, year, class_tag, area, language"
)

type tagSearchCacheItem struct {
	Total     int                      `json:"total"`
	PageCount int                      `json:"page_count"`
	Snapshots []model.FilmListSnapshot `json:"snapshots"`
}

var tagSearchSfGroup singleflight.Group

func ListFilmSnapshotsByTagsReadModel(version string, st model.SearchTagsVO, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = shared.EnsurePage(page)
	st = shared.NormalizeSearchTagsVO(st)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}

	cacheKey := fmt.Sprintf("%s:v%s:%d:%d:%s:%s:%s:%s:%s:p%d:s%d",
		config.FilmSearchTagsKey, version, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, page.Current, page.PageSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var item tagSearchCacheItem
			if json.Unmarshal([]byte(data), &item) == nil {
				page.Total = item.Total
				page.PageCount = item.PageCount
				log.Printf(
					"[FilmClassifySearch] 命中缓存 pid=%d cid=%d plot=%q area=%q language=%q year=%q sort=%q total=%d page=%d size=%d cost=%s",
					st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, page.Total, page.Current, len(item.Snapshots), time.Since(startedAt),
				)
				return item.Snapshots
			}
		}
	}

	val, err, _ := tagSearchSfGroup.Do(cacheKey, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var item tagSearchCacheItem
				if json.Unmarshal([]byte(data), &item) == nil {
					return item, nil
				}
			}
		}

		query := db.Mdb.Model(&model.FilmListSnapshot{}).Unscoped().Where("snapshot_version = ?", version)
		if st.Pid > 0 {
			query = query.Where("pid = ?", st.Pid)
		}
		if st.Cid > 0 {
			query = query.Where("cid = ?", st.Cid)
		}
		query = applyTagSearchFilter(query, version, st)

		var total int64 = -1
		countKey := fmt.Sprintf("%s:count:v%s:%d:%d:%s:%s:%s:%s",
			config.FilmSearchTagsKey, version, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year)
		if db.Rdb != nil {
			if countStr, err := db.Rdb.Get(db.Cxt, countKey).Result(); err == nil && countStr != "" {
				if parsedTotal, err := strconv.ParseInt(countStr, 10, 64); err == nil && parsedTotal >= 0 {
					total = parsedTotal
				}
			}
		}

		if total < 0 {
			if err := query.Count(&total).Error; err != nil {
				return tagSearchCacheItem{}, err
			}
			if db.Rdb != nil {
				_ = db.Rdb.Set(db.Cxt, countKey, strconv.FormatInt(total, 10), tagSearchCacheTTL).Err()
			}
		}

		calcTotal := int(total)
		calcPageCount := (calcTotal + page.PageSize - 1) / page.PageSize
		if calcPageCount <= 0 {
			calcPageCount = 1
		}

		orderClause := "update_stamp DESC, id DESC"
		switch st.Sort {
		case "hits":
			orderClause = "hits DESC, id DESC"
		case "score":
			orderClause = "score DESC, id DESC"
		case "year":
			orderClause = "year DESC, update_stamp DESC, id DESC"
		}

		offset := shared.PageOffset(page)
		var ids []uint
		if err := query.Select("id").Order(orderClause).Offset(offset).Limit(page.PageSize).Pluck("id", &ids).Error; err != nil {
			return tagSearchCacheItem{}, err
		}

		var snapshots []model.FilmListSnapshot
		if len(ids) > 0 {
			if err := db.Mdb.Model(&model.FilmListSnapshot{}).Unscoped().Select(snapshotSelectFields).Where("id IN ?", ids).Order(orderClause).Find(&snapshots).Error; err != nil {
				return tagSearchCacheItem{}, err
			}
		}
		if snapshots == nil {
			snapshots = []model.FilmListSnapshot{}
		}

		item := tagSearchCacheItem{
			Total:     calcTotal,
			PageCount: calcPageCount,
			Snapshots: snapshots,
		}

		if db.Rdb != nil {
			ttl := tagSearchCacheTTL
			if len(snapshots) == 0 {
				ttl = 60 * time.Second
			}
			if raw, err := json.Marshal(item); err == nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), ttl).Err()
			}
		}
		return item, nil
	})

	if err != nil || val == nil {
		return []model.FilmListSnapshot{}
	}
	item, ok := val.(tagSearchCacheItem)
	if !ok {
		return []model.FilmListSnapshot{}
	}
	page.Total = item.Total
	page.PageCount = item.PageCount

	res := make([]model.FilmListSnapshot, len(item.Snapshots))
	copy(res, item.Snapshots)

	log.Printf(
		"[FilmClassifySearch] 筛选完成 pid=%d cid=%d plot=%q area=%q language=%q year=%q sort=%q total=%d page=%d size=%d cost=%s",
		st.Pid,
		st.Cid,
		st.Plot,
		st.Area,
		st.Language,
		st.Year,
		st.Sort,
		page.Total,
		page.Current,
		len(res),
		time.Since(startedAt),
	)
	return res
}

type searchCacheItem struct {
	Total     int                      `json:"total"`
	PageCount int                      `json:"page_count"`
	Snapshots []model.FilmListSnapshot `json:"snapshots"`
}

func cloneFilmListSnapshots(src []model.FilmListSnapshot) []model.FilmListSnapshot {
	if src == nil {
		return nil
	}
	out := make([]model.FilmListSnapshot, len(src))
	copy(out, src)
	return out
}

var searchSnapshotsSf singleflight.Group

func SearchSnapshotsByKeywordAndSortReadModel(version string, keyword string, sortField string, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = shared.EnsurePage(page)
	keyword = strings.TrimSpace(keyword)
	sortField = utils.NormalizeSearchSortField(sortField)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" || keyword == "" {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}

	// 快速过滤非正常片名（例如 URL 或长度过长字符串），避免无意义全表扫描
	if len([]rune(keyword)) > 64 || strings.HasPrefix(keyword, "http://") || strings.HasPrefix(keyword, "https://") {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}

	// 1. 尝试从 Redis 读搜索缓存
	searchVer := GetSearchCacheVersion()
	cacheKey := fmt.Sprintf("%s:v%s:sv%s:%s:%s:p%d:s%d", config.FilmSearchCachePrefix, version, searchVer, keyword, sortField, page.Current, page.PageSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var item searchCacheItem
			if json.Unmarshal([]byte(data), &item) == nil {
				page.Total = item.Total
				page.PageCount = item.PageCount
				log.Printf("[SearchFilm] 搜索命中缓存 keyword=%q sort=%q cache=HIT total=%d page=%d size=%d cost=%s",
					keyword, sortField, item.Total, page.Current, len(item.Snapshots), time.Since(startedAt))
				return item.Snapshots
			}
		}
	}

	// 2. 并发防击穿：相同关键词搜索合并执行
	sfKey := fmt.Sprintf("v%s:sv%s:%s:%s:p%d:s%d", version, searchVer, keyword, sortField, page.Current, page.PageSize)
	val, err, _ := searchSnapshotsSf.Do(sfKey, func() (any, error) {
		// 二次双检 Redis 缓存
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var item searchCacheItem
				if json.Unmarshal([]byte(data), &item) == nil {
					return item, nil
				}
			}
		}

		// A. 优先内存元数据检索
		idx := loadFilmSearchMetaIndex(version)
		if idx != nil && len(idx.Items) > 0 {
			hits := searchFilmMetas(idx, keyword, sortField, 0, 0)
			pageMids := pageMidsFromMetaHits(hits, page)
			var snapshots []model.FilmListSnapshot
			if len(pageMids) > 0 {
				snapshots = GetProjectedSnapshotsByMidsOrdered(version, pageMids)
			}
			if snapshots == nil {
				snapshots = []model.FilmListSnapshot{}
			}
			item := searchCacheItem{
				Total:     page.Total,
				PageCount: page.PageCount,
				Snapshots: snapshots,
			}
			if db.Rdb != nil {
				// 仅在当前检索版本与全局版本一致时回写缓存，防止已过期的旧检索结果污染新版本
				if GetSearchCacheVersion() == searchVer {
					if raw, err := json.Marshal(item); err == nil {
						ttl := 3 * time.Minute
						if len(snapshots) == 0 {
							ttl = 1 * time.Minute
						}
						_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), ttl).Err()
					}
				}
			}
			log.Printf("[SearchFilm] 内存检索完成 keyword=%q sort=%q cache=MISS(MEMORY_HIT) total=%d page=%d size=%d cost=%s",
				keyword, sortField, page.Total, page.Current, len(snapshots), time.Since(startedAt))
			return item, nil
		}

		if db.Mdb == nil {
			return searchCacheItem{Total: 0, PageCount: 1, Snapshots: []model.FilmListSnapshot{}}, nil
		}

		// B. 数据库兜底查询（采用延迟关联避免全字段参与 filesort）
		query := applyNameLikeFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Unscoped().Where("snapshot_version = ?", version), keyword)

		var total int64
		if err := query.Count(&total).Error; err != nil {
			return searchCacheItem{Total: 0, PageCount: 1, Snapshots: []model.FilmListSnapshot{}}, nil
		}
		calcTotal := int(total)
		calcPageCount := (calcTotal + page.PageSize - 1) / page.PageSize
		if calcPageCount <= 0 {
			calcPageCount = 1
		}

		orderClause := snapshotSortOrderClause(sortField, true)
		offset := shared.PageOffset(page)

		var ids []uint
		if err := query.Select("id").Order(orderClause).Offset(offset).Limit(page.PageSize).Pluck("id", &ids).Error; err != nil {
			return searchCacheItem{Total: 0, PageCount: 1, Snapshots: []model.FilmListSnapshot{}}, nil
		}

		var snapshots []model.FilmListSnapshot
		if len(ids) > 0 {
			if err := db.Mdb.Model(&model.FilmListSnapshot{}).Unscoped().Select(snapshotSelectFields).Where("id IN ?", ids).Order(orderClause).Find(&snapshots).Error; err != nil {
				return searchCacheItem{Total: 0, PageCount: 1, Snapshots: []model.FilmListSnapshot{}}, nil
			}
		}
		if snapshots == nil {
			snapshots = []model.FilmListSnapshot{}
		}

		item := searchCacheItem{
			Total:     calcTotal,
			PageCount: calcPageCount,
			Snapshots: snapshots,
		}
		if db.Rdb != nil {
			// 仅在当前检索版本与全局版本一致时回写缓存，防止已过期的旧检索结果污染新版本
			if GetSearchCacheVersion() == searchVer {
				if raw, err := json.Marshal(item); err == nil {
					ttl := 3 * time.Minute
					if len(snapshots) == 0 {
						ttl = 1 * time.Minute // 空结果防穿透短缓存
					}
					_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), ttl).Err()
				}
			}
		}

		log.Printf("[SearchFilm] DB搜索完成 keyword=%q sort=%q cache=MISS total=%d page=%d size=%d cost=%s",
			keyword, sortField, calcTotal, page.Current, len(snapshots), time.Since(startedAt))
		return item, nil
	})

	if err != nil || val == nil {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}
	cachedItem, ok := val.(searchCacheItem)
	if !ok {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}
	page.Total = cachedItem.Total
	page.PageCount = cachedItem.PageCount
	return cloneFilmListSnapshots(cachedItem.Snapshots)
}
