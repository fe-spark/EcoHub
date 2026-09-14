package film

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/config"
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

type FilmSearchMeta struct {
	Mid  int64
	Pid  int64
	Cid  int64
	Item utils.FilmSearchItem
}

type filmSearchMetaIndex struct {
	Version string
	Items   []FilmSearchMeta
}

var activeFilmReadModel atomic.Pointer[FilmReadModel]
var activeFilmReadModelMu sync.Mutex

var activeFilmSearchMetas atomic.Pointer[filmSearchMetaIndex]
var activeFilmSearchMetasMu sync.Mutex
var searchMetaBuildSf singleflight.Group
var searchMetaBuildWg sync.WaitGroup

func init() {
	activeFilmReadModel.Store(&FilmReadModel{Version: ""})
}

func WaitActiveFilmSearchIndexBuilt() {
	searchMetaBuildWg.Wait()
}

func loadFilmSearchMetaIndex(version string) *filmSearchMetaIndex {
	version = strings.TrimSpace(version)
	if version == "" || db.Mdb == nil {
		return nil
	}
	if cur := activeFilmSearchMetas.Load(); cur != nil && cur.Version == version {
		return cur
	}
	val, err, _ := searchMetaBuildSf.Do(version, func() (any, error) {
		if cur := activeFilmSearchMetas.Load(); cur != nil && cur.Version == version {
			return cur, nil
		}
		type dbMetaRow struct {
			Mid         int64
			Pid         int64
			Cid         int64
			Name        string
			Hits        int64
			Score       float64
			Year        int64
			UpdateStamp int64
		}
		var rows []dbMetaRow
		if err := db.Mdb.Model(&model.FilmListSnapshot{}).
			Select("mid, pid, cid, name, hits, score, year, update_stamp").
			Where("snapshot_version = ?", version).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		items := make([]FilmSearchMeta, len(rows))
		for i, r := range rows {
			item := utils.FilmSearchItem{
				Mid:         r.Mid,
				Name:        r.Name,
				Hits:        r.Hits,
				Score:       r.Score,
				Year:        r.Year,
				UpdateStamp: r.UpdateStamp,
			}
			utils.FillSearchDerivedFields(&item)
			items[i] = FilmSearchMeta{
				Mid:  r.Mid,
				Pid:  r.Pid,
				Cid:  r.Cid,
				Item: item,
			}
		}
		idx := &filmSearchMetaIndex{
			Version: version,
			Items:   items,
		}
		activeFilmSearchMetasMu.Lock()
		activeFilmSearchMetas.Store(idx)
		activeFilmSearchMetasMu.Unlock()
		return idx, nil
	})
	if err != nil || val == nil {
		return nil
	}
	return val.(*filmSearchMetaIndex)
}

type scoredMetaHit struct {
	mid         int64
	matchScore  int
	hits        int64
	score       float64
	year        int64
	updateStamp int64
}

func searchFilmMetas(idx *filmSearchMetaIndex, keyword, sortField string, pid, cid int64) []scoredMetaHit {
	if idx == nil || len(idx.Items) == 0 {
		return nil
	}
	q := utils.BuildQueryContext(keyword)
	matches := make([]scoredMetaHit, 0, 64)
	for i := range idx.Items {
		meta := &idx.Items[i]
		if pid > 0 && meta.Pid != pid {
			continue
		}
		if cid > 0 && meta.Cid != cid {
			continue
		}
		score := utils.ScoreFilmMatch(meta.Item, q)
		if score <= 0 {
			continue
		}
		matches = append(matches, scoredMetaHit{
			mid:         meta.Mid,
			matchScore:  score,
			hits:        meta.Item.Hits,
			score:       meta.Item.Score,
			year:        meta.Item.Year,
			updateStamp: meta.Item.UpdateStamp,
		})
	}
	if len(matches) == 0 {
		return matches
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
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
	})
	return matches
}

func pageMidsFromMetaHits(hits []scoredMetaHit, page *dto.Page) []int64 {
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
	mids := make([]int64, 0, end-offset)
	for _, h := range hits[offset:end] {
		mids = append(mids, h.mid)
	}
	return mids
}

