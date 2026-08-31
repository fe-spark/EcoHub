package film

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

type FilmReadModel struct {
	Version string
}

type filmSearchIndexRow struct {
	Mid         int64
	Pid         int64
	Cid         int64
	Name        string
	Hits        int64
	Score       float64
	Year        int64
	UpdateStamp int64
}

type filmSearchMemoryItem struct {
	Mid               int64
	Pid               int64
	Cid               int64
	Hits              int64
	Score             float64
	Year              int64
	UpdateStamp       int64
	Name              string
	CleanName         string
	PinyinFull        string
	PinyinInitials    string
	PinyinInitialAlts string
}

type scoredSearchHit struct {
	mid         int64
	matchScore  int
	hits        int64
	score       float64
	year        int64
	updateStamp int64
}

type filmSearchMemoryIndex struct {
	Version              string
	Items                []filmSearchMemoryItem
	nameBigrams          map[string][]int32
	nameUnigrams         map[rune][]int32
	pinyinFullBigrams    map[string][]int32
	pinyinInitialBigrams map[string][]int32
}

var activeFilmReadModel atomic.Pointer[FilmReadModel]
var activeFilmReadModelMu sync.Mutex

var activeFilmSearchIndex atomic.Pointer[filmSearchMemoryIndex]
var activeFilmSearchIndexMu sync.Mutex
var searchIndexBuild singleflight.Group

func init() {
	activeFilmReadModel.Store(&FilmReadModel{Version: ""})
	activeFilmSearchIndex.Store(&filmSearchMemoryIndex{Version: ""})
}

func getOrLoadFilmSearchMemoryIndex(version string) *filmSearchMemoryIndex {
	if version == "" {
		return nil
	}
	if idx := loadedFilmSearchIndex(version); idx != nil {
		return idx
	}
	v, _, _ := searchIndexBuild.Do(version, func() (any, error) {
		if idx := loadedFilmSearchIndex(version); idx != nil {
			return idx, nil
		}
		built := buildFilmSearchMemoryIndex(version)
		if built == nil {
			return (*filmSearchMemoryIndex)(nil), nil
		}
		activeFilmSearchIndexMu.Lock()
		defer activeFilmSearchIndexMu.Unlock()
		if cur := loadedFilmSearchIndex(version); cur != nil {
			return cur, nil
		}
		if cur := activeFilmSearchIndex.Load(); cur != nil && cur.Version != version && len(cur.Items) > 0 {
			if m := activeFilmReadModel.Load(); m != nil && m.Version != "" && m.Version != version {
				return built, nil
			}
		}
		activeFilmSearchIndex.Store(built)
		return built, nil
	})
	idx, _ := v.(*filmSearchMemoryIndex)
	return idx
}

func loadedFilmSearchIndex(version string) *filmSearchMemoryIndex {
	idx := activeFilmSearchIndex.Load()
	if idx != nil && idx.Version == version && len(idx.Items) > 0 {
		return idx
	}
	return nil
}

