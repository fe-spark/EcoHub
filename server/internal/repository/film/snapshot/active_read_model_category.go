package snapshot

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	snapshotListCacheTTL = 10 * time.Minute
	snapshotPageCacheTTL = 5 * time.Minute
	basicSelectFields    = "mid, first_source_id, pid, cid, c_name, name, score, hits, update_stamp, remarks, state, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture, year"
)

type categoryPageCacheItem struct {
	Total     int                    `json:"total"`
	PageCount int                    `json:"page_count"`
	Movies    []model.MovieBasicInfo `json:"movies"`
}

func applyCategorySnapshotSourceFilter(query *gorm.DB, version string, sourceID string) *gorm.DB {
	return applySourceMembership(query, sourceID)
}

func GetSnapshotMovieListByCategoryWithSourceReadModel(version string, sourceID string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	startedAt := time.Now()
	version = strings.TrimSpace(version)
	sourceID = strings.TrimSpace(sourceID)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	id = support.ResolveCategoryID(id)
	if version == "" || id <= 0 || limit <= 0 {
		return []model.MovieBasicInfo{}
	}
	if offset < 0 {
		offset = 0
	}

	cacheKey := fmt.Sprintf("%s:v%s:src_%s:%s:%d:%d:%d", config.FilmCategoryCachePrefix, version, sourceID, field, id, limit, offset)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	query := applyCategorySnapshotSourceFilter(liveFilmQuery().Select(basicSelectFields), version, sourceID)
	query = applyCategoryUpdateIndexHint(query, field)
	if field == "pid" {
		query = query.Where("pid = ?", id)
	} else {
		query = query.Where("cid = ?", id)
	}

	snapshots, err := scanListSnapshots(query.Order(liveTieOrder("update_stamp DESC, mid DESC")).Offset(offset).Limit(limit))
	if err != nil {
		return []model.MovieBasicInfo{}
	}
	result := shared.BuildMovieBasicInfosFromSnapshots(snapshots...)

	if db.Rdb != nil && len(result) > 0 {
		if raw, err := json.Marshal(result); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), snapshotListCacheTTL).Err()
		}
	}

	log.Printf("[FilmCategoryList] 获取分类列表 source=%s field=%s id=%d count=%d offset=%d limit=%d cost=%s",
		sourceID, field, id, len(result), offset, limit, time.Since(startedAt))
	return result
}

func GetSnapshotMovieListByCategoryReadModel(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	return GetSnapshotMovieListByCategoryWithSourceReadModel(version, "", field, id, limit, offset)
}

func GetSnapshotMovieListByCategoryPageWithSourceReadModel(version string, sourceID string, field string, id int64, page *dto.Page) []model.MovieBasicInfo {
	startedAt := time.Now()
	page = shared.EnsurePage(page)
	version = strings.TrimSpace(version)
	sourceID = strings.TrimSpace(sourceID)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	id = support.ResolveCategoryID(id)
	if version == "" || id <= 0 {
		return []model.MovieBasicInfo{}
	}

	cacheKey := fmt.Sprintf("%s:v%s:src_%s:%s:%d:p%d:s%d", config.FilmCategoryPageCachePrefix, version, sourceID, field, id, page.Current, page.PageSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var item categoryPageCacheItem
			if json.Unmarshal([]byte(data), &item) == nil {
				page.Total = item.Total
				page.PageCount = item.PageCount
				return item.Movies
			}
		}
	}

	query := applyCategorySnapshotSourceFilter(liveFilmQuery(), version, sourceID)
	query = applyCategoryUpdateIndexHint(query, field)
	if field == "pid" {
		query = query.Where("pid = ?", id)
	} else {
		query = query.Where("cid = ?", id)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return []model.MovieBasicInfo{}
	}
	page.Total = int(total)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	offset := shared.PageOffset(page)
	snapshots, err := scanListSnapshots(query.Select(basicSelectFields).Order(liveTieOrder("update_stamp DESC, mid DESC")).Offset(offset).Limit(page.PageSize))
	if err != nil {
		return []model.MovieBasicInfo{}
	}
	result := shared.BuildMovieBasicInfosFromSnapshots(snapshots...)

	if db.Rdb != nil && len(result) > 0 {
		item := categoryPageCacheItem{
			Total:     page.Total,
			PageCount: page.PageCount,
			Movies:    result,
		}
		if raw, err := json.Marshal(item); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), snapshotPageCacheTTL).Err()
		}
	}

	log.Printf("[FilmCategoryList] 获取分类分页列表 source=%s field=%s id=%d total=%d page=%d size=%d cost=%s",
		sourceID, field, id, page.Total, page.Current, len(result), time.Since(startedAt))
	return result
}

