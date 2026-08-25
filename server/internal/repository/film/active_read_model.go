package film

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
)

type FilmReadModel struct {
	Version string
}

var activeFilmReadModel atomic.Value
var activeFilmReadModelMu sync.Mutex

func init() {
	activeFilmReadModel.Store(&FilmReadModel{Version: ""})
}

func LoadActiveFilmReadModel(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	activeFilmReadModelMu.Lock()
	defer activeFilmReadModelMu.Unlock()
	activeFilmReadModel.Store(&FilmReadModel{Version: version})
	log.Printf("[ActiveReadModel] 活跃读模型已就绪 version=%s", version)
	return nil
}

func RefreshActiveProjectedReadModel() error {
	RefreshAccessDataCaches()
	return nil
}

func ApplyActiveFilmReadModelSnapshots(version string, snapshots []model.FilmListSnapshot, deletedMIDs []int64) error {
	RefreshAccessDataCaches()
	return nil
}

func ClearActiveFilmReadModel() {
	activeFilmReadModel.Store(&FilmReadModel{Version: ""})
}

func GetActiveFilmReadModel() *FilmReadModel {
	value := activeFilmReadModel.Load()
	if value == nil {
		return nil
	}
	readModel, _ := value.(*FilmReadModel)
	return readModel
}

func GetProjectedSnapshotByMid(version string, mid int64) *model.FilmListSnapshot {
	return GetSnapshotByMid(version, mid)
}

func GetProjectedSnapshotsByMidsOrdered(version string, mids []int64) []model.FilmListSnapshot {
	return GetSnapshotsByMidsOrdered(version, mids)
}

const (
	tagSearchCacheTTL = 3 * time.Minute
	snapshotSelectFields = "id, snapshot_version, mid, pid, cid, c_name, name, score, hits, update_stamp, remarks, state, picture, year, class_tag, area, language"
)

type tagSearchCacheItem struct {
	Total     int                     `json:"total"`
	PageCount int                     `json:"page_count"`
	Snapshots []model.FilmListSnapshot `json:"snapshots"`
}

func ListFilmSnapshotsByTagsReadModel(version string, st model.SearchTagsVO, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	st = normalizeSearchTagsVO(st)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" {
		return []model.FilmListSnapshot{}
	}

	cacheKey := fmt.Sprintf("EcoHub:tags_search:v%s:%d:%d:%s:%s:%s:%s:%s:p%d:s%d",
		version, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, page.Current, page.PageSize)
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

	query := db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version)
	if st.Pid > 0 {
		query = query.Where("pid = ?", st.Pid)
	}
	if st.Cid > 0 {
		query = query.Where("cid = ?", st.Cid)
	}
	if st.Plot != "" && st.Plot != "全部" && st.Plot != model.TagOthersValue && st.Plot != model.TagUnknownValue {
		query = query.Where("class_tag LIKE ?", "%"+escapeLikePattern(st.Plot)+"%")
	}
	if st.Area != "" && st.Area != "全部" && st.Area != model.TagOthersValue && st.Area != model.TagUnknownValue {
		query = query.Where("area = ?", st.Area)
	}
	if st.Language != "" && st.Language != "全部" && st.Language != model.TagOthersValue && st.Language != model.TagUnknownValue {
		query = query.Where("language = ?", st.Language)
	}
	if st.Year != "" && st.Year != "全部" && st.Year != model.TagOthersValue && st.Year != model.TagUnknownValue {
		if yearInt, err := strconv.ParseInt(st.Year, 10, 64); err == nil && yearInt > 0 {
			query = query.Where("year = ?", yearInt)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return []model.FilmListSnapshot{}
	}
	page.Total = int(total)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
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

	var snapshots []model.FilmListSnapshot
	offset := getPageOffset(page)
	if err := query.Select(snapshotSelectFields).Order(orderClause).Offset(offset).Limit(page.PageSize).Find(&snapshots).Error; err != nil {
		return []model.FilmListSnapshot{}
	}

	if db.Rdb != nil && len(snapshots) > 0 {
		item := tagSearchCacheItem{
			Total:     page.Total,
			PageCount: page.PageCount,
			Snapshots: snapshots,
		}
		if raw, err := json.Marshal(item); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), tagSearchCacheTTL).Err()
		}
	}

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
		page.PageSize,
		time.Since(startedAt),
	)
	return snapshots
}

func ListProvideSnapshotsReadModel(version string, st model.SearchTagsVO, keyword string, recentHours int, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	st = normalizeSearchTagsVO(st)
	keyword = strings.TrimSpace(keyword)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" {
		return []model.FilmListSnapshot{}
	}

	// 快速过滤非正常片名（例如 URL 或长度过长字符串），避免无意义全表扫描
	if len([]rune(keyword)) > 64 || strings.HasPrefix(keyword, "http://") || strings.HasPrefix(keyword, "https://") {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}

	// 1. 尝试从 Redis 读 Provide 缓存
	cacheKey := fmt.Sprintf("EcoHub:provide:v%s:%d:%d:%s:%s:%s:%s:%s:k%s:h%d:p%d:s%d",
		version, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, keyword, recentHours, page.Current, page.PageSize)
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

	query := db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version)
	if st.Pid > 0 {
		query = query.Where("pid = ?", st.Pid)
	}
	if st.Cid > 0 {
		query = query.Where("cid = ?", st.Cid)
	}
	if keyword != "" {
		like := "%" + escapeLikePattern(keyword) + "%"
		query = query.Where("name LIKE ? OR sub_title LIKE ?", like, like)
	}
	if recentHours > 0 {
		timeLimit := time.Now().Add(-time.Duration(recentHours) * time.Hour).Unix()
		query = query.Where("update_stamp >= ?", timeLimit)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return []model.FilmListSnapshot{}
	}
	page.Total = int(total)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	orderClause := "update_stamp DESC, id DESC"
	if st.Sort == "hits" {
		orderClause = "hits DESC, id DESC"
	} else if st.Sort == "score" {
		orderClause = "score DESC, id DESC"
	}

	var snapshots []model.FilmListSnapshot
	offset := getPageOffset(page)
	if err := query.Select(snapshotSelectFields).Order(orderClause).Offset(offset).Limit(page.PageSize).Find(&snapshots).Error; err != nil {
		return []model.FilmListSnapshot{}
	}

	// 2. 写入 Redis 缓存
	if db.Rdb != nil {
		item := searchCacheItem{
			Total:     page.Total,
			PageCount: page.PageCount,
			Snapshots: snapshots,
		}
		if raw, err := json.Marshal(item); err == nil {
			ttl := 3 * time.Minute
			if len(snapshots) == 0 {
				ttl = 1 * time.Minute // 空结果防穿透短缓存
			}
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), ttl).Err()
		}
	}

	log.Printf(
		"[ProvideVod] 筛选完成 pid=%d cid=%d keyword=%q total=%d page=%d size=%d cost=%s",
		st.Pid,
		st.Cid,
		keyword,
		page.Total,
		page.Current,
		page.PageSize,
		time.Since(startedAt),
	)
	return snapshots
}

