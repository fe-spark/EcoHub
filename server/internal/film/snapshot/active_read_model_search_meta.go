package snapshot

import (
	"golang.org/x/sync/singleflight"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"server/internal/film/catalog"
	"server/internal/film/shared"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/taxonomy"
	"server/internal/utils"
)

type FilmSearchMeta struct {
	Mid             int64
	Pid             int64
	Cid             int64
	FirstSourceId   string
	PlaySources     []string
	Area            string
	Language        string
	ClassTag        string
	CategoryKey     string
	RootCategoryKey string
	Item            utils.FilmSearchItem
}

type filmSearchMetaIndex struct {
	Version     string
	PrimaryID   string
	Items       []*FilmSearchMeta
	ByUpdate    []int
	Pos         map[int64]int
	HotPools    map[string][]int64
	YearPools   map[string][]int64
	RecentPools map[string][]int64
	SourceHot   map[string][]int64
	ByCategory  map[string][]int
	SearchPosts *searchPostings
}

var activeFilmSearchMetas atomic.Pointer[filmSearchMetaIndex]
var activeFilmSearchMetasMu sync.Mutex
var searchMetaBuildSf singleflight.Group
var searchMetaBuildWg sync.WaitGroup
var isSearchIndexBuilding atomic.Bool

// IsSearchIndexBuilding 返回当前是否正在加载或重建检索倒排索引
func IsSearchIndexBuilding() bool {
	return isSearchIndexBuilding.Load()
}

func WaitActiveFilmSearchIndexBuilt() {
	searchMetaBuildWg.Wait()
}

type scoredMetaHit struct {
	mid         int64
	matchScore  int
	hits        int64
	score       float64
	year        int64
	updateStamp int64
}

// SourceCategoryVisible 当前站这部片子的分类对前台是否可见。隐藏组、隐藏子类、隐藏父级都为 false。
// 循环里批量判断请用 SourceCategoryVisibility，避免每条都读 Redis。
func SourceCategoryVisible(sourceID, categoryKey, rootKey string, cid, pid int64) bool {
	return SourceCategoryVisibility(sourceID)(categoryKey, rootKey, cid, pid)
}

// SourceCategoryVisibility 预先加载一次当前站的分类可见性，返回可在循环里反复调用的判断函数。
func SourceCategoryVisibility(sourceID string) func(categoryKey, rootKey string, cid, pid int64) bool {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return func(_, _ string, cid, pid int64) bool {
			return taxonomy.IsCategoryVisible(cid, pid)
		}
	}
	typeVisible := catalog.PublicSourceTypeVisibility(sourceID)
	return func(categoryKey, rootKey string, cid, pid int64) bool {
		if sid, typeID, ok := taxonomy.ParseSourceCategoryKey(categoryKey); ok && sid == sourceID {
			return typeVisible(typeID)
		}
		if sid, typeID, ok := taxonomy.ParseSourceCategoryKey(rootKey); ok && sid == sourceID {
			return typeVisible(typeID)
		}
		return taxonomy.IsCategoryVisible(cid, pid)
	}
}

