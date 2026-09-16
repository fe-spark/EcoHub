package snapshot

import (
	"golang.org/x/sync/singleflight"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/film/shared"
	"server/internal/utils"
)

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

var activeFilmSearchMetas atomic.Pointer[filmSearchMetaIndex]
var activeFilmSearchMetasMu sync.Mutex
var searchMetaBuildSf singleflight.Group
var searchMetaBuildWg sync.WaitGroup

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
