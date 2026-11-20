package snapshot

import (
	"errors"
	"slices"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
)

// ErrSearchIndexMissing 检索索引还没建好，或数据库不可用。
var ErrSearchIndexMissing = errors.New("检索索引未就绪")

const searchHotPoolCap = 50

func loadCurrentSearchIndex() (*filmSearchMetaIndex, error) {
	if db.Mdb == nil {
		return nil, ErrSearchIndexMissing
	}
	version := strings.TrimSpace(GetActiveSnapshotVersion())
	if version == "" {
		if m := GetActiveFilmReadModel(); m != nil {
			version = strings.TrimSpace(m.Version)
		}
	}
	if version == "" {
		version = LiveReadVersion
	}
	return loadFilmSearchMetaIndex(version)
}

// WarmSearchMetaIndex 等到当前这次检索索引构建结束。
func WarmSearchMetaIndex() error {
	_, err := loadCurrentSearchIndex()
	return err
}

func loadPlaySources(mids []int64) (map[int64][]string, error) {
	if db.Mdb == nil {
		return nil, ErrSearchIndexMissing
	}
	if len(mids) == 0 {
		return make(map[int64][]string), nil
	}
	type row struct {
		Mid      int64
		SourceId string `gorm:"column:source_id"`
	}
	const batchSize = 2500
	out := make(map[int64][]string)
	for i := 0; i < len(mids); i += batchSize {
		end := i + batchSize
		if end > len(mids) {
			end = len(mids)
		}
		var chunk []row
		if err := db.Mdb.Model(&model.FilmSourcePlaylist{}).
			Select("mid, source_id").
			Where("line_kind = ?", "play").
			Where("mid IN ?", mids[i:end]).
			Scan(&chunk).Error; err != nil {
			return nil, err
		}
		for _, r := range chunk {
			addPlaySource(out, r.Mid, r.SourceId)
		}
	}
	return out, nil
}

func addPlaySource(out map[int64][]string, mid int64, sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if mid <= 0 || sourceID == "" {
		return
	}
	list := out[mid]
	for _, id := range list {
		if id == sourceID {
			return
		}
	}
	out[mid] = append(list, sourceID)
}

func attachPlaySources(items []FilmSearchMeta, plays map[int64][]string) {
	if len(plays) == 0 {
		return
	}
	for i := range items {
		if srcs, ok := plays[items[i].Mid]; ok {
			items[i].PlaySources = srcs
		}
	}
}

func internSearchMetas(items []FilmSearchMeta) {
	pool := make(map[string]string)
	keep := func(s string) string {
		if s == "" {
			return ""
		}
		if v, ok := pool[s]; ok {
			return v
		}
		pool[s] = s
		return s
	}
	singlePlay := make(map[string][]string)
	for i := range items {
		item := &items[i]
		item.FirstSourceId = keep(item.FirstSourceId)
		item.Area = keep(item.Area)
		item.Language = keep(item.Language)
		item.ClassTag = keep(item.ClassTag)
		item.CategoryKey = keep(item.CategoryKey)
		item.RootCategoryKey = keep(item.RootCategoryKey)
		item.PlaySources = internPlaySources(item.PlaySources, keep, singlePlay)
	}
}

// internPlaySources 单源影片共用一个新切片。多源仍各自分配，不改已发布索引里的切片。
func internPlaySources(srcs []string, keep func(string) string, single map[string][]string) []string {
	if len(srcs) == 0 {
		return nil
	}
	if len(srcs) == 1 {
		src := keep(srcs[0])
		if src == "" {
			return nil
		}
		shared, ok := single[src]
		if !ok {
			shared = []string{src}
			single[src] = shared
		}
		return shared
	}
	copied := make([]string, len(srcs))
	for i, src := range srcs {
		copied[i] = keep(src)
	}
	return copied
}

