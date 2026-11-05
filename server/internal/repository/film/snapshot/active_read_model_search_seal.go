package snapshot

import (
	"database/sql"
	"errors"
	"log"
	"slices"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/utils"
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
		return streamPlaySources()
	}
	type row struct {
		Mid      int64
		SourceId string `gorm:"column:source_id"`
	}
	const batchSize = 1000
	var rows []row
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
		rows = append(rows, chunk...)
	}
	out := make(map[int64][]string)
	for _, r := range rows {
		addPlaySource(out, r.Mid, r.SourceId)
	}
	return out, nil
}

func streamPlaySources() (map[int64][]string, error) {
	rows, err := db.Mdb.Raw(
		"SELECT mid, source_id FROM "+model.TableFilmSourcePlaylist+" WHERE line_kind = ?",
		"play",
	).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64][]string)
	n := 0
	for rows.Next() {
		var mid int64
		var sourceID sql.NullString
		if err := rows.Scan(&mid, &sourceID); err != nil {
			return nil, err
		}
		addPlaySource(out, mid, sourceID.String)
		n++
		if n%200000 == 0 {
			log.Printf("[ActiveReadModel] 检索索引加载中 播放线路 %d", n)
		}
	}
	if n == 0 || n%200000 != 0 {
		log.Printf("[ActiveReadModel] 检索索引加载中 播放线路 %d", n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
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

func itemAggregateKey(item *FilmSearchMeta) string {
	if item == nil {
		return ""
	}
	clean := strings.TrimSpace(item.Item.CleanName)
	if clean != "" {
		return clean
	}
	return utils.NormalizeIdentityTitle(item.Item.Name)
}

func buildSearchMetaRanks(items []FilmSearchMeta) (
	byUpdate []int,
	pos map[int64]int,
	films map[string]int64,
	multiSource map[string]int64,
	hotPools map[string][]int64,
	yearPools map[string][]int64,
	recentPools map[string][]int64,
	sourceHot map[string][]int64,
	byCategory map[string][]int,
) {
	byUpdate = make([]int, len(items))
	pos = make(map[int64]int, len(items))
	films = make(map[string]int64)
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
		if src := strings.TrimSpace(item.FirstSourceId); src != "" {
			films[src]++
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

	multiSource = countMultiSourceBySource(items)
	slices.SortFunc(byUpdate, func(i, j int) int {
		left := &items[i]
		right := &items[j]
		if left.Item.UpdateStamp != right.Item.UpdateStamp {
			if left.Item.UpdateStamp > right.Item.UpdateStamp {
				return -1
			}
			return 1
		}
		if left.Mid > right.Mid {
			return -1
		}
		if left.Mid < right.Mid {
			return 1
		}
		return 0
	})
	byCategory = make(map[string][]int)
	for _, p := range byUpdate {
		key := strings.TrimSpace(items[p].CategoryKey)
		if key == "" {
			continue
		}
		byCategory[key] = append(byCategory[key], p)
	}
	return byUpdate, pos, films, multiSource, finishRanks(hits, 1), finishRanks(years, 0), finishRanks(recents, 2), finishRanks(sourceHotMap, 1), byCategory
}

func sealSearchMetaIndex(version string, items []FilmSearchMeta) *filmSearchMetaIndex {
	internSearchMetas(items)
	byUpdate, pos, films, multiSource, hotPools, yearPools, recentPools, sourceHot, byCategory := buildSearchMetaRanks(items)
	return &filmSearchMetaIndex{
		Version:             version,
		Items:               items,
		ByUpdate:            byUpdate,
		Pos:                 pos,
		FilmsBySource:       films,
		MultiSourceBySource: multiSource,
		HotPools:            hotPools,
		YearPools:           yearPools,
		RecentPools:         recentPools,
		SourceHot:           sourceHot,
		ByCategory:          byCategory,
		SearchPosts:         buildSearchPostings(items),
	}
}
func patchSingleRankPool(pools map[string][]int64, key string, cand rankCand, items []FilmSearchMeta, pos map[int64]int, sortType, cap int) {
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
		other := rankFromMeta(&items[p])
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

	newItems := make([]FilmSearchMeta, len(cur.Items), len(cur.Items)+len(upsertMap))
	copy(newItems, cur.Items)

	newPos := make(map[int64]int, len(cur.Pos)+len(upsertMap))
	for k, v := range cur.Pos {
		newPos[k] = v
	}

	newByCategory := make(map[string][]int, len(cur.ByCategory))
	for k, v := range cur.ByCategory {
		newByCategory[k] = append([]int(nil), v...)
	}

	newFilms := make(map[string]int64, len(cur.FilmsBySource))
	for k, v := range cur.FilmsBySource {
		newFilms[k] = v
	}

	for mid, meta := range upsertMap {
		if idx, exists := newPos[mid]; exists {
			newItems[idx] = meta
		} else {
			newIdx := len(newItems)
			newItems = append(newItems, meta)
			newPos[mid] = newIdx
			if src := strings.TrimSpace(meta.FirstSourceId); src != "" {
				newFilms[src]++
			}
			if cKey := strings.TrimSpace(meta.CategoryKey); cKey != "" {
				newByCategory[cKey] = append(newByCategory[cKey], newIdx)
			}
		}
	}
	if len(newItems) > len(cur.Items) {
		internSearchMetas(newItems[len(cur.Items):])
	}

	newByUpdate := make([]int, len(newItems))
	for i := range newItems {
		newByUpdate[i] = i
	}
	slices.SortFunc(newByUpdate, func(i, j int) int {
		left := &newItems[i]
		right := &newItems[j]
		if left.Item.UpdateStamp != right.Item.UpdateStamp {
			if left.Item.UpdateStamp > right.Item.UpdateStamp {
				return -1
			}
			return 1
		}
		if left.Mid > right.Mid {
			return -1
		}
		if left.Mid < right.Mid {
			return 1
		}
		return 0
	})

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

	return &filmSearchMetaIndex{
		Version:             version,
		Items:               newItems,
		ByUpdate:            newByUpdate,
		Pos:                 newPos,
		FilmsBySource:       newFilms,
		MultiSourceBySource: countMultiSourceBySource(newItems),
		HotPools:            newHotPools,
		YearPools:           newYearPools,
		RecentPools:         newRecentPools,
		SourceHot:           newSourceHot,
		ByCategory:          newByCategory,
		SearchPosts:         posts,
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
		if !visit(&items[i]) {
			return nil
		}
	}
	return nil
}

func SearchInventoryCounts() (films map[string]int64, multiSource map[string]int64, err error) {
	idx, err := loadCurrentSearchIndex()
	if err != nil {
		return nil, nil, err
	}
	return idx.FilmsBySource, idx.MultiSourceBySource, nil
}