func buildFilmSearchMemoryIndex(version string) *filmSearchMemoryIndex {
	if db.Mdb == nil {
		return nil
	}

	buildStarted := time.Now()
	var rows []filmSearchIndexRow
	if err := db.Mdb.Model(&model.FilmListSnapshot{}).
		Select("mid, pid, cid, name, hits, score, year, update_stamp").
		Where("snapshot_version = ?", version).
		Scan(&rows).Error; err != nil {
		log.Printf("[ActiveReadModel] 加载内存搜索索引失败: %v", err)
		return nil
	}
	scanCost := time.Since(buildStarted)
	log.Printf("[ActiveReadModel] 开始构建内存搜索索引 version=%s rows=%d scan=%s", version, len(rows), scanCost)

	items := parallelBuildItems(rows)
	newIdx := &filmSearchMemoryIndex{
		Version: version,
		Items:   items,
	}
	newIdx.buildInverted()
	log.Printf("[ActiveReadModel] 内存搜索索引已构建 version=%s count=%d scan=%s total=%s",
		version, len(items), scanCost, time.Since(buildStarted))
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
		runtime.GC()
		debug.FreeOSMemory()
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

// InvalidateActiveFilmSearchIndex 增量发布后重置内存搜索索引并在后台异步重建，保持活跃读模型 Version 处于有效状态。
func InvalidateActiveFilmSearchIndex(version string) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version != "" {
		activeFilmSearchIndex.Store(&filmSearchMemoryIndex{Version: ""})
		if err := LoadActiveFilmReadModel(version); err != nil {
			log.Printf("[ActiveReadModel] 重载读模型失败 version=%s: %v", version, err)
		}
	} else {
		ClearActiveFilmReadModel()
	}
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

func compareScoredHits(a, b scoredSearchHit, sortField string) bool {
	switch sortField {
	case "hits":
		if a.hits != b.hits {
			return a.hits > b.hits
		}
	case "latest":
		if a.updateStamp != b.updateStamp {
			return a.updateStamp > b.updateStamp
		}
	case "year":
		if a.year != b.year {
			return a.year > b.year
		}
	case "score":
		if a.score != b.score {
			return a.score > b.score
		}
	}
	if a.matchScore != b.matchScore {
		return a.matchScore > b.matchScore
	}
	if a.hits != b.hits {
		return a.hits > b.hits
	}
	if a.year != b.year {
		return a.year > b.year
	}
	if a.updateStamp != b.updateStamp {
		return a.updateStamp > b.updateStamp
	}
	return a.mid > b.mid
}

func scoreOneItem(item *filmSearchMemoryItem, q utils.QueryContext, pid, cid int64) scoredSearchHit {
	if pid > 0 && item.Pid != pid {
		return scoredSearchHit{}
	}
	if cid > 0 && item.Cid != cid {
		return scoredSearchHit{}
	}
	s := utils.ScoreFilmMatch(item.asSearchItem(), q)
	if s <= 0 {
		return scoredSearchHit{}
	}
	return scoredSearchHit{
		mid:         item.Mid,
		matchScore:  s,
		hits:        item.Hits,
		score:       item.Score,
		year:        item.Year,
		updateStamp: item.UpdateStamp,
	}
}

func scoreMemoryIndex(idx *filmSearchMemoryIndex, keyword string, sortField string, pid, cid int64) []scoredSearchHit {
	if idx == nil || len(idx.Items) == 0 {
		return nil
	}
	q := utils.BuildQueryContext(keyword)
	cands := idx.collectCandidates(q)

	matched := make([]scoredSearchHit, 0, 64)
	appendHit := func(item *filmSearchMemoryItem) {
		hit := scoreOneItem(item, q, pid, cid)
		if hit.mid != 0 {
			matched = append(matched, hit)
		}
	}

	if cands == nil {
		for i := range idx.Items {
			appendHit(&idx.Items[i])
		}
	} else {
		for _, id := range cands {
			if int(id) < 0 || int(id) >= len(idx.Items) {
				continue
			}
			appendHit(&idx.Items[id])
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return compareScoredHits(matched[i], matched[j], sortField)
	})
	return matched
}

func pageMidsFromHits(hits []scoredSearchHit, page *dto.Page) []int64 {
	page.Total = len(hits)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	offset := getPageOffset(page)
	if offset >= len(hits) {
		return nil
	}
	end := offset + page.PageSize
	if end > len(hits) {
		end = len(hits)
	}
	mids := make([]int64, end-offset)
	for i, h := range hits[offset:end] {
		mids[i] = h.mid
	}
	return mids
}

func applyNameLikeFilter(query *gorm.DB, keyword string) *gorm.DB {
	tokens := utils.ExtractSearchTokens(keyword)
	if len(tokens) == 0 {
		return query.Where("name LIKE ?", "%"+escapeLikePattern(keyword)+"%")
	}
	for _, tok := range tokens {
		query = query.Where("name LIKE ?", "%"+escapeLikePattern(tok)+"%")
	}
	return query
}

func snapshotSortOrderClause(sortField string, keywordSearch bool) string {
	switch sortField {
	case "hits":
		return "hits DESC, id DESC"
	case "latest":
		return "update_stamp DESC, id DESC"
	case "year":
		return "year DESC, id DESC"
	case "score":
		return "score DESC, id DESC"
	default:
		if keywordSearch {
			return "hits DESC, year DESC, update_stamp DESC, id DESC"
		}
		return "update_stamp DESC, id DESC"
	}
}

const (
	tagSearchCacheTTL    = 3 * time.Minute
	snapshotSelectFields = "id, snapshot_version, mid, pid, cid, c_name, name, score, hits, update_stamp, remarks, state, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture, year, class_tag, area, language"
)

type tagSearchCacheItem struct {
	Total     int                      `json:"total"`
	PageCount int                      `json:"page_count"`
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
	st.Sort = utils.NormalizeSearchSortField(st.Sort)
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

	// 2. 若带有搜索关键词且无时间限制，优先走内存倒排搜索索引
	if keyword != "" && recentHours == 0 {
		idx := getOrLoadFilmSearchMemoryIndex(version)
		if idx != nil && len(idx.Items) > 0 {
			matched := scoreMemoryIndex(idx, keyword, st.Sort, st.Pid, st.Cid)
			var snapshots []model.FilmListSnapshot
			if pageMids := pageMidsFromHits(matched, page); len(pageMids) > 0 {
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
				"[ProvideVod] 模糊内存搜索完成 pid=%d cid=%d keyword=%q cache=MISS(MEMORY_HIT) total=%d page=%d size=%d cost=%s",
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
		query = applyNameLikeFilter(query, keyword)
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

	orderClause := snapshotSortOrderClause(st.Sort, keyword != "")

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
	Total     int                      `json:"total"`
	PageCount int                      `json:"page_count"`
	Snapshots []model.FilmListSnapshot `json:"snapshots"`
}

func SearchSnapshotsByKeywordReadModel(version string, keyword string, page *dto.Page) []model.FilmListSnapshot {
	return SearchSnapshotsByKeywordAndSortReadModel(version, keyword, "", page)
}

func SearchSnapshotsByKeywordAndSortReadModel(version string, keyword string, sortField string, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	keyword = strings.TrimSpace(keyword)
	sortField = utils.NormalizeSearchSortField(sortField)
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
	cacheKey := fmt.Sprintf("EcoHub:search:v%s:%s:%s:p%d:s%d", version, keyword, sortField, page.Current, page.PageSize)
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

	// 2. 优先使用内存倒排索引模糊搜索
	idx := getOrLoadFilmSearchMemoryIndex(version)
	if idx != nil && len(idx.Items) > 0 {
		matched := scoreMemoryIndex(idx, keyword, sortField, 0, 0)
		var snapshots []model.FilmListSnapshot
		if pageMids := pageMidsFromHits(matched, page); len(pageMids) > 0 {
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

		log.Printf("[SearchFilm] 内存模糊搜索完成 keyword=%q sort=%q cache=MISS(MEMORY_HIT) total=%d page=%d size=%d cost=%s",
			keyword, sortField, page.Total, page.Current, len(snapshots), time.Since(startedAt))
		return snapshots
	}

	// 降级：仅查 name，避免扫描 sub_title / actor / director 的 TEXT 字段
	query := applyNameLikeFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version), keyword)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return []model.FilmListSnapshot{}
	}
	page.Total = int(total)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	orderClause := snapshotSortOrderClause(sortField, true)

	var snapshots []model.FilmListSnapshot
	offset := getPageOffset(page)
	if err := query.Select(snapshotSelectFields).Order(orderClause).Offset(offset).Limit(page.PageSize).Find(&snapshots).Error; err != nil {
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

	log.Printf("[SearchFilm] DB搜索完成 keyword=%q sort=%q cache=MISS total=%d page=%d size=%d cost=%s",
		keyword, sortField, page.Total, page.Current, len(snapshots), time.Since(startedAt))
	return snapshots
}

func GetSearchPageReadModel(s model.SearchVo) []model.FilmIndex {
	startedAt := time.Now()
	page := ensurePage(s.Paging)
	name := strings.TrimSpace(s.Name)
	version := strings.TrimSpace(GetActiveSnapshotVersion())

	// 1. 如果有搜索词，优先走内存倒排搜索索引与极速过滤 (毫秒级响应)
	if name != "" && version != "" {
		idx := getOrLoadFilmSearchMemoryIndex(version)
		if idx != nil && len(idx.Items) > 0 {
			matched := scoreMemoryIndex(idx, name, "latest", s.Pid, s.Cid)

			// 内存过滤年份与更新时间范围
			filteredHits := make([]scoredSearchHit, 0, len(matched))
			for _, hit := range matched {
				if s.Year > 0 && hit.year != s.Year {
					continue
				}
				if s.BeginTime > 0 && hit.updateStamp < s.BeginTime {
					continue
				}
				if s.EndTime > 0 && hit.updateStamp > s.EndTime {
					continue
				}
				filteredHits = append(filteredHits, hit)
			}

			// 如果带有次级标签筛选 (plot / area / language)，做快照级后置过滤
			if s.Plot != "" || s.Area != "" || s.Language != "" {
				allCandidateMids := make([]int64, len(filteredHits))
				for i, h := range filteredHits {
					allCandidateMids[i] = h.mid
				}
				allSnapshots := GetProjectedSnapshotsByMidsOrdered(version, allCandidateMids)
				finalSnapshots := make([]model.FilmListSnapshot, 0, len(allSnapshots))
				for _, snap := range allSnapshots {
					if s.Plot != "" && !strings.Contains(snap.ClassTag, s.Plot) {
						continue
					}
					if s.Area != "" && strings.TrimSpace(snap.Area) != s.Area {
						continue
					}
					if s.Language != "" && strings.TrimSpace(snap.Language) != s.Language {
						continue
					}
					finalSnapshots = append(finalSnapshots, snap)
				}

				page.Total = len(finalSnapshots)
				page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
				if page.PageCount <= 0 {
					page.PageCount = 1
				}

				offset := getPageOffset(page)
				if offset >= len(finalSnapshots) {
					return []model.FilmIndex{}
				}
				end := offset + page.PageSize
				if end > len(finalSnapshots) {
					end = len(finalSnapshots)
				}
				paged := finalSnapshots[offset:end]
				log.Printf("[ManageFilmSearch] 内存模糊过滤完成 name=%q pid=%d cid=%d total=%d page=%d size=%d cost=%s",
					name, s.Pid, s.Cid, page.Total, page.Current, len(paged), time.Since(startedAt))
				return convertSnapshotsToFilmIndexes(paged)
			}

			// 无次级标签筛选，直接在命中结果中分页切片
			pageMids := pageMidsFromHits(filteredHits, page)
			if len(pageMids) == 0 {
				return []model.FilmIndex{}
			}

			snapshots := GetProjectedSnapshotsByMidsOrdered(version, pageMids)
			log.Printf("[ManageFilmSearch] 内存模糊检索完成 name=%q pid=%d cid=%d total=%d page=%d size=%d cost=%s",
				name, s.Pid, s.Cid, page.Total, page.Current, len(snapshots), time.Since(startedAt))
			return convertSnapshotsToFilmIndexes(snapshots)
		}
	}

	// 2. 无搜索词时，优先走轻量只读快照表 FilmListSnapshot 投影查询
	if version != "" {
		query := db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version)
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
		if s.BeginTime > 0 {
			query = query.Where("update_stamp >= ?", s.BeginTime)
		}
		if s.EndTime > 0 {
			query = query.Where("update_stamp <= ?", s.EndTime)
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

		var snapshots []model.FilmListSnapshot
		offset := getPageOffset(page)
		if err := query.Select(snapshotSelectFields).Order("update_stamp DESC, id DESC").Offset(offset).Limit(page.PageSize).Find(&snapshots).Error; err != nil {
			return []model.FilmIndex{}
		}

		log.Printf(
			"[ManageFilmSearch] 快照检索完成 name=%q pid=%d cid=%d total=%d page=%d size=%d cost=%s",
			s.Name,
			s.Pid,
			s.Cid,
			page.Total,
			page.Current,
			page.PageSize,
			time.Since(startedAt),
		)
		return convertSnapshotsToFilmIndexes(snapshots)
	}

	// 3. 兜底降级：快照未初始化时查询底层 FilmIndex
	query := db.Mdb.Model(&model.FilmIndex{}).Where("deleted_at IS NULL")
	if name != "" {
		query = applyNameLikeFilter(query, name)
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
	if s.BeginTime > 0 {
		query = query.Where("update_stamp >= ?", s.BeginTime)
	}
	if s.EndTime > 0 {
		query = query.Where("update_stamp <= ?", s.EndTime)
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
		"[ManageFilmSearch] 降级检索完成 name=%q pid=%d cid=%d total=%d page=%d size=%d cost=%s",
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

func convertSnapshotsToFilmIndexes(snapshots []model.FilmListSnapshot) []model.FilmIndex {
	if len(snapshots) == 0 {
		return []model.FilmIndex{}
	}
	result := make([]model.FilmIndex, len(snapshots))
	for i, snap := range snapshots {
		result[i] = model.FilmIndex{
			Model: gorm.Model{
				ID:        snap.ID,
				CreatedAt: snap.CreatedAt,
				UpdatedAt: snap.UpdatedAt,
			},
			FilmIndexIdentity: model.FilmIndexIdentity{
				Mid:        snap.Mid,
				ContentKey: snap.ContentKey,
				SourceId:   snap.SourceId,
				DbId:       snap.DbId,
			},
			FilmIndexCategory: model.FilmIndexCategory{
				Cid:              snap.Cid,
				Pid:              snap.Pid,
				RootCategoryKey:  snap.RootCategoryKey,
				CategoryKey:      snap.CategoryKey,
				OriginalCategory: snap.OriginalCategory,
				CName:            snap.CName,
			},
			FilmIndexContent: model.FilmIndexContent{
				SeriesKey:          snap.SeriesKey,
				Name:               snap.Name,
				SubTitle:           snap.SubTitle,
				ClassTag:           snap.ClassTag,
				Area:               snap.Area,
				Language:           snap.Language,
				Year:               snap.Year,
				Initial:            snap.Initial,
				Score:              snap.Score,
				UpdateStamp:        snap.UpdateStamp,
				Hits:               snap.Hits,
				State:              snap.State,
				Remarks:            snap.Remarks,
				Picture:            snap.Picture,
				PictureSlide:       snap.PictureSlide,
				CustomPicture:      snap.CustomPicture,
				CustomPictureSlide: snap.CustomPictureSlide,
				IsCustomPicture:    snap.IsCustomPicture,
				Actor:              snap.Actor,
				Director:           snap.Director,
				Blurb:              snap.Blurb,
			},
			FilmIndexVersion: model.FilmIndexVersion{
				CollectStamp:    snap.CollectStamp,
				CategoryVersion: snap.CategoryVersion,
				RuleVersion:     snap.RuleVersion,
			},
			FilmIndexDerived: model.FilmIndexDerived{
				PlayFromSummary: snap.PlayFromSummary,
			},
		}
	}
	return result
}

func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