type searchCacheItem struct {
	Total     int                     `json:"total"`
	PageCount int                     `json:"page_count"`
	Snapshots []model.FilmListSnapshot `json:"snapshots"`
}

func SearchSnapshotsByKeywordReadModel(version string, keyword string, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	keyword = strings.TrimSpace(keyword)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" || keyword == "" {
		return []model.FilmListSnapshot{}
	}

	// 快速过滤非正常片名（例如 URL 或长度过长字符串），避免无意义全表扫描
	if len([]rune(keyword)) > 64 || strings.HasPrefix(keyword, "http://") || strings.HasPrefix(keyword, "https://") {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}

	// 1. 尝试从 Redis 读搜索缓存
	cacheKey := fmt.Sprintf("EcoHub:search:v%s:%s:p%d:s%d", version, keyword, page.Current, page.PageSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var item searchCacheItem
			if json.Unmarshal([]byte(data), &item) == nil {
				page.Total = item.Total
				page.PageCount = item.PageCount
				log.Printf("[SearchFilm] 搜索命中缓存 keyword=%q cache=HIT total=%d page=%d size=%d cost=%s",
					keyword, item.Total, page.Current, len(item.Snapshots), time.Since(startedAt))
				return item.Snapshots
			}
		}
	}

	like := "%" + escapeLikePattern(keyword) + "%"
	query := db.Mdb.Model(&model.FilmListSnapshot{}).
		Where("snapshot_version = ? AND (name LIKE ? OR sub_title LIKE ?)", version, like, like)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return []model.FilmListSnapshot{}
	}
	page.Total = int(total)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	var snapshots []model.FilmListSnapshot
	offset := getPageOffset(page)
	if err := query.Select(snapshotSelectFields).Order("year DESC, update_stamp DESC, id DESC").Offset(offset).Limit(page.PageSize).Find(&snapshots).Error; err != nil {
		return []model.FilmListSnapshot{}
	}

	if db.Rdb != nil {
		item := searchCacheItem{
			Total:     page.Total,
			PageCount: page.PageCount,
			Snapshots: snapshots,
		}
		if raw, err := json.Marshal(item); err == nil {
			ttl := 3 * time.Minute
			if len(snapshots) == 0 {
				ttl = 1 * time.Minute // 空结果防穿透短缓存
			}
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), ttl).Err()
		}
	}

	log.Printf("[SearchFilm] 搜索完成 keyword=%q cache=MISS total=%d page=%d size=%d cost=%s",
		keyword, page.Total, page.Current, len(snapshots), time.Since(startedAt))
	return snapshots
}

func GetSearchPageReadModel(s model.SearchVo) []model.FilmIndex {
	startedAt := time.Now()
	page := ensurePage(s.Paging)

	query := db.Mdb.Model(&model.FilmIndex{}).Where("deleted_at IS NULL")
	if name := strings.TrimSpace(s.Name); name != "" {
		like := "%" + escapeLikePattern(name) + "%"
		query = query.Where("name LIKE ? OR sub_title LIKE ?", like, like)
	}
	if s.Pid > 0 {
		query = query.Where("pid = ?", s.Pid)
	}
	if s.Cid > 0 {
		query = query.Where("cid = ?", s.Cid)
	}
	if plot := strings.TrimSpace(s.Plot); plot != "" {
		query = query.Where("class_tag LIKE ?", "%"+escapeLikePattern(plot)+"%")
	}
	if area := strings.TrimSpace(s.Area); area != "" {
		query = query.Where("area = ?", area)
	}
	if lang := strings.TrimSpace(s.Language); lang != "" {
		query = query.Where("language = ?", lang)
	}
	if s.Year > 0 {
		query = query.Where("year = ?", s.Year)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return []model.FilmIndex{}
	}
	page.Total = int(total)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	var indexes []model.FilmIndex
	offset := getPageOffset(page)
	if err := query.Order("update_stamp DESC, id DESC").Offset(offset).Limit(page.PageSize).Find(&indexes).Error; err != nil {
		return []model.FilmIndex{}
	}

	log.Printf(
		"[ManageFilmSearch] 检索完成 name=%q pid=%d cid=%d total=%d page=%d size=%d cost=%s",
		s.Name,
		s.Pid,
		s.Cid,
		page.Total,
		page.Current,
		page.PageSize,
		time.Since(startedAt),
	)
	return indexes
}

func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