func buildSearchMetaRanks(items []FilmSearchMeta) (
	byUpdate []int,
	pos map[int64]int,
	hotPools map[string][]int64,
	yearPools map[string][]int64,
	recentPools map[string][]int64,
	sourceHot map[string][]int64,
	byCategory map[string][]int,
) {
	byUpdate = make([]int, len(items))
	pos = make(map[int64]int, len(items))
	hits := make(map[string][]rankCand)
	years := make(map[string][]rankCand)
	recents := make(map[string][]rankCand)
	sourceHotMap := make(map[string][]rankCand)
	for i := range items {
		byUpdate[i] = i
		item := &items[i]
		if item.Mid > 0 {
			pos[item.Mid] = i
		}
		for _, src := range item.PlaySources {
			src = strings.TrimSpace(src)
			if src == "" {
				continue
			}
			keepRank(sourceHotMap, src, rankFromMeta(item), 1, sourceHotNameCap)
			noteFilmRanks(hits, years, recents, hotPoolMapKey(src, item.CategoryKey), item)
			if item.RootCategoryKey != "" && item.RootCategoryKey != item.CategoryKey {
				noteFilmRanks(hits, years, recents, hotPoolMapKey(src, item.RootCategoryKey), item)
			}
		}
	}

	slices.SortFunc(byUpdate, func(i, j int) int {
		return compareSearchMetaUpdate(&items[i], &items[j])
	})
	byCategory = make(map[string][]int)
	for _, p := range byUpdate {
		key := strings.TrimSpace(items[p].CategoryKey)
		if key == "" {
			continue
		}
		byCategory[key] = append(byCategory[key], p)
	}
	return byUpdate, pos, finishRanks(hits, 1), finishRanks(years, 0), finishRanks(recents, 2), finishRanks(sourceHotMap, 1), byCategory
}

func sealSearchMetaIndex(version string, items []FilmSearchMeta) *filmSearchMetaIndex {
	internSearchMetas(items)
	byUpdate, pos, hotPools, yearPools, recentPools, sourceHot, byCategory := buildSearchMetaRanks(items)
	return &filmSearchMetaIndex{
		Version:     version,
		PrimaryID:   currentPrimarySourceID(),
		Items:       pointSearchMetas(items),
		ByUpdate:    byUpdate,
		Pos:         pos,
		HotPools:    hotPools,
		YearPools:   yearPools,
		RecentPools: recentPools,
		SourceHot:   sourceHot,
		ByCategory:  byCategory,
		SearchPosts: buildSearchPostings(items),
	}
}

func pointSearchMetas(items []FilmSearchMeta) []*FilmSearchMeta {
	out := make([]*FilmSearchMeta, len(items))
	for i := range items {
		out[i] = &items[i]
	}
	return out
}

func detachMeta(meta FilmSearchMeta) *FilmSearchMeta {
	copied := meta
	return &copied
}

func patchSingleRankPool(pools map[string][]int64, key string, cand rankCand, items []*FilmSearchMeta, pos map[int64]int, sortType, cap int) {
	if key == "" || cand.mid <= 0 || cap <= 0 {
		return
	}
	existing := pools[key]
	filtered := make([]int64, 0, len(existing)+1)
	for _, id := range existing {
		if id != cand.mid {
			filtered = append(filtered, id)
		}
	}
	insertIdx := len(filtered)
	for i, id := range filtered {
		p, ok := pos[id]
		if !ok || p >= len(items) {
			continue
		}
		other := rankFromMeta(items[p])
		if rankBetter(sortType, cand, other) {
			insertIdx = i
			break
		}
	}
	res := make([]int64, 0, len(filtered)+1)
	res = append(res, filtered[:insertIdx]...)
	res = append(res, cand.mid)
	res = append(res, filtered[insertIdx:]...)
	if len(res) > cap {
		res = res[:cap]
	}
	pools[key] = res
}

