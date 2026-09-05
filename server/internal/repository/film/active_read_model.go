package film

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"runtime/debug"
	"slices"
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
	Mid         int64
	Hits        int32
	UpdateStamp int32
	PoolOffset  uint32
	Pid         int32
	Cid         int32
	Year        int32
	Score       float32
	NameLen     uint16
	CleanLen    uint16
	PyFullLen   uint16
	PyInitLen   uint16
	PyAltLen    uint16
	_           uint16
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
	StringPool           []byte
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

	// 双缓冲保护：如果新版本索引正在构建，但当前内存中已有有效索引（哪怕是已标记过期的上一版本），
	// 非阻塞加入或触发构建，同时立即返回当前可用索引，避免将在线 HTTP 请求阻塞 5 秒！
	if cur := activeFilmSearchIndex.Load(); cur != nil && len(cur.Items) > 0 {
		_ = searchIndexBuild.DoChan(version, func() (any, error) {
			return buildAndStoreSearchIndex(version)
		})
		return cur
	}

	// 冷启动无任何索引时，同步构建首个索引
	v, _, _ := searchIndexBuild.Do(version, func() (any, error) {
		return buildAndStoreSearchIndex(version)
	})
	idx, _ := v.(*filmSearchMemoryIndex)
	return idx
}

func buildAndStoreSearchIndex(version string) (*filmSearchMemoryIndex, error) {
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
	if m := activeFilmReadModel.Load(); m != nil && m.Version != "" && m.Version != version {
		return built, nil
	}
	activeFilmSearchIndex.Store(built)
	runtime.GC()
	debug.FreeOSMemory()
	return built, nil
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

	newIdx := buildSearchIndexFromRows(version, rows)
	rows = nil // 立即解除对临时切片引用，协助 GC 释放
	log.Printf("[ActiveReadModel] 内存搜索索引已构建 version=%s count=%d poolSize=%d scan=%s total=%s",
		version, len(newIdx.Items), len(newIdx.StringPool), scanCost, time.Since(buildStarted))
	return newIdx
}

var searchIndexBuildWg sync.WaitGroup

// WaitActiveFilmSearchIndexBuilt 等待正在异步构建的内存搜索索引协程执行完毕。
func WaitActiveFilmSearchIndexBuilt() {
	searchIndexBuildWg.Wait()
}

func LoadActiveFilmReadModel(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	activeFilmReadModelMu.Lock()
	defer activeFilmReadModelMu.Unlock()
	activeFilmReadModel.Store(&FilmReadModel{Version: version})
	searchIndexBuildWg.Add(1)
	go func(ver string) {
		defer searchIndexBuildWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[ActiveReadModel] 异步构建内存搜索索引发生异常: %v", r)
			}
		}()
		_, _, _ = searchIndexBuild.Do(ver, func() (any, error) {
			return buildAndStoreSearchIndex(ver)
		})
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
		if cur := activeFilmSearchIndex.Load(); cur != nil && len(cur.Items) > 0 {
			staleCopy := *cur
			staleCopy.Version = ""
			activeFilmSearchIndex.Store(&staleCopy)
		} else {
			activeFilmSearchIndex.Store(&filmSearchMemoryIndex{Version: ""})
		}
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

func (idx *filmSearchMemoryIndex) scoreOneItem(itemIndex int, q utils.QueryContext, pid, cid int64) scoredSearchHit {
	if itemIndex < 0 || itemIndex >= len(idx.Items) {
		return scoredSearchHit{}
	}
	item := &idx.Items[itemIndex]
	if pid > 0 && int64(item.Pid) != pid {
		return scoredSearchHit{}
	}
	if cid > 0 && int64(item.Cid) != cid {
		return scoredSearchHit{}
	}
	s := utils.ScoreFilmMatch(idx.asSearchItem(itemIndex), q)
	if s <= 0 {
		return scoredSearchHit{}
	}
	return scoredSearchHit{
		mid:         item.Mid,
		matchScore:  s,
		hits:        int64(item.Hits),
		score:       float64(item.Score),
		year:        int64(item.Year),
		updateStamp: int64(item.UpdateStamp),
	}
}

