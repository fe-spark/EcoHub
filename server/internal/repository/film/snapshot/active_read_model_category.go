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
	"server/internal/repository/film/query"
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

	cacheKey := fmt.Sprintf("%s:v%s:src_%s:%s:%d:%d:%d:ck", config.FilmCategoryCachePrefix, version, sourceID, field, id, limit, offset)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	listGen := GetSearchCacheVersion()
	query := categoryFilmQuery(db.Mdb, sourceID, field, id, basicSelectFields)
	snapshots, err := scanListSnapshots(query.Order(liveTieOrder("update_stamp DESC, mid DESC")).Offset(offset).Limit(limit))
	if err != nil {
		return []model.MovieBasicInfo{}
	}
	result := shared.BuildMovieBasicInfosFromSnapshots(snapshots...)

	if raw, err := json.Marshal(result); err == nil {
		writeListCache(cacheKey, raw, listCacheTTL(len(result), snapshotListCacheTTL), listGen)
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

	cacheKey := fmt.Sprintf("%s:v%s:src_%s:%s:%d:p%d:s%d:ck", config.FilmCategoryPageCachePrefix, version, sourceID, field, id, page.Current, page.PageSize)
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

	listGen := GetSearchCacheVersion()
	query := categoryFilmQuery(db.Mdb, sourceID, field, id, "")

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

	item := categoryPageCacheItem{
		Total:     page.Total,
		PageCount: page.PageCount,
		Movies:    result,
	}
	if raw, err := json.Marshal(item); err == nil {
		writeListCache(cacheKey, raw, listCacheTTL(len(result), snapshotPageCacheTTL), listGen)
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

// hotCategoryQuery 热播按该站播放线路取 mid，再按 hits 截断。
// 禁止从 idx_pid_hits 扫整类再 EXISTS：新首选站在这个分类没有线路时，会把该分类全部热度行探完。
func hotCategoryQuery(conn *gorm.DB, sourceID, field string, categoryID int64) *gorm.DB {
	if conn == nil {
		return nil
	}
	col := "cid"
	if field == "pid" {
		col = "pid"
	}
	q := conn.Model(&model.FilmIndex{}).Select(basicSelectFields)
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return applyCategoryHotIndexHint(q.Where(col+" = ?", categoryID), field)
	}
	memberSQL, args := query.LiveCategoryMemberSQL(dialectName(conn), sourceID, field, categoryID)
	return q.Where("mid IN (SELECT mid FROM ("+memberSQL+") AS hot_members)", args...)
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

	cacheKey := fmt.Sprintf("%s:v%s:src_%s:%s:%d:%d:%d:ck", config.FilmHotCachePrefix, version, sourceID, field, id, limit, offset)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	listGen := GetSearchCacheVersion()
	query := hotCategoryQuery(db.Mdb, sourceID, field, id)
	snapshots, err := scanListSnapshots(query.Order(liveTieOrder("hits DESC, mid DESC")).Offset(offset).Limit(limit))
	if err != nil {
		return []model.MovieBasicInfo{}
	}
	result := shared.BuildMovieBasicInfosFromSnapshots(snapshots...)

	if raw, err := json.Marshal(result); err == nil {
		writeListCache(cacheKey, raw, listCacheTTL(len(result), snapshotListCacheTTL), listGen)
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

	cacheKey := fmt.Sprintf("%s:v%s:src_%s:%s:%d:%d:ck", config.FilmHotPoolCachePrefix, version, sourceID, field, id, poolSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	listGen := GetSearchCacheVersion()
	query := hotCategoryQuery(db.Mdb, sourceID, field, id)
	snapshots, err := scanListSnapshots(query.Order(liveTieOrder("hits DESC, mid DESC")).Limit(poolSize))
	if err != nil {
		return []model.MovieBasicInfo{}
	}
	result := shared.BuildMovieBasicInfosFromSnapshots(snapshots...)

	if raw, err := json.Marshal(result); err == nil {
		writeListCache(cacheKey, raw, listCacheTTL(len(result), snapshotListCacheTTL), listGen)
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
		orderClause = "hits DESC, mid DESC"
	case 2:
		orderClause = "update_stamp DESC"
	}

	// 热度 Top 与首页热播同一条 SQL。有采集站时从线路索引取成员，禁止扫 idx_pid_hits 再 EXISTS。
	query := categorySortFastQuery(db.Mdb, sourceID, sortType, pid)
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

// categorySortFastQuery 分类页三路排序。sortType 1 走热播成员查询，其余走更新时间索引加线路主键点查。
func categorySortFastQuery(conn *gorm.DB, sourceID string, sortType int, pid int64) *gorm.DB {
	if sortType == 1 {
		return hotCategoryQuery(conn, sourceID, "pid", pid)
	}
	return categoryFilmQuery(conn, sourceID, "pid", pid, basicSelectFields)
}

// categoryFilmQuery 分类列表。有来源映射时从该站播放线路取片，并认 category_key，避免 pid 仍为 0 的影片被丢掉。
func categoryFilmQuery(conn *gorm.DB, sourceID, field string, categoryID int64, selectFields string) *gorm.DB {
	if conn == nil {
		return nil
	}
	q := conn.Model(&model.FilmIndex{})
	if selectFields != "" {
		q = q.Select(selectFields)
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID != "" && query.HasLiveCategoryKeys(field, categoryID) {
		memberSQL, args := query.LiveCategoryMemberSQL(dialectName(conn), sourceID, field, categoryID)
		return q.Where("mid IN (SELECT mid FROM ("+memberSQL+") AS cat_members)", args...)
	}
	q = applyCategorySnapshotSourceFilter(q, "", sourceID)
	q = applyCategoryUpdateIndexHint(q, field)
	if field == "pid" {
		return q.Where("pid = ?", categoryID)
	}
	return q.Where("cid = ?", categoryID)
}

func dialectName(conn *gorm.DB) string {
	if conn == nil || conn.Dialector == nil {
		return ""
	}
	return conn.Dialector.Name()
}
