package snapshot

import "slices"

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
func patchByUpdate(cur *filmSearchMetaIndex, items []FilmSearchMeta, upsert map[int64]FilmSearchMeta, pos map[int64]int) []int {
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
		return compareSearchMetaUpdate(&items[i], &items[j])
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
		if compareSearchMetaUpdate(&items[changed[i]], &items[rest[j]]) <= 0 {
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