func scoreMemoryIndex(idx *filmSearchMemoryIndex, keyword string, sortField string, pid, cid int64) []scoredSearchHit {
	if idx == nil || len(idx.Items) == 0 {
		return nil
	}
	q := utils.BuildQueryContext(keyword)
	cands := idx.collectCandidates(q)

	matched := make([]scoredSearchHit, 0, 64)
	appendHit := func(itemIndex int) {
		hit := idx.scoreOneItem(itemIndex, q, pid, cid)
		if hit.mid != 0 {
			matched = append(matched, hit)
		}
	}

	if cands == nil {
		for i := range idx.Items {
			appendHit(i)
		}
	} else {
		for _, id := range cands {
			if int(id) < 0 || int(id) >= len(idx.Items) {
				continue
			}
			appendHit(int(id))
		}
	}

	slices.SortFunc(matched, func(a, b scoredSearchHit) int {
		if compareScoredHits(a, b, sortField) {
			return -1
		}
		return 1
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

var tagSearchSfGroup singleflight.Group

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

	val, err, _ := tagSearchSfGroup.Do(cacheKey, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var item tagSearchCacheItem
				if json.Unmarshal([]byte(data), &item) == nil {
					return item, nil
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
			return tagSearchCacheItem{}, err
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

		var snapshots []model.FilmListSnapshot
		offset := getPageOffset(page)
		if err := query.Select(snapshotSelectFields).Order(orderClause).Offset(offset).Limit(page.PageSize).Find(&snapshots).Error; err != nil {
			return tagSearchCacheItem{}, err
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

func cloneFilmListSnapshots(src []model.FilmListSnapshot) []model.FilmListSnapshot {
	if src == nil {
		return nil
	}
	out := make([]model.FilmListSnapshot, len(src))
	copy(out, src)
	return out
}

var searchSnapshotsSf singleflight.Group

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

	// 2. 并发防击穿：相同关键词搜索合并执行
	sfKey := fmt.Sprintf("v%s:%s:%s:p%d:s%d", version, keyword, sortField, page.Current, page.PageSize)
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

		// 优先使用内存倒排索引模糊搜索
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

			item := searchCacheItem{
				Total:     page.Total,
				PageCount: page.PageCount,
				Snapshots: snapshots,
			}
			if db.Rdb != nil {
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
			return item, nil
		}

		// 降级：仅查 name，避免扫描 sub_title / actor / director 的 TEXT 字段
		query := applyNameLikeFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version), keyword)

		var total int64
		if err := query.Count(&total).Error; err != nil {
			return searchCacheItem{Total: 0, PageCount: 1, Snapshots: []model.FilmListSnapshot{}}, nil
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
			return searchCacheItem{Total: 0, PageCount: 1, Snapshots: []model.FilmListSnapshot{}}, nil
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
					ttl = 1 * time.Minute // 空结果防穿透短缓存
				}
				_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), ttl).Err()
			}
		}

		log.Printf("[SearchFilm] DB搜索完成 keyword=%q sort=%q cache=MISS total=%d page=%d size=%d cost=%s",
			keyword, sortField, page.Total, page.Current, len(snapshots), time.Since(startedAt))
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

func GetSearchPageReadModel(s model.SearchVo) []model.FilmIndex {
	startedAt := time.Now()
	page := ensurePage(s.Paging)
	name := strings.TrimSpace(s.Name)
	version := strings.TrimSpace(GetActiveSnapshotVersion())
	if version == "" {
		if m := GetActiveFilmReadModel(); m != nil && m.Version != "" {
			version = m.Version
		}
	}

	// 当包含剧情(class_tag/plot)、地区(area)、语言(language)等复合条件时，
	// 因内存紧凑索引未收录这些字段，直接透传快照表查询，保证多维筛选结果准确
	hasComplexFilter := strings.TrimSpace(s.Plot) != "" || strings.TrimSpace(s.Area) != "" || strings.TrimSpace(s.Language) != ""

	// 1. 如果有搜索词且无复合文本/地区/语言过滤，优先走内存倒排搜索索引与极速过滤 (毫秒级响应)
	if name != "" && version != "" && !hasComplexFilter {
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

			// 直接在命中结果中分页切片
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

	// 2. 快照表 FilmListSnapshot 投影查询（含复合筛选、无搜索词或冷启动降级）
	if version != "" {
		query := db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version)
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