func LoadActiveFilmReadModel(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	activeFilmReadModelMu.Lock()
	defer activeFilmReadModelMu.Unlock()
	activeFilmReadModel.Store(&FilmReadModel{Version: version})
	activeFilmSearchMetasMu.Lock()
	activeFilmSearchMetas.Store(nil)
	activeFilmSearchMetasMu.Unlock()
	if version != "" {
		searchMetaBuildWg.Add(1)
		go func(ver string) {
			defer searchMetaBuildWg.Done()
			_ = loadFilmSearchMetaIndex(ver)
		}(version)
	}
	log.Printf("[ActiveReadModel] 活跃读模型已就绪 version=%s", version)
	return nil
}

func RefreshActiveProjectedReadModel() error {
	RefreshAccessDataCaches()
	return nil
}

func ApplyActiveFilmReadModelSnapshots(version string, snapshots []model.FilmListSnapshot, deletedMIDs []int64) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	activeFilmSearchMetasMu.Lock()
	activeFilmSearchMetas.Store(nil)
	activeFilmSearchMetasMu.Unlock()
	if version != "" {
		searchMetaBuildWg.Add(1)
		go func(ver string) {
			defer searchMetaBuildWg.Done()
			_ = loadFilmSearchMetaIndex(ver)
		}(version)
	}
	RefreshAccessDataCaches()
	return nil
}

func ClearActiveFilmReadModel() {
	activeFilmReadModel.Store(&FilmReadModel{Version: ""})
	activeFilmSearchMetasMu.Lock()
	activeFilmSearchMetas.Store(nil)
	activeFilmSearchMetasMu.Unlock()
}

// InvalidateActiveFilmSearchIndex 增量发布后重载活跃读模型版本
func InvalidateActiveFilmSearchIndex(version string) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version != "" {
		if err := LoadActiveFilmReadModel(version); err != nil {
			log.Printf("[ActiveReadModel] 重载读模型失败 version=%s: %v", version, err)
		}
	} else {
		ClearActiveFilmReadModel()
	}
}

// RemoveMidsFromActiveFilmSearchIndex 增量从内存搜索元数据索引中剔除指定 mid，避免全量重建耗时
func RemoveMidsFromActiveFilmSearchIndex(version string, mids []int64) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" || len(mids) == 0 {
		return
	}

	delSet := make(map[int64]struct{}, len(mids))
	for _, id := range mids {
		if id > 0 {
			delSet[id] = struct{}{}
		}
	}
	if len(delSet) == 0 {
		return
	}

	activeFilmSearchMetasMu.Lock()
	cur := activeFilmSearchMetas.Load()
	if cur == nil || cur.Version != version {
		activeFilmSearchMetasMu.Unlock()
		loadFilmSearchMetaIndex(version)
		activeFilmSearchMetasMu.Lock()
		cur = activeFilmSearchMetas.Load()
	}
	defer activeFilmSearchMetasMu.Unlock()

	if cur == nil || cur.Version != version || len(cur.Items) == 0 {
		return
	}

	newItems := make([]FilmSearchMeta, 0, len(cur.Items))
	removed := 0
	for _, item := range cur.Items {
		if _, exists := delSet[item.Mid]; exists {
			removed++
			continue
		}
		newItems = append(newItems, item)
	}
	if removed > 0 {
		activeFilmSearchMetas.Store(&filmSearchMetaIndex{
			Version: version,
			Items:   newItems,
		})
		log.Printf("[ActiveReadModel] 内存索引增量剔除 mids=%d removed=%d remaining=%d", len(mids), removed, len(newItems))
	}
}

