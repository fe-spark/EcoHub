package snapshot

import (
	"slices"
	"strings"
)

func compareSearchMetaUpdate(left, right *FilmSearchMeta) int {
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
}

// patchByUpdate 把本次变更插进上一版的时间序。变更项先按时间排好，再与原顺序归并。
func patchByUpdate(cur *filmSearchMetaIndex, items []*FilmSearchMeta, upsert map[int64]FilmSearchMeta, pos map[int64]int) []int {
	changed := make([]int, 0, len(upsert))
	skip := make(map[int]struct{}, len(upsert))
	for mid := range upsert {
		idx, ok := pos[mid]
		if !ok || idx < 0 || idx >= len(items) {
			continue
		}
		changed = append(changed, idx)
		skip[idx] = struct{}{}
	}
	slices.SortFunc(changed, func(i, j int) int {
		return compareSearchMetaUpdate(items[i], items[j])
	})
	rest := make([]int, 0, len(items))
	seen := make(map[int]struct{}, len(items))
	oldLen := 0
	if cur != nil {
		oldLen = len(cur.Items)
		for _, idx := range cur.ByUpdate {
			if _, ok := skip[idx]; ok || idx < 0 || idx >= oldLen {
				continue
			}
			rest = append(rest, idx)
			seen[idx] = struct{}{}
		}
	}
	for i := 0; i < oldLen; i++ {
		if _, ok := seen[i]; ok {
			continue
		}
		if _, ok := skip[i]; ok {
			continue
		}
		rest = append(rest, i)
	}
	out := make([]int, 0, len(changed)+len(rest))
	i, j := 0, 0
	for i < len(changed) && j < len(rest) {
		if compareSearchMetaUpdate(items[changed[i]], items[rest[j]]) <= 0 {
			out = append(out, changed[i])
			i++
		} else {
			out = append(out, rest[j])
			j++
		}
	}
	out = append(out, changed[i:]...)
	out = append(out, rest[j:]...)
	return out
}

func clonePosMap(src map[int64]int, extra int) map[int64]int {
	out := make(map[int64]int, len(src)+extra)
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneFilmCounts(src map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(src)+1)
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneCategoryLists(src map[string][]int) map[string][]int {
	out := make(map[string][]int, len(src)+1)
	for k, v := range src {
		copied := make([]int, len(v))
		copy(copied, v)
		out[k] = copied
	}
	return out
}

type keptSearchMeta struct {
	idx  int
	old  *FilmSearchMeta
	meta FilmSearchMeta
}

func searchMetaCategoryMoved(old *FilmSearchMeta, meta FilmSearchMeta) bool {
	if old == nil {
		return false
	}
	return strings.TrimSpace(old.CategoryKey) != strings.TrimSpace(meta.CategoryKey)
}

func searchMetaSourceMoved(old *FilmSearchMeta, meta FilmSearchMeta) bool {
	if old == nil {
		return false
	}
	return strings.TrimSpace(old.FirstSourceId) != strings.TrimSpace(meta.FirstSourceId)
}

// relocateSearchMeta 在已经复制的表上把已有影片从旧分类、旧来源挪到新键。
func relocateSearchMeta(byCategory map[string][]int, films map[string]int64, moveCategory, moveSource bool, idx int, old *FilmSearchMeta, meta FilmSearchMeta) {
	if old == nil {
		return
	}
	if moveCategory {
		oldCat := strings.TrimSpace(old.CategoryKey)
		newCat := strings.TrimSpace(meta.CategoryKey)
		if oldCat != newCat {
			if oldCat != "" {
				next := removeCategoryPos(byCategory[oldCat], idx)
				if len(next) == 0 {
					delete(byCategory, oldCat)
				} else {
					byCategory[oldCat] = next
				}
			}
			if newCat != "" {
				byCategory[newCat] = append(byCategory[newCat], idx)
			}
		}
	}
	if moveSource {
		oldSrc := strings.TrimSpace(old.FirstSourceId)
		newSrc := strings.TrimSpace(meta.FirstSourceId)
		if oldSrc != newSrc {
			if oldSrc != "" {
				if films[oldSrc] > 1 {
					films[oldSrc]--
				} else {
					delete(films, oldSrc)
				}
			}
			if newSrc != "" {
				films[newSrc]++
			}
		}
	}
}

func removeCategoryPos(list []int, idx int) []int {
	out := make([]int, 0, len(list))
	for _, p := range list {
		if p != idx {
			out = append(out, p)
		}
	}
	return out
}

func dropLeftRankKeys(hot, year, recent, sourceHot map[string][]int64, old *FilmSearchMeta, meta *FilmSearchMeta) {
	if old == nil || meta == nil || !rankMembershipChanged(old, meta) {
		return
	}
	oldKeys := rankPoolKeys(old)
	newKeys := rankPoolKeys(meta)
	for key := range oldKeys {
		if _, ok := newKeys[key]; ok {
			continue
		}
		dropRankMid(hot, key, meta.Mid)
		dropRankMid(year, key, meta.Mid)
		dropRankMid(recent, key, meta.Mid)
	}
	for _, src := range old.PlaySources {
		src = strings.TrimSpace(src)
		if src == "" || metaHasPlaySource(meta, src) {
			continue
		}
		dropRankMid(sourceHot, src, meta.Mid)
	}
}

func rankMembershipChanged(old, meta *FilmSearchMeta) bool {
	if strings.TrimSpace(old.CategoryKey) != strings.TrimSpace(meta.CategoryKey) {
		return true
	}
	if strings.TrimSpace(old.RootCategoryKey) != strings.TrimSpace(meta.RootCategoryKey) {
		return true
	}
	if len(old.PlaySources) != len(meta.PlaySources) {
		return true
	}
	for i := range old.PlaySources {
		if strings.TrimSpace(old.PlaySources[i]) != strings.TrimSpace(meta.PlaySources[i]) {
			return true
		}
	}
	return false
}

func rankPoolKeys(item *FilmSearchMeta) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, src := range item.PlaySources {
		src = strings.TrimSpace(src)
		if src == "" {
			continue
		}
		if key := hotPoolMapKey(src, item.CategoryKey); key != "" {
			keys[key] = struct{}{}
		}
		if item.RootCategoryKey != "" && item.RootCategoryKey != item.CategoryKey {
			if key := hotPoolMapKey(src, item.RootCategoryKey); key != "" {
				keys[key] = struct{}{}
			}
		}
	}
	return keys
}

func metaHasPlaySource(item *FilmSearchMeta, sourceID string) bool {
	for _, src := range item.PlaySources {
		if strings.TrimSpace(src) == sourceID {
			return true
		}
	}
	return false
}

func dropRankMid(pools map[string][]int64, key string, mid int64) {
	if pools == nil || key == "" || mid <= 0 {
		return
	}
	existing := pools[key]
	found := false
	for _, id := range existing {
		if id == mid {
			found = true
			break
		}
	}
	if !found {
		return
	}
	filtered := make([]int64, 0, len(existing)-1)
	for _, id := range existing {
		if id != mid {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		delete(pools, key)
		return
	}
	pools[key] = filtered
}
