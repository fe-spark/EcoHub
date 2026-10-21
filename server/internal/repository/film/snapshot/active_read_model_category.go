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
	"server/internal/repository"
	"server/internal/repository/film/query"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	snapshotListCacheTTL = 10 * time.Minute
	snapshotPageCacheTTL = 5 * time.Minute
	basicSelectFields    = "mid, first_source_id, pid, cid, c_name, name, score, hits, update_stamp, remarks, state, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture, year, class_tag, area, language, actor, director, blurb"
)

type categoryPageCacheItem struct {
	Total     int                    `json:"total"`
	PageCount int                    `json:"page_count"`
	Movies    []model.MovieBasicInfo `json:"movies"`
}

func applyCategorySnapshotSourceFilter(query *gorm.DB, version string, sourceID string) *gorm.DB {
	return applySourceMembership(query, sourceID)
}

func GetSnapshotMovieListByCategoryWithSourceReadModel(version string, sourceID string, field string, id int64, limit int, offset int) ([]model.MovieBasicInfo, error) {
	startedAt := time.Now()
	version = strings.TrimSpace(version)
	sourceID = strings.TrimSpace(sourceID)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	id = listedCategoryID(sourceID, id)
	if version == "" || id <= 0 || limit <= 0 {
		return []model.MovieBasicInfo{}, nil
	}
	if offset < 0 {
		offset = 0
	}

	cacheKey := fmt.Sprintf("%s:v%s:src_%s:%s:%d:%d:%d:ck5", config.FilmCategoryCachePrefix, version, sourceID, field, id, limit, offset)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached, nil
			}
		}
	}

	listGen := GetSearchCacheVersion()
	if snaps, used, err := probeCategorySourceList(db.Mdb, sourceID, field, id, "update", offset, limit); used {
		if err != nil {
			return nil, err
		}
		return finishCategoryList(cacheKey, listGen, snaps, sourceID, field, id, offset, limit, startedAt), nil
	}
	if db.Mdb == nil {
		return nil, ErrSearchIndexMissing
	}
	query := categoryFilmQuery(db.Mdb, sourceID, field, id, basicSelectFields)
	snapshots, err := scanListSnapshots(query.Order(liveTieOrder("update_stamp DESC, mid DESC")).Offset(offset).Limit(limit))
	if err != nil {
		return nil, err
	}
	return finishCategoryList(cacheKey, listGen, snapshots, sourceID, field, id, offset, limit, startedAt), nil
}

func finishCategoryList(cacheKey string, listGen string, snapshots []model.FilmListSnapshot, sourceID, field string, id int64, offset, limit int, startedAt time.Time) []model.MovieBasicInfo {
	result := shared.BuildMovieBasicInfosFromSnapshots(snapshots...)
	if raw, err := json.Marshal(result); err == nil {
		writeListCache(cacheKey, raw, listCacheTTL(len(result), snapshotListCacheTTL), listGen)
	}
	log.Printf("[FilmCategoryList] 获取分类列表 source=%s field=%s id=%d count=%d offset=%d limit=%d cost=%s",
		sourceID, field, id, len(result), offset, limit, time.Since(startedAt))
	return result
}

func GetSnapshotMovieListByCategoryReadModel(version string, field string, id int64, limit int, offset int) ([]model.MovieBasicInfo, error) {
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
	id = listedCategoryID(sourceID, id)
	if version == "" || id <= 0 {
		return []model.MovieBasicInfo{}
	}

	cacheKey := fmt.Sprintf("%s:v%s:src_%s:%s:%d:p%d:s%d:ck5", config.FilmCategoryPageCachePrefix, version, sourceID, field, id, page.Current, page.PageSize)
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

// hotCategoryQuery 热播深分页仍从该站线路取成员。
// 首页这种小结果集不走这里，改由分类键索引点查播放线路主键。
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
	memberSQL, args := query.LiveCategoryMemberSQLIDs(dialectName(conn), sourceID, field, repository.PublicSourceTypeIDs(sourceID, field, categoryID))
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

func movieInfosByMids(version string, mids []int64) ([]model.MovieBasicInfo, error) {
	if len(mids) == 0 {
		return []model.MovieBasicInfo{}, nil
	}
	snaps, err := GetSnapshotsByMidsOrdered(version, mids)
	if err != nil {
		return nil, err
	}
	if len(snaps) == 0 {
		return []model.MovieBasicInfo{}, nil
	}
	infos := shared.BuildMovieBasicInfosFromSnapshots(snaps...)
	if infos == nil {
		return []model.MovieBasicInfo{}, nil
	}
	return infos, nil
}

func resolveHotListVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = strings.TrimSpace(GetActiveSnapshotVersion())
	}
	if version == "" {
		version = LiveReadVersion
	}
	return version
}