func searchFilmMetas(idx *filmSearchMetaIndex, keyword, sortField, sourceID string, pid, cid int64) []scoredMetaHit {
	if idx == nil || len(idx.Items) == 0 {
		return nil
	}
	q := utils.BuildQueryContext(keyword)
	positions, narrowed := idx.SearchPosts.lookup(q)
	matches := make([]scoredMetaHit, 0, 64)
	categoryVisible := SourceCategoryVisibility(sourceID)
	visit := func(meta *FilmSearchMeta) {
		if !categoryVisible(meta.CategoryKey, meta.RootCategoryKey, meta.Cid, meta.Pid) {
			return
		}
		if pid > 0 && meta.Pid != pid {
			return
		}
		if cid > 0 && meta.Cid != cid {
			return
		}
		score := utils.ScoreFilmMatch(meta.Item, q)
		if score <= 0 {
			return
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
	if narrowed {
		for _, pos := range positions {
			if pos < 0 || int(pos) >= len(idx.Items) {
				continue
			}
			visit(idx.Items[pos])
		}
	} else {
		for i := range idx.Items {
			visit(idx.Items[i])
		}
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
	offset := shared.PageOffset(page)
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
		if _, err := loadFilmSearchMetaIndex(version); err != nil {
			log.Printf("[ActiveReadModel] 检索索引加载失败 version=%s: %v", version, err)
			return
		}
		activeFilmSearchMetasMu.Lock()
		cur = activeFilmSearchMetas.Load()
	}
	rebuild := false
	defer func() {
		activeFilmSearchMetasMu.Unlock()
		if rebuild {
			RequestActiveFilmSearchIndexRebuild()
		}
	}()

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
		newItems = append(newItems, *item)
	}
	if removed == 0 {
		return
	}
	if cur.PrimaryID != currentPrimarySourceID() {
		rebuild = true
		log.Printf("[ActiveReadModel] 剔除时主站已变，放弃封口并重建 mids=%d primary=%s", len(mids), cur.PrimaryID)
		return
	}
	idx := sealSearchMetaIndex(version, newItems)
	idx.PrimaryID = cur.PrimaryID
	activeFilmSearchMetas.Store(idx)
	log.Printf("[ActiveReadModel] 内存索引增量剔除 mids=%d removed=%d remaining=%d", len(mids), removed, len(newItems))
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

	activeFilmSearchMetasMu.Lock()
	cur := activeFilmSearchMetas.Load()
	if cur == nil || cur.Version != version {
		activeFilmSearchMetasMu.Unlock()
		if _, err := loadFilmSearchMetaIndex(version); err != nil {
			log.Printf("[ActiveReadModel] 检索索引加载失败 version=%s: %v", version, err)
			return
		}
		activeFilmSearchMetasMu.Lock()
		cur = activeFilmSearchMetas.Load()
	}
	rebuild := false
	defer func() {
		activeFilmSearchMetasMu.Unlock()
		if rebuild {
			RequestActiveFilmSearchIndexRebuild()
		}
	}()

	if cur == nil || cur.Version != version {
		return
	}

	// 拿锁后再读库。重建扫描持有同一把锁，过期预读不能在 Store 之后把已删片子写回去。
	const batchSize = 500
	var allRows []dbMetaRow
	for i := 0; i < len(cleanMids); i += batchSize {
		end := i + batchSize
		if end > len(cleanMids) {
			end = len(cleanMids)
		}
		var batchRows []dbMetaRow
		if err := db.Mdb.Model(&model.FilmIndex{}).
			Select(filmMetaSelect).
			Where("mid IN ?", cleanMids[i:end]).
			Find(&batchRows).Error; err != nil {
			log.Printf("[ActiveReadModel] UpsertMidsToActiveFilmSearchIndex 查询 film_index 失败: %v", err)
			return
		}
		allRows = append(allRows, batchRows...)
	}

	plays, err := loadPlaySources(cleanMids)
	if err != nil {
		log.Printf("[ActiveReadModel] 播放源加载失败: %v", err)
		return
	}
	primary := currentPrimarySourceID()
	upsertMap := make(map[int64]FilmSearchMeta, len(allRows))
	for _, r := range allRows {
		meta := metaFromRow(r)
		if strings.TrimSpace(meta.FirstSourceId) != primary {
			continue
		}
		meta.PlaySources = plays[r.Mid]
		upsertMap[r.Mid] = meta
	}

	needsRemoval := false
	for _, id := range cleanMids {
		if _, ok := upsertMap[id]; ok {
			continue
		}
		if searchIndexHasMid(cur, id) {
			needsRemoval = true
			break
		}
	}
	if len(upsertMap) == 0 && !needsRemoval {
		return
	}
	if cur.PrimaryID != currentPrimarySourceID() {
		rebuild = true
		log.Printf("[ActiveReadModel] 增量写入时主站已变，放弃封口并重建 mids=%d primary=%s", len(cleanMids), cur.PrimaryID)
		return
	}
	if !needsRemoval && len(cur.Items) > 0 && len(upsertMap) > 0 {
		idx := updateSearchMetaIndexIncremental(cur, version, upsertMap)
		idx.PrimaryID = cur.PrimaryID
		activeFilmSearchMetas.Store(idx)
		log.Printf("[ActiveReadModel] 内存索引快速增量更新 mids=%d snapshot_found=%d", len(cleanMids), len(upsertMap))
		return
	}
	newItems := make([]FilmSearchMeta, 0, len(cur.Items)+len(upsertMap))
	seenMids := make(map[int64]struct{}, len(upsertMap))
	for _, it := range cur.Items {
		if updated, exists := upsertMap[it.Mid]; exists {
			newItems = append(newItems, updated)
			seenMids[it.Mid] = struct{}{}
		} else if _, inRequest := midSet[it.Mid]; inRequest {
			// 请求里的编号已删除，或不再属于主站。只剔除索引里已有的。
			continue
		} else {
			newItems = append(newItems, *it)
		}
	}
	for mid, it := range upsertMap {
		if _, seen := seenMids[mid]; !seen {
			newItems = append(newItems, it)
		}
	}

	idx := sealSearchMetaIndex(version, newItems)
	idx.PrimaryID = cur.PrimaryID
	activeFilmSearchMetas.Store(idx)
	log.Printf("[ActiveReadModel] 内存索引增量更新/剔除 mids=%d snapshot_found=%d total=%d", len(cleanMids), len(upsertMap), len(newItems))
}

func indexedMIDs(mids []int64) []int64 {
	if len(mids) == 0 {
		return nil
	}
	cur := activeFilmSearchMetas.Load()
	out := make([]int64, 0)
	for _, id := range mids {
		if searchIndexHasMid(cur, id) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func searchIndexHasMid(cur *filmSearchMetaIndex, mid int64) bool {
	if cur == nil || mid <= 0 {
		return false
	}
	if cur.Pos != nil {
		idx, ok := cur.Pos[mid]
		return ok && idx >= 0 && idx < len(cur.Items) && cur.Items[idx] != nil && cur.Items[idx].Mid == mid
	}
	for _, it := range cur.Items {
		if it != nil && it.Mid == mid {
			return true
		}
	}
	return false
}
