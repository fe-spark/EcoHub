package snapshot

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/film/catalog"
	"server/internal/film/query"
	"server/internal/film/shared"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	snapshotListCacheTTL = 10 * time.Minute
	basicSelectFields    = "mid, first_source_id, pid, cid, c_name, name, score, hits, update_stamp, remarks, state, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture, year, class_tag, area, language, actor, director, blurb"
)

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
	if snaps, used, err := probeCategorySourceList(db.Mdb, sourceID, field, id, offset, limit); used {
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
		if !bannerTableIndexReady(query, model.TableFilmIndex, pidIndex) {
			return query
		}
		return query.Clauses(mysqlUseIndexHint{index: pidIndex})
	case "cid":
		if !bannerTableIndexReady(query, model.TableFilmIndex, cidIndex) {
			return query
		}
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

// HomeCategoryMids 首页一个分类的推荐片和热播 mid。推荐从热度池抽样，热播取热度序。
func HomeCategoryMids(version, sourceID, field string, categoryID int64, movieLimit, poolSize int) (movies []int64, hot []int64, err error) {
	if poolSize <= 0 {
		poolSize = searchHotPoolCap
	}
	if poolSize > searchHotPoolCap {
		poolSize = searchHotPoolCap
	}
	if movieLimit <= 0 {
		movieLimit = 14
	}
	pool, err := categoryRankMids(version, sourceID, field, categoryID, 1, poolSize)
	if err != nil {
		return nil, nil, err
	}
	hotN := movieLimit
	if hotN > len(pool) {
		hotN = len(pool)
	}
	hot = append([]int64(nil), pool[:hotN]...)
	if len(pool) <= movieLimit {
		return append([]int64(nil), pool...), hot, nil
	}
	order := make([]int, len(pool))
	for i := range order {
		order[i] = i
	}
	rand.Shuffle(len(order), func(i, j int) {
		order[i], order[j] = order[j], order[i]
	})
	movies = make([]int64, movieLimit)
	for i := 0; i < movieLimit; i++ {
		movies[i] = pool[order[i]]
	}
	return movies, hot, nil
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
	mids, err := categoryRankMids(version, sourceID, field, id, 1, need)
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

// GetSnapshotTopMoviesBySortFastWithSource 分类页三路排序。年份、热播、最新读同一套内存榜。
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
	mids, err := categoryRankMids(version, sourceID, "pid", pid, sortType, limit)
	if err != nil {
		return nil, err
	}
	result, err := movieInfosByMids(version, mids)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []model.MovieBasicInfo{}
	}
	log.Printf("[FilmSortListFast] 获取分类排序Top列表 source=%s pid=%d sortType=%d count=%d cost=%s",
		sourceID, pid, sortType, len(result), time.Since(startedAt))
	return result, nil
}

// GetSnapshotTopMoviesBySortFast 分类页三路排序，不限采集站。
func GetSnapshotTopMoviesBySortFast(version string, sortType int, pid int64, limit int) ([]model.MovieBasicInfo, error) {
	return GetSnapshotTopMoviesBySortFastWithSource(version, "", sortType, pid, limit)
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
		memberSQL, args := query.LiveCategoryMemberSQLIDs(dialectName(conn), sourceID, field, catalog.PublicSourceTypeIDs(sourceID, field, categoryID))
		return q.Where("mid IN (SELECT mid FROM ("+memberSQL+") AS cat_members)", args...)
	}
	q = applyCategorySnapshotSourceFilter(q, "", sourceID)
	if disabledIDs := taxonomy.GetDisabledCategoryIDs(); len(disabledIDs) > 0 {
		q = q.Where("pid NOT IN ? AND cid NOT IN ?", disabledIDs, disabledIDs)
	}
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
	return taxonomy.ResolveCategoryID(id)
}

// CountFilmsByCategoryKeys 若当前内存检索索引非空且与目标采集站（主站）匹配，直接返回按 category_key 汇总的影片数，零 SQL 开销。
// 若快照尚未封入任何分类（如首采/重建前空快照），返回 ok=false 交由调用方回源查询数据库，避免把新入库影片误统计为 0。
func CountFilmsByCategoryKeys(sourceID string) (map[string]int64, bool) {
	sourceID = strings.TrimSpace(sourceID)
	idx := activeFilmSearchMetas.Load()
	if idx == nil || sourceID == "" || strings.TrimSpace(idx.PrimaryID) != sourceID || len(idx.Items) == 0 || len(idx.ByCategory) == 0 {
		return nil, false
	}
	out := make(map[string]int64, len(idx.ByCategory))
	for key, list := range idx.ByCategory {
		if len(list) > 0 {
			out[key] = int64(len(list))
		}
	}
	return out, true
}

func dialectName(conn *gorm.DB) string {
	if conn == nil || conn.Dialector == nil {
		return ""
	}
	return conn.Dialector.Name()
}