func GetSnapshotHotMovieListByCategoryWithSourceReadModel(version string, sourceID string, field string, id int64, limit int, offset int) ([]model.MovieBasicInfo, error) {
	if limit <= 0 {
		return []model.MovieBasicInfo{}, nil
	}
	if offset < 0 {
		offset = 0
	}
	sourceID = strings.TrimSpace(sourceID)
	id = listedCategoryID(sourceID, id)
	if id <= 0 {
		return []model.MovieBasicInfo{}, nil
	}
	need := offset + limit
	if need > searchHotPoolCap {
		need = searchHotPoolCap
	}
	version = resolveHotListVersion(version)
	mids, err := hotMidsFromIndex(version, sourceID, field, id, need)
	if err != nil {
		return nil, err
	}
	if offset >= len(mids) {
		return []model.MovieBasicInfo{}, nil
	}
	end := offset + limit
	if end > len(mids) {
		end = len(mids)
	}
	return movieInfosByMids(version, mids[offset:end])
}

func GetSnapshotHotMovieListByCategoryReadModel(version string, field string, id int64, limit int, offset int) ([]model.MovieBasicInfo, error) {
	return GetSnapshotHotMovieListByCategoryWithSourceReadModel(version, "", field, id, limit, offset)
}

func GetSnapshotHotPoolByCategoryWithSourceReadModel(version string, sourceID string, field string, id int64, poolSize int) ([]model.MovieBasicInfo, error) {
	if poolSize <= 0 {
		poolSize = searchHotPoolCap
	}
	if poolSize > searchHotPoolCap {
		poolSize = searchHotPoolCap
	}
	sourceID = strings.TrimSpace(sourceID)
	id = listedCategoryID(sourceID, id)
	if id <= 0 {
		return []model.MovieBasicInfo{}, nil
	}
	version = resolveHotListVersion(version)
	mids, err := hotMidsFromIndex(version, sourceID, field, id, poolSize)
	if err != nil {
		return nil, err
	}
	return movieInfosByMids(version, mids)
}

func GetSnapshotHotPoolByCategoryReadModel(version string, field string, id int64, poolSize int) ([]model.MovieBasicInfo, error) {
	return GetSnapshotHotPoolByCategoryWithSourceReadModel(version, "", field, id, poolSize)
}