// UpsertMidsToActiveFilmSearchIndex 增量更新或新增指定 mid 的搜索元数据索引，避免全量重建
func UpsertMidsToActiveFilmSearchIndex(version string, mids []int64) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" || len(mids) == 0 || db.Mdb == nil {
		return
	}

	midSet := make(map[int64]struct{}, len(mids))
	cleanMids := make([]int64, 0, len(mids))
	for _, id := range mids {
		if id > 0 {
			if _, exists := midSet[id]; !exists {
				midSet[id] = struct{}{}
				cleanMids = append(cleanMids, id)
			}
		}
	}
	if len(cleanMids) == 0 {
		return
	}

	type dbMetaRow struct {
		Mid         int64
		Pid         int64
		Cid         int64
		Name        string
		Hits        int64
		Score       float64
		Year        int64
		UpdateStamp int64
	}
	const batchSize = 500
	var allRows []dbMetaRow
	for i := 0; i < len(cleanMids); i += batchSize {
		end := i + batchSize
		if end > len(cleanMids) {
			end = len(cleanMids)
		}
		var batchRows []dbMetaRow
		if err := db.Mdb.Model(&model.FilmListSnapshot{}).
			Select("mid, pid, cid, name, hits, score, year, update_stamp").
			Where("snapshot_version = ? AND mid IN ?", version, cleanMids[i:end]).
			Find(&batchRows).Error; err != nil {
			log.Printf("[ActiveReadModel] UpsertMidsToActiveFilmSearchIndex 查询快照失败 version=%s: %v", version, err)
			return
		}
		allRows = append(allRows, batchRows...)
	}

	upsertMap := make(map[int64]FilmSearchMeta, len(allRows))
	for _, r := range allRows {
		item := utils.FilmSearchItem{
			Mid:         r.Mid,
			Name:        r.Name,
			Hits:        r.Hits,
			Score:       r.Score,
			Year:        r.Year,
			UpdateStamp: r.UpdateStamp,
		}
		utils.FillSearchDerivedFields(&item)
		upsertMap[r.Mid] = FilmSearchMeta{
			Mid:  r.Mid,
			Pid:  r.Pid,
			Cid:  r.Cid,
			Item: item,
		}
	}

	activeFilmSearchMetasMu.Lock()
	cur := activeFilmSearchMetas.Load()
	if cur == nil || cur.Version != version {
		activeFilmSearchMetasMu.Unlock()
		loadFilmSearchMetaIndex(version)
		activeFilmSearchMetasMu.Lock()
		cur = activeFilmSearchMetas.Load()
	}
	defer activeFilmSearchMetasMu.Unlock()

	if cur == nil || cur.Version != version {
		return
	}

	newItems := make([]FilmSearchMeta, 0, len(cur.Items)+len(upsertMap))
	seenMids := make(map[int64]struct{}, len(upsertMap))
	for _, it := range cur.Items {
		if updated, exists := upsertMap[it.Mid]; exists {
			newItems = append(newItems, updated)
			seenMids[it.Mid] = struct{}{}
		} else if _, isDeleted := midSet[it.Mid]; isDeleted {
			// 在传入的 mids 中但快照中已不存在（已删除/失效），从内存索引中剔除，防止幽灵数据残留
			continue
		} else {
			newItems = append(newItems, it)
		}
	}
	for mid, it := range upsertMap {
		if _, seen := seenMids[mid]; !seen {
			newItems = append(newItems, it)
		}
	}

	activeFilmSearchMetas.Store(&filmSearchMetaIndex{
		Version: version,
		Items:   newItems,
	})
	log.Printf("[ActiveReadModel] 内存索引增量更新/剔除 mids=%d snapshot_found=%d total=%d", len(cleanMids), len(upsertMap), len(newItems))
}

type searchCacheVerState struct {
	version   string
	updatedAt time.Time
}

var searchCacheVer atomic.Pointer[searchCacheVerState]
var searchVerSeq uint64

const searchVerCacheTTL = 1 * time.Second

func BumpSearchCacheVersion() {
	seq := atomic.AddUint64(&searchVerSeq, 1)
	newVer := fmt.Sprintf("%d_%d", time.Now().UnixNano(), seq)
	if db.Rdb != nil {
		db.Rdb.Set(db.Cxt, config.SearchCacheVersionKey, newVer, 0)
	}
	searchCacheVer.Store(&searchCacheVerState{
		version:   newVer,
		updatedAt: time.Now(),
	})
}

