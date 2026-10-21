package snapshot

import (
	"database/sql"
	"errors"
	"sort"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/support"
)

// ErrSearchIndexMissing 检索索引还没建好，或数据库不可用。
var ErrSearchIndexMissing = errors.New("检索索引未就绪")

const searchHotPoolCap = 50

type hotCand struct {
	hits int64
	mid  int64
}

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
	var rows []row
	if err := db.Mdb.Model(&model.FilmSourcePlaylist{}).
		Select("mid, source_id").
		Where("line_kind = ?", "play").
		Where("mid IN ?", mids).
		Scan(&rows).Error; err != nil {
		return nil, err
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
	for rows.Next() {
		var mid int64
		var sourceID sql.NullString
		if err := rows.Scan(&mid, &sourceID); err != nil {
			return nil, err
		}
		addPlaySource(out, mid, sourceID.String)
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

func sealSearchMetaIndex(version string, items []FilmSearchMeta) *filmSearchMetaIndex {
	internSearchMetas(items)
	byUpdate := make([]int, len(items))
	pos := make(map[int64]int, len(items))
	films := make(map[string]int64)
	playable := make(map[string]int64)
	hot := make(map[string][]hotCand)
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
			playable[src]++
			considerHot(hot, hotPoolMapKey(src, item.CategoryKey), item.Item.Hits, item.Mid)
			if item.RootCategoryKey != "" && item.RootCategoryKey != item.CategoryKey {
				considerHot(hot, hotPoolMapKey(src, item.RootCategoryKey), item.Item.Hits, item.Mid)
			}
		}
	}
	sort.Slice(byUpdate, func(i, j int) bool {
		left := &items[byUpdate[i]]
		right := &items[byUpdate[j]]
		if left.Item.UpdateStamp != right.Item.UpdateStamp {
			return left.Item.UpdateStamp > right.Item.UpdateStamp
		}
		return left.Mid > right.Mid
	})
	pools := make(map[string][]int64, len(hot))
	for key, cands := range hot {
		sort.Slice(cands, func(i, j int) bool {
			return hotter(cands[i], cands[j])
		})
		mids := make([]int64, len(cands))
		for i, c := range cands {
			mids[i] = c.mid
		}
		pools[key] = mids
	}
	return &filmSearchMetaIndex{
		Version:          version,
		Items:            items,
		ByUpdate:         byUpdate,
		Pos:              pos,
		FilmsBySource:    films,
		PlayableBySource: playable,
		HotPools:         pools,
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

func considerHot(pools map[string][]hotCand, key string, hits, mid int64) {
	if key == "" || mid <= 0 {
		return
	}
	cand := hotCand{hits: hits, mid: mid}
	slot := pools[key]
	if len(slot) < searchHotPoolCap {
		pools[key] = append(slot, cand)
		return
	}
	cold := 0
	for i := 1; i < len(slot); i++ {
		if hotter(slot[cold], slot[i]) {
			cold = i
		}
	}
	if hotter(cand, slot[cold]) {
		slot[cold] = cand
	}
}

func hotter(a, b hotCand) bool {
	if a.hits != b.hits {
		return a.hits > b.hits
	}
	return a.mid > b.mid
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

func SearchInventoryCounts() (films map[string]int64, playable map[string]int64, err error) {
	idx, err := loadCurrentSearchIndex()
	if err != nil {
		return nil, nil, err
	}
	return idx.FilmsBySource, idx.PlayableBySource, nil
}

func hotMidsFromIndex(version, sourceID, field string, categoryID int64, limit int) ([]int64, error) {
	if limit <= 0 || categoryID <= 0 {
		return nil, nil
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = strings.TrimSpace(GetActiveSnapshotVersion())
	}
	if version == "" {
		version = LiveReadVersion
	}
	idx, err := loadFilmSearchMetaIndex(version)
	if err != nil {
		return nil, err
	}
	if len(idx.Items) == 0 {
		return nil, nil
	}
	sourceID = strings.TrimSpace(sourceID)
	categoryID = listedCategoryID(sourceID, categoryID)
	if sourceID == "" {
		return topHitsByLocalCategory(idx, field, categoryID, limit), nil
	}
	typeIDs := repository.PublicSourceTypeIDs(sourceID, field, categoryID)
	seen := make(map[int64]struct{})
	cands := make([]hotCand, 0, limit)
	for _, typeID := range typeIDs {
		key := support.BuildSourceCategoryKey(sourceID, typeID)
		for _, mid := range idx.HotPools[hotPoolMapKey(sourceID, key)] {
			if mid <= 0 {
				continue
			}
			if _, ok := seen[mid]; ok {
				continue
			}
			seen[mid] = struct{}{}
			pos, ok := idx.Pos[mid]
			if !ok || pos < 0 || pos >= len(idx.Items) {
				continue
			}
			cands = append(cands, hotCand{hits: idx.Items[pos].Item.Hits, mid: mid})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return hotter(cands[i], cands[j]) })
	if len(cands) > limit {
		cands = cands[:limit]
	}
	mids := make([]int64, len(cands))
	for i, c := range cands {
		mids[i] = c.mid
	}
	return mids, nil
}

func topHitsByLocalCategory(idx *filmSearchMetaIndex, field string, categoryID int64, limit int) []int64 {
	pools := map[string][]hotCand{}
	for i := range idx.Items {
		item := &idx.Items[i]
		match := item.Pid == categoryID
		if field == "cid" {
			match = item.Cid == categoryID
		}
		if !match {
			continue
		}
		considerHot(pools, "k", item.Item.Hits, item.Mid)
	}
	cands := pools["k"]
	sort.Slice(cands, func(i, j int) bool { return hotter(cands[i], cands[j]) })
	if len(cands) > limit {
		cands = cands[:limit]
	}
	mids := make([]int64, len(cands))
	for i, c := range cands {
		mids[i] = c.mid
	}
	return mids
}