func GetSnapshotMovieListByCategoryPageReadModel(version string, field string, id int64, page *dto.Page) []model.MovieBasicInfo {
	return GetSnapshotMovieListByCategoryPageWithSourceReadModel(version, "", field, id, page)
}

// mysqlUseIndexHint 把 USE INDEX 挂到 FROM 子句之后，避免 Table("t USE INDEX ...")
// 被驱动整段加反引号后变成非法表名。
type mysqlUseIndexHint struct {
	index string
}

func (h mysqlUseIndexHint) ModifyStatement(stmt *gorm.Statement) {
	if stmt == nil || strings.TrimSpace(h.index) == "" {
		return
	}
	c := stmt.Clauses["FROM"]
	c.AfterExpression = h
	stmt.Clauses["FROM"] = c
}

func (h mysqlUseIndexHint) Build(builder clause.Builder) {
	builder.WriteString("USE INDEX (")
	builder.WriteQuoted(h.index)
	builder.WriteByte(')')
}

func applyCategoryHotIndexHint(query *gorm.DB, field string) *gorm.DB {
	return applyCategoryIndexHint(query, field, "idx_pid_hits", "idx_cid_hits")
}

func applyCategoryUpdateIndexHint(query *gorm.DB, field string) *gorm.DB {
	return applyCategoryIndexHint(query, field, "idx_film_index_pid_update_mid", "idx_cid_update")
}

func applyCategoryIndexHint(query *gorm.DB, field, pidIndex, cidIndex string) *gorm.DB {
	if query == nil || query.Dialector == nil {
		return query
	}
	if query.Dialector.Name() != "mysql" {
		return query
	}
	switch field {
	case "pid":
		return query.Clauses(mysqlUseIndexHint{index: pidIndex})
	case "cid":
		return query.Clauses(mysqlUseIndexHint{index: cidIndex})
	default:
		return query
	}
}

func GetSnapshotHotMovieListByCategoryWithSourceReadModel(version string, sourceID string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	startedAt := time.Now()
	version = strings.TrimSpace(version)
	sourceID = strings.TrimSpace(sourceID)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	id = support.ResolveCategoryID(id)
	if version == "" || id <= 0 || limit <= 0 {
		return []model.MovieBasicInfo{}
	}
	if offset < 0 {
		offset = 0
	}

	cacheKey := fmt.Sprintf("%s:v%s:src_%s:%s:%d:%d:%d", config.FilmHotCachePrefix, version, sourceID, field, id, limit, offset)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	query := applyCategorySnapshotSourceFilter(liveFilmQuery().Select(basicSelectFields), version, sourceID)
	query = applyCategoryHotIndexHint(query, field)
	if field == "pid" {
		query = query.Where("pid = ?", id)
	} else {
		query = query.Where("cid = ?", id)
	}

	snapshots, err := scanListSnapshots(query.Order(liveTieOrder("hits DESC, mid DESC")).Offset(offset).Limit(limit))
	if err != nil {
		return []model.MovieBasicInfo{}
	}
	result := shared.BuildMovieBasicInfosFromSnapshots(snapshots...)

	if db.Rdb != nil && len(result) > 0 {
		if raw, err := json.Marshal(result); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), snapshotListCacheTTL).Err()
		}
	}

	log.Printf("[FilmHotList] 获取分类热播列表 source=%s field=%s id=%d count=%d offset=%d limit=%d cost=%s",
		sourceID, field, id, len(result), offset, limit, time.Since(startedAt))
	return result
}

func GetSnapshotHotMovieListByCategoryReadModel(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	return GetSnapshotHotMovieListByCategoryWithSourceReadModel(version, "", field, id, limit, offset)
}