func updateSearchMetaIndexIncremental(cur *filmSearchMetaIndex, version string, upsertMap map[int64]FilmSearchMeta) *filmSearchMetaIndex {
	if cur == nil || len(cur.Items) == 0 || cur.Version != version {
		items := make([]FilmSearchMeta, 0, len(upsertMap))
		for _, v := range upsertMap {
			items = append(items, v)
		}
		return sealSearchMetaIndex(version, items)
	}

	newItems := make([]*FilmSearchMeta, len(cur.Items), len(cur.Items)+len(upsertMap))
	copy(newItems, cur.Items)

	// 分类没变时，Pos 和分类列表与上一版共用。变了才复制并搬走下标。
	newPos := cur.Pos
	newByCategory := cur.ByCategory
	updates := make([]keptSearchMeta, 0, len(upsertMap))
	var added []FilmSearchMeta
	var addedMids []int64
	moveCategory := false
	for mid, meta := range upsertMap {
		if idx, exists := newPos[mid]; exists && idx >= 0 && idx < len(newItems) {
			old := newItems[idx]
			if searchMetaCategoryMoved(old, meta) {
				moveCategory = true
			}
			updates = append(updates, keptSearchMeta{idx: idx, old: old, meta: meta})
			continue
		}
		added = append(added, meta)
		addedMids = append(addedMids, mid)
	}
	if len(added) > 0 {
		internSearchMetas(added)
		newPos = clonePosMap(cur.Pos, len(added))
	}
	if len(added) > 0 || moveCategory {
		newByCategory = cloneCategoryLists(cur.ByCategory)
	}
	for _, u := range updates {
		if moveCategory {
			relocateSearchMeta(newByCategory, u.idx, u.old, u.meta)
		}
		newItems[u.idx] = detachMeta(u.meta)
	}
	if len(added) > 0 {
		for i := range added {
			newIdx := len(newItems)
			newItems = append(newItems, &added[i])
			newPos[addedMids[i]] = newIdx
			meta := &added[i]
			if cKey := strings.TrimSpace(meta.CategoryKey); cKey != "" {
				newByCategory[cKey] = append(newByCategory[cKey], newIdx)
			}
		}
	}

	newByUpdate := patchByUpdate(cur, newItems, upsertMap, newPos)

	newRecentPools := make(map[string][]int64, len(cur.RecentPools))
	for k, v := range cur.RecentPools {
		newRecentPools[k] = v
	}
	newHotPools := make(map[string][]int64, len(cur.HotPools))
	for k, v := range cur.HotPools {
		newHotPools[k] = v
	}
	newYearPools := make(map[string][]int64, len(cur.YearPools))
	for k, v := range cur.YearPools {
		newYearPools[k] = v
	}
	newSourceHot := make(map[string][]int64, len(cur.SourceHot))
	for k, v := range cur.SourceHot {
		newSourceHot[k] = v
	}

	for _, u := range updates {
		dropLeftRankKeys(newHotPools, newYearPools, newRecentPools, newSourceHot, u.old, &u.meta)
	}

	for _, meta := range upsertMap {
		item := meta
		cand := rankFromMeta(&item)
		for _, src := range item.PlaySources {
			src = strings.TrimSpace(src)
			if src == "" {
				continue
			}
			patchSingleRankPool(newSourceHot, src, cand, newItems, newPos, 1, sourceHotNameCap)
			key := hotPoolMapKey(src, item.CategoryKey)
			patchSingleRankPool(newHotPools, key, cand, newItems, newPos, 1, categoryRankCap)
			patchSingleRankPool(newYearPools, key, cand, newItems, newPos, 0, categoryRankCap)
			patchSingleRankPool(newRecentPools, key, cand, newItems, newPos, 2, categoryRankCap)
			if item.RootCategoryKey != "" && item.RootCategoryKey != item.CategoryKey {
				rootKey := hotPoolMapKey(src, item.RootCategoryKey)
				patchSingleRankPool(newHotPools, rootKey, cand, newItems, newPos, 1, categoryRankCap)
				patchSingleRankPool(newYearPools, rootKey, cand, newItems, newPos, 0, categoryRankCap)
				patchSingleRankPool(newRecentPools, rootKey, cand, newItems, newPos, 2, categoryRankCap)
			}
		}
	}

	posts := cloneAndPatchSearchPostings(cur, newItems, upsertMap, newPos)

	primaryID := cur.PrimaryID
	if primaryID == "" {
		primaryID = currentPrimarySourceID()
	}
	return &filmSearchMetaIndex{
		Version:     version,
		PrimaryID:   primaryID,
		Items:       newItems,
		ByUpdate:    newByUpdate,
		Pos:         newPos,
		HotPools:    newHotPools,
		YearPools:   newYearPools,
		RecentPools: newRecentPools,
		SourceHot:   newSourceHot,
		ByCategory:  newByCategory,
		SearchPosts: posts,
	}
}

func hotPoolMapKey(sourceID, categoryKey string) string {
	sourceID = strings.TrimSpace(sourceID)
	categoryKey = strings.TrimSpace(categoryKey)
	if sourceID == "" || categoryKey == "" {
		return ""
	}
	return sourceID + "\x00" + categoryKey
}

func (m *FilmSearchMeta) HasPlaySource(sourceID string) bool {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || m == nil {
		return false
	}
	for _, id := range m.PlaySources {
		if id == sourceID {
			return true
		}
	}
	return false
}

// VisitSearchMetasByUpdate 按更新时间从新到旧遍历当前检索索引。visit 返回 false 时停止。
func VisitSearchMetasByUpdate(visit func(*FilmSearchMeta) bool) error {
	idx, err := loadCurrentSearchIndex()
	if err != nil {
		return err
	}
	items := idx.Items
	order := idx.ByUpdate
	if len(order) != len(items) {
		return errors.New("检索索引损坏")
	}
	for _, i := range order {
		if i < 0 || i >= len(items) {
			return errors.New("检索索引损坏")
		}
		if !visit(items[i]) {
			return nil
		}
	}
	return nil
}
