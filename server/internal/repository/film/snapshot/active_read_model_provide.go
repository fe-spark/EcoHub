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

	"golang.org/x/sync/singleflight"
)

var provideSnapshotsSf singleflight.Group

func ListProvideSnapshotsReadModel(version string, st model.SearchTagsVO, keyword string, recentHours int, page *dto.Page) []model.FilmListSnapshot {
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

	// 1. 尝试从 Redis 读 Provide 缓存
	cacheKey := fmt.Sprintf("%s:v%s:%d:%d:%s:%s:%s:%s:%s:k%s:h%d:p%d:s%d",
		config.ProvideListKey, version, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, keyword, recentHours, page.Current, page.PageSize)
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
				return item.Snapshots
			}
		}
	}

	// 2. 并发防击穿：相同参数的 ProvideVod 请求合并执行
	sfKey := fmt.Sprintf("v%s:%d:%d:%s:%s:%s:%s:%s:k%s:h%d:p%d:s%d",
		version, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, keyword, recentHours, page.Current, page.PageSize)
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

		// A. 若有搜索词且无时间限制和复合分类筛选，优先走内存元数据检索
		if keyword != "" && recentHours == 0 && st.Plot == "" && st.Area == "" && st.Language == "" && st.Year == "" {
			idx := loadFilmSearchMetaIndex(version)
			if idx != nil && len(idx.Items) > 0 {
				hits := searchFilmMetas(idx, keyword, st.Sort, st.Pid, st.Cid)
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
					if raw, err := json.Marshal(item); err == nil {
						ttl := 3 * time.Minute
						if len(snapshots) == 0 {
							ttl = 1 * time.Minute
						}
						_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), ttl).Err()
					}
				}
				log.Printf(
					"[ProvideVod] 内存筛选完成 pid=%d cid=%d keyword=%q total=%d page=%d size=%d cost=%s",
					st.Pid,
					st.Cid,
					keyword,
					page.Total,
					page.Current,
					len(snapshots),
					time.Since(startedAt),
				)
				return item, nil
			}
		}

		if db.Mdb == nil {
			return searchCacheItem{Total: 0, PageCount: 1, Snapshots: []model.FilmListSnapshot{}}, nil
		}

		query := db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version)
		if st.Pid > 0 {
			query = query.Where("pid = ?", st.Pid)
		}
		if st.Cid > 0 {
			query = query.Where("cid = ?", st.Cid)
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
			return searchCacheItem{Total: 0, PageCount: 1, Snapshots: []model.FilmListSnapshot{}}, nil
		}
		calcTotal := int(total)
		calcPageCount := (calcTotal + page.PageSize - 1) / page.PageSize
		if calcPageCount <= 0 {
			calcPageCount = 1
		}

		orderClause := snapshotSortOrderClause(st.Sort, keyword != "")
		offset := shared.PageOffset(page)

		// 延迟关联：先取 id，再取宽字段，避免大宽表参与文件排序
		var ids []uint
		if err := query.Select("id").Order(orderClause).Offset(offset).Limit(page.PageSize).Pluck("id", &ids).Error; err != nil {
			return searchCacheItem{Total: 0, PageCount: 1, Snapshots: []model.FilmListSnapshot{}}, nil
		}

		var snapshots []model.FilmListSnapshot
		if len(ids) > 0 {
			if err := db.Mdb.Model(&model.FilmListSnapshot{}).Select(snapshotSelectFields).Where("id IN ?", ids).Order(orderClause).Find(&snapshots).Error; err != nil {
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

		// 写入 Redis 缓存
		if db.Rdb != nil {
			if raw, err := json.Marshal(item); err == nil {
				ttl := 3 * time.Minute
				if len(snapshots) == 0 {
					ttl = 1 * time.Minute
				}
				_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), ttl).Err()
			}
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

	if err == nil && val != nil {
		if item, ok := val.(searchCacheItem); ok {
			page.Total = item.Total
			page.PageCount = item.PageCount
			return item.Snapshots
		}
	}

	return []model.FilmListSnapshot{}
}