func GetSnapshotHotPoolByCategoryWithSourceReadModel(version string, sourceID string, field string, id int64, poolSize int) []model.MovieBasicInfo {
	startedAt := time.Now()
	version = strings.TrimSpace(version)
	sourceID = strings.TrimSpace(sourceID)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	id = support.ResolveCategoryID(id)
	if version == "" || id <= 0 || poolSize <= 0 {
		return []model.MovieBasicInfo{}
	}

	cacheKey := fmt.Sprintf("%s:v%s:src_%s:%s:%d:%d", config.FilmHotPoolCachePrefix, version, sourceID, field, id, poolSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	query := applyCategorySnapshotSourceFilter(liveFilmQuery().Select(basicSelectFields), version, sourceID)
	query = applyCategoryHotIndexHint(query, field)
	if field == "pid" {
		query = query.Where("pid = ?", id)
	} else {
		query = query.Where("cid = ?", id)
	}

	snapshots, err := scanListSnapshots(query.Order(liveTieOrder("hits DESC, mid DESC")).Limit(poolSize))
	if err != nil {
		return []model.MovieBasicInfo{}
	}
	result := shared.BuildMovieBasicInfosFromSnapshots(snapshots...)

	if db.Rdb != nil && len(result) > 0 {
		if raw, err := json.Marshal(result); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), snapshotListCacheTTL).Err()
		}
	}

	log.Printf("[FilmHotPool] 获取分类热门候选池 source=%s field=%s id=%d count=%d poolSize=%d cost=%s",
		sourceID, field, id, len(result), poolSize, time.Since(startedAt))
	return result
}

func GetSnapshotHotPoolByCategoryReadModel(version string, field string, id int64, poolSize int) []model.MovieBasicInfo {
	return GetSnapshotHotPoolByCategoryWithSourceReadModel(version, "", field, id, poolSize)
}

func GetSnapshotDynamicHotMovieListByCategoryWithSourceReadModel(version string, sourceID string, field string, id int64, limit int, poolSize int) []model.MovieBasicInfo {
	if limit <= 0 {
		return []model.MovieBasicInfo{}
	}
	if poolSize <= 0 {
		poolSize = 50
	}
	pool := GetSnapshotHotPoolByCategoryWithSourceReadModel(version, sourceID, field, id, poolSize)
	if len(pool) <= limit {
		res := make([]model.MovieBasicInfo, len(pool))
		copy(res, pool)
		return res
	}

	indices := make([]int, len(pool))
	for i := range indices {
		indices[i] = i
	}
	rand.Shuffle(len(indices), func(i, j int) {
		indices[i], indices[j] = indices[j], indices[i]
	})

	result := make([]model.MovieBasicInfo, limit)
	for i := 0; i < limit; i++ {
		result[i] = pool[indices[i]]
	}
	return result
}

func GetSnapshotDynamicHotMovieListByCategoryReadModel(version string, field string, id int64, limit int, poolSize int) []model.MovieBasicInfo {
	return GetSnapshotDynamicHotMovieListByCategoryWithSourceReadModel(version, "", field, id, limit, poolSize)
}

// GetSnapshotTopMoviesBySortFastWithSource 快速获取指定采集站分类排序 Top 影片
func GetSnapshotTopMoviesBySortFastWithSource(version string, sourceID string, sortType int, pid int64, limit int) []model.MovieBasicInfo {
	startedAt := time.Now()
	version = strings.TrimSpace(version)
	sourceID = strings.TrimSpace(sourceID)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	pid = support.ResolveCategoryID(pid)
	if version == "" || pid <= 0 || limit <= 0 {
		return []model.MovieBasicInfo{}
	}
	if db.Mdb == nil {
		return []model.MovieBasicInfo{}
	}

	orderClause := "update_stamp DESC"
	switch sortType {
	case 0:
		orderClause = "year DESC, update_stamp DESC"
	case 1:
		orderClause = "hits DESC"
	case 2:
		orderClause = "update_stamp DESC"
	}

	query := applyCategorySnapshotSourceFilter(liveFilmQuery().Select(basicSelectFields).Where("pid = ?", pid), version, sourceID)
	if sortType == 1 {
		query = applyCategoryHotIndexHint(query, "pid")
	} else {
		query = applyCategoryUpdateIndexHint(query, "pid")
	}
	snapshots, err := scanListSnapshots(query.Order(liveTieOrder(orderClause)).Limit(limit))
	if err != nil {
		return []model.MovieBasicInfo{}
	}
	result := shared.BuildMovieBasicInfosFromSnapshots(snapshots...)
	log.Printf("[FilmSortListFast] 获取分类排序Top列表 source=%s pid=%d sortType=%d count=%d cost=%s",
		sourceID, pid, sortType, len(result), time.Since(startedAt))
	return result
}

// GetSnapshotTopMoviesBySortFast 快速获取分类排序 Top 影片，直接基于复合索引排序，消除 COUNT 扫描开销
func GetSnapshotTopMoviesBySortFast(version string, sortType int, pid int64, limit int) []model.MovieBasicInfo {
	return GetSnapshotTopMoviesBySortFastWithSource(version, "", sortType, pid, limit)
}