func GetSearchCacheVersion() string {
	cur := searchCacheVer.Load()
	if cur != nil && cur.version != "" && time.Since(cur.updatedAt) < searchVerCacheTTL {
		return cur.version
	}

	if db.Rdb == nil {
		if cur != nil && cur.version != "" {
			return cur.version
		}
		newVer := fmt.Sprintf("%d", time.Now().UnixNano())
		searchCacheVer.Store(&searchCacheVerState{
			version:   newVer,
			updatedAt: time.Now(),
		})
		return newVer
	}

	version, err := db.Rdb.Get(db.Cxt, config.SearchCacheVersionKey).Result()
	if err == nil && version != "" {
		searchCacheVer.Store(&searchCacheVerState{
			version:   version,
			updatedAt: time.Now(),
		})
		return version
	}

	version = fmt.Sprintf("%d", time.Now().UnixNano())
	if set, _ := db.Rdb.SetNX(db.Cxt, config.SearchCacheVersionKey, version, 0).Result(); !set {
		if curVer, err := db.Rdb.Get(db.Cxt, config.SearchCacheVersionKey).Result(); err == nil && curVer != "" {
			version = curVer
		}
	}
	searchCacheVer.Store(&searchCacheVerState{
		version:   version,
		updatedAt: time.Now(),
	})
	return version
}

// ResetSearchCacheVersionForTest 重置内存缓存版本（仅供测试隔离使用）
func ResetSearchCacheVersionForTest() {
	searchCacheVer.Store(nil)
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

var provideSnapshotsSf singleflight.Group

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
		offset := getPageOffset(page)

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
		query := applyNameLikeFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version), keyword)

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
		offset := getPageOffset(page)

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

	// 1. 快照表 FilmListSnapshot 投影查询
	if version != "" && db.Mdb != nil {
		hasComplexFilter := strings.TrimSpace(s.Plot) != "" || strings.TrimSpace(s.Area) != "" || strings.TrimSpace(s.Language) != ""
		if name != "" && !hasComplexFilter {
			idx := loadFilmSearchMetaIndex(version)
			if idx != nil && len(idx.Items) > 0 {
				hits := searchFilmMetas(idx, name, "latest", s.Pid, s.Cid)
				filteredHits := make([]scoredMetaHit, 0, len(hits))
				for _, h := range hits {
					if s.Year > 0 && h.year != s.Year {
						continue
					}
					if s.BeginTime > 0 && h.updateStamp < s.BeginTime {
						continue
					}
					if s.EndTime > 0 && h.updateStamp > s.EndTime {
						continue
					}
					filteredHits = append(filteredHits, h)
				}
				pageMids := pageMidsFromMetaHits(filteredHits, page)
				var snapshots []model.FilmListSnapshot
				if len(pageMids) > 0 {
					snapshots = GetProjectedSnapshotsByMidsOrdered(version, pageMids)
				}
				log.Printf(
					"[ManageFilmSearch] 内存检索完成 name=%q pid=%d cid=%d total=%d page=%d size=%d cost=%s",
					s.Name,
					s.Pid,
					s.Cid,
					page.Total,
					page.Current,
					len(snapshots),
					time.Since(startedAt),
				)
				return convertSnapshotsToFilmIndexes(snapshots)
			}
		}

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

		offset := getPageOffset(page)
		var ids []uint
		if err := query.Select("id").Order("update_stamp DESC, id DESC").Offset(offset).Limit(page.PageSize).Pluck("id", &ids).Error; err != nil {
			return []model.FilmIndex{}
		}

		var snapshots []model.FilmListSnapshot
		if len(ids) > 0 {
			if err := db.Mdb.Model(&model.FilmListSnapshot{}).Select(snapshotSelectFields).Where("id IN ?", ids).Order("update_stamp DESC, id DESC").Find(&snapshots).Error; err != nil {
				return []model.FilmIndex{}
			}
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

	// 2. 兜底降级：快照未初始化时查询底层 FilmIndex
	if db.Mdb == nil {
		return []model.FilmIndex{}
	}
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