func GetSnapshotDynamicHotMovieListByCategoryWithSourceReadModel(version string, sourceID string, field string, id int64, limit int, poolSize int) ([]model.MovieBasicInfo, error) {
	if limit <= 0 {
		return []model.MovieBasicInfo{}, nil
	}
	if poolSize <= 0 {
		poolSize = 50
	}
	pool, err := GetSnapshotHotPoolByCategoryWithSourceReadModel(version, sourceID, field, id, poolSize)
	if err != nil {
		return nil, err
	}
	if len(pool) <= limit {
		res := make([]model.MovieBasicInfo, len(pool))
		copy(res, pool)
		return res, nil
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
	return result, nil
}

func GetSnapshotDynamicHotMovieListByCategoryReadModel(version string, field string, id int64, limit int, poolSize int) ([]model.MovieBasicInfo, error) {
	return GetSnapshotDynamicHotMovieListByCategoryWithSourceReadModel(version, "", field, id, limit, poolSize)
}

// GetSnapshotTopMoviesBySortFastWithSource 快速获取指定采集站分类排序 Top 影片
func GetSnapshotTopMoviesBySortFastWithSource(version string, sourceID string, sortType int, pid int64, limit int) ([]model.MovieBasicInfo, error) {
	startedAt := time.Now()
	version = strings.TrimSpace(version)
	sourceID = strings.TrimSpace(sourceID)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	pid = listedCategoryID(sourceID, pid)
	if version == "" || pid <= 0 || limit <= 0 {
		return []model.MovieBasicInfo{}, nil
	}
	if db.Mdb == nil {
		return nil, ErrSearchIndexMissing
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

	if sortType == 1 || sortType == 2 {
		orderKind := "update"
		if sortType == 1 {
			orderKind = "hits"
		}
		if snaps, used, err := probeCategorySourceList(db.Mdb, sourceID, "pid", pid, orderKind, 0, limit); used {
			if err != nil {
				return nil, err
			}
			result := shared.BuildMovieBasicInfosFromSnapshots(snaps...)
			log.Printf("[FilmSortListFast] 获取分类排序Top列表 source=%s pid=%d sortType=%d count=%d cost=%s",
				sourceID, pid, sortType, len(result), time.Since(startedAt))
			return result, nil
		}
	}
	query := categorySortFastQuery(db.Mdb, sourceID, sortType, pid)
	snapshots, err := scanListSnapshots(query.Order(liveTieOrder(orderClause)).Limit(limit))
	if err != nil {
		return nil, err
	}
	result := shared.BuildMovieBasicInfosFromSnapshots(snapshots...)
	log.Printf("[FilmSortListFast] 获取分类排序Top列表 source=%s pid=%d sortType=%d count=%d cost=%s",
		sourceID, pid, sortType, len(result), time.Since(startedAt))
	return result, nil
}

// GetSnapshotTopMoviesBySortFast 快速获取分类排序 Top 影片，直接基于复合索引排序，消除 COUNT 扫描开销
func GetSnapshotTopMoviesBySortFast(version string, sortType int, pid int64, limit int) ([]model.MovieBasicInfo, error) {
	return GetSnapshotTopMoviesBySortFastWithSource(version, "", sortType, pid, limit)
}

// categorySortFastQuery 分类页三路排序的深分页回退。
// 热播和最新的小结果集先走分类键索引点查，这里只在点查不适用时使用。
func categorySortFastQuery(conn *gorm.DB, sourceID string, sortType int, pid int64) *gorm.DB {
	if sortType == 1 {
		return hotCategoryQuery(conn, sourceID, "pid", pid)
	}
	return categoryFilmQuery(conn, sourceID, "pid", pid, basicSelectFields)
}

// categoryFilmQuery 分类列表。指定采集站时只认该站自己的分类键。
func categoryFilmQuery(conn *gorm.DB, sourceID, field string, categoryID int64, selectFields string) *gorm.DB {
	if conn == nil {
		return nil
	}
	q := conn.Model(&model.FilmIndex{})
	if selectFields != "" {
		q = q.Select(selectFields)
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID != "" {
		memberSQL, args := query.LiveCategoryMemberSQLIDs(dialectName(conn), sourceID, field, repository.PublicSourceTypeIDs(sourceID, field, categoryID))
		return q.Where("mid IN (SELECT mid FROM ("+memberSQL+") AS cat_members)", args...)
	}
	q = applyCategorySnapshotSourceFilter(q, "", sourceID)
	q = applyCategoryUpdateIndexHint(q, field)
	if field == "pid" {
		return q.Where("pid = ?", categoryID)
	}
	return q.Where("cid = ?", categoryID)
}

// listedCategoryID 有采集站时，传入的 id 就是该站 type_id，不再改写成本地 film_category.id。
func listedCategoryID(sourceID string, id int64) int64 {
	if strings.TrimSpace(sourceID) != "" {
		return id
	}
	return support.ResolveCategoryID(id)
}

func dialectName(conn *gorm.DB) string {
	if conn == nil || conn.Dialector == nil {
		return ""
	}
	return conn.Dialector.Name()
}
