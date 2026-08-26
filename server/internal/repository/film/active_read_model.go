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

type filmSearchMemoryItem struct {
	Mid         int64
	Pid         int64
	Cid         int64
	Name        string
	LowerName   string
	Year        int64
	UpdateStamp int64
}

type filmSearchMemoryIndex struct {
	Version string
	Items   []filmSearchMemoryItem
}

var activeFilmReadModel atomic.Pointer[FilmReadModel]
var activeFilmReadModelMu sync.Mutex

var activeFilmSearchIndex atomic.Pointer[filmSearchMemoryIndex]
var activeFilmSearchIndexMu sync.Mutex

func init() {
	activeFilmReadModel.Store(&FilmReadModel{Version: ""})
	activeFilmSearchIndex.Store(&filmSearchMemoryIndex{Version: ""})
}

func getOrLoadFilmSearchMemoryIndex(version string) *filmSearchMemoryIndex {
	if version == "" {
		return nil
	}
	if idx := activeFilmSearchIndex.Load(); idx != nil && idx.Version == version && len(idx.Items) > 0 {
		return idx
	}
	activeFilmSearchIndexMu.Lock()
	defer activeFilmSearchIndexMu.Unlock()
	if idx := activeFilmSearchIndex.Load(); idx != nil && idx.Version == version && len(idx.Items) > 0 {
		return idx
	}

	if db.Mdb == nil {
		return nil
	}

	var rows []struct {
		Mid         int64
		Pid         int64
		Cid         int64
		Name        string
		Year        int64
		UpdateStamp int64
	}
	if err := db.Mdb.Model(&model.FilmListSnapshot{}).
		Select("mid, pid, cid, name, year, update_stamp").
		Where("snapshot_version = ?", version).
		Order("year DESC, update_stamp DESC, id DESC").
		Scan(&rows).Error; err != nil {
		log.Printf("[ActiveReadModel] 加载内存搜索索引失败: %v", err)
		return nil
	}

	items := make([]filmSearchMemoryItem, 0, len(rows))
	for _, r := range rows {
		if r.Mid > 0 && r.Name != "" {
			items = append(items, filmSearchMemoryItem{
				Mid:         r.Mid,
				Pid:         r.Pid,
				Cid:         r.Cid,
				Name:        r.Name,
				LowerName:   strings.ToLower(r.Name),
				Year:        r.Year,
				UpdateStamp: r.UpdateStamp,
			})
		}
	}
	newIdx := &filmSearchMemoryIndex{
		Version: version,
		Items:   items,
	}
	activeFilmSearchIndex.Store(newIdx)
	log.Printf("[ActiveReadModel] 内存搜索索引已构建 version=%s count=%d", version, len(items))
	return newIdx
}

func LoadActiveFilmReadModel(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	activeFilmReadModelMu.Lock()
	defer activeFilmReadModelMu.Unlock()
	activeFilmReadModel.Store(&FilmReadModel{Version: version})
	go func(ver string) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[ActiveReadModel] 异步构建内存搜索索引发生异常: %v", r)
			}
		}()
		getOrLoadFilmSearchMemoryIndex(ver)
	}(version)
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
	activeFilmSearchIndex.Store(&filmSearchMemoryIndex{Version: ""})
}

func GetActiveFilmReadModel() *FilmReadModel {
	return activeFilmReadModel.Load()
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

	// 2. 若带有搜索关键词且无时间限制，优先走内存搜索索引（1~2ms 极速响应）
	if keyword != "" && recentHours == 0 {
		idx := getOrLoadFilmSearchMemoryIndex(version)
		if idx != nil && len(idx.Items) > 0 {
			lowerKey := strings.ToLower(keyword)
			var matchedMids []int64
			for _, item := range idx.Items {
				if st.Pid > 0 && item.Pid != st.Pid {
					continue
				}
				if st.Cid > 0 && item.Cid != st.Cid {
					continue
				}
				if strings.Contains(item.LowerName, lowerKey) {
					matchedMids = append(matchedMids, item.Mid)
				}
			}

			page.Total = len(matchedMids)
			page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
			if page.PageCount <= 0 {
				page.PageCount = 1
			}

			var snapshots []model.FilmListSnapshot
			offset := getPageOffset(page)
			if offset < len(matchedMids) {
				end := offset + page.PageSize
				if end > len(matchedMids) {
					end = len(matchedMids)
				}
				pageMids := matchedMids[offset:end]
				snapshots = GetProjectedSnapshotsByMidsOrdered(version, pageMids)
			}
			if snapshots == nil {
				snapshots = []model.FilmListSnapshot{}
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

			log.Printf(
				"[ProvideVod] 内存搜索完成 pid=%d cid=%d keyword=%q cache=MISS(MEMORY_HIT) total=%d page=%d size=%d cost=%s",
				st.Pid, st.Cid, keyword, page.Total, page.Current, len(snapshots), time.Since(startedAt),
			)
			return snapshots
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
		query = query.Where("name LIKE ?", like)
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

	// 3. 写入 Redis 缓存
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

	// 2. 优先使用全内存片名索引快速搜索（1~2ms 级响应）
	idx := getOrLoadFilmSearchMemoryIndex(version)
	if idx != nil && len(idx.Items) > 0 {
		lowerKey := strings.ToLower(keyword)
		var matchedMids []int64
		for _, item := range idx.Items {
			if strings.Contains(item.LowerName, lowerKey) {
				matchedMids = append(matchedMids, item.Mid)
			}
		}

		page.Total = len(matchedMids)
		page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
		if page.PageCount <= 0 {
			page.PageCount = 1
		}

		var snapshots []model.FilmListSnapshot
		offset := getPageOffset(page)
		if offset < len(matchedMids) {
			end := offset + page.PageSize
			if end > len(matchedMids) {
				end = len(matchedMids)
			}
			pageMids := matchedMids[offset:end]
			snapshots = GetProjectedSnapshotsByMidsOrdered(version, pageMids)
		}
		if snapshots == nil {
			snapshots = []model.FilmListSnapshot{}
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

		log.Printf("[SearchFilm] 内存搜索完成 keyword=%q cache=MISS(MEMORY_HIT) total=%d page=%d size=%d cost=%s",
			keyword, page.Total, page.Current, len(snapshots), time.Since(startedAt))
		return snapshots
	}

	// 降级：仅查 name 字段，避免扫描 sub_title 的 TEXT 字段
	like := "%" + escapeLikePattern(keyword) + "%"
	query := db.Mdb.Model(&model.FilmListSnapshot{}).
		Where("snapshot_version = ? AND name LIKE ?", version, like)

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

	log.Printf("[SearchFilm] DB搜索完成 keyword=%q cache=MISS total=%d page=%d size=%d cost=%s",
		keyword, page.Total, page.Current, len(snapshots), time.Since(startedAt))
	return snapshots
}

func GetSearchPageReadModel(s model.SearchVo) []model.FilmIndex {
	startedAt := time.Now()
	page := ensurePage(s.Paging)

	query := db.Mdb.Model(&model.FilmIndex{}).Where("deleted_at IS NULL")
	if name := strings.TrimSpace(s.Name); name != "" {
		like := "%" + escapeLikePattern(name) + "%"
		query = query.Where("name LIKE ?", like)
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

