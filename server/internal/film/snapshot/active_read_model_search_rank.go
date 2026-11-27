package snapshot

import (
	"sort"
	"strings"
)

// PagePrimarySourceFilmMids 当主站内存索引已就绪时，直接按排序与偏移切片返回该站可播影片 mid 与总数，避免大表 COUNT(*) 与深分页 OFFSET。
func PagePrimarySourceFilmMids(sourceID, sortField string, offset, limit int) ([]int64, int, bool) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || limit <= 0 || offset < 0 {
		return nil, 0, false
	}
	idx := activeFilmSearchMetas.Load()
	if idx == nil || len(idx.Items) == 0 || idx.PrimaryID != sourceID || idx.PrimaryID != currentPrimarySourceID() {
		return nil, 0, false
	}
	if len(idx.ByUpdate) != len(idx.Items) {
		return nil, 0, false
	}

	sortKey := strings.TrimSpace(sortField)
	if sortKey != "hits" && sortKey != "score" && sortKey != "year" {
		mids := make([]int64, 0, min(limit, len(idx.Items)))
		total := 0
		for _, pos := range idx.ByUpdate {
			if pos < 0 || pos >= len(idx.Items) {
				continue
			}
			meta := idx.Items[pos]
			if meta == nil || meta.Mid <= 0 || !meta.HasPlaySource(sourceID) {
				continue
			}
			if total >= offset && len(mids) < limit {
				mids = append(mids, meta.Mid)
			}
			total++
		}
		return mids, total, true
	}

	better := func(a, b *FilmSearchMeta) bool {
		switch sortKey {
		case "hits":
			if a.Item.Hits != b.Item.Hits {
				return a.Item.Hits > b.Item.Hits
			}
		case "score":
			if a.Item.Score != b.Item.Score {
				return a.Item.Score > b.Item.Score
			}
			if a.Item.Hits != b.Item.Hits {
				return a.Item.Hits > b.Item.Hits
			}
		case "year":
			if a.Item.Year != b.Item.Year {
				return a.Item.Year > b.Item.Year
			}
			if a.Item.UpdateStamp != b.Item.UpdateStamp {
				return a.Item.UpdateStamp > b.Item.UpdateStamp
			}
		}
		return a.Mid > b.Mid
	}

	const maxPrimaryMemoryOffset = 10000
	if offset >= len(idx.Items) || offset >= maxPrimaryMemoryOffset {
		total := 0
		for _, meta := range idx.Items {
			if meta != nil && meta.Mid > 0 && meta.HasPlaySource(sourceID) {
				total++
			}
		}
		return []int64{}, total, true
	}

	need := len(idx.Items)
	if limit < len(idx.Items)-offset {
		need = offset + limit
	}
	total := 0
	top := make([]*FilmSearchMeta, 0, need)
	for _, meta := range idx.Items {
		if meta == nil || meta.Mid <= 0 || !meta.HasPlaySource(sourceID) {
			continue
		}
		total++
		top = pushBoundedTopMeta(top, meta, need, better)
	}
	if offset >= total || len(top) <= offset {
		return []int64{}, total, true
	}
	sort.Slice(top, func(i, j int) bool {
		return better(top[i], top[j])
	})
	end := min(need, len(top))
	mids := make([]int64, 0, end-offset)
	for _, meta := range top[offset:end] {
		mids = append(mids, meta.Mid)
	}
	return mids, total, true
}

// topKSearchMetas 从候选中选出按 better 排最前的 k 条并保持有序，避免对大集合做全量 sort.Slice。
func topKSearchMetas(cands []*FilmSearchMeta, k int, better func(a, b *FilmSearchMeta) bool) []*FilmSearchMeta {
	if k <= 0 || len(cands) == 0 {
		return nil
	}
	if len(cands) <= k {
		out := append([]*FilmSearchMeta(nil), cands...)
		sort.Slice(out, func(i, j int) bool { return better(out[i], out[j]) })
		return out
	}
	top := make([]*FilmSearchMeta, 0, k)
	for _, meta := range cands {
		top = pushBoundedTopMeta(top, meta, k, better)
	}
	sort.Slice(top, func(i, j int) bool { return better(top[i], top[j]) })
	return top
}

// pushBoundedTopMeta 用小顶堆（堆顶为当前最差元素）将 Top-K 内存与比较收敛在 O(N log K)。
func pushBoundedTopMeta(heap []*FilmSearchMeta, item *FilmSearchMeta, k int, better func(a, b *FilmSearchMeta) bool) []*FilmSearchMeta {
	if k <= 0 || item == nil {
		return heap
	}
	if len(heap) < k {
		heap = append(heap, item)
		i := len(heap) - 1
		for i > 0 {
			parent := (i - 1) / 2
			// 堆顶放更“差”的元素：若 parent 比 i 好，则把 i 往上换，保证 heap[0] 始终是最差元素。
			if !better(heap[parent], heap[i]) {
				break
			}
			heap[parent], heap[i] = heap[i], heap[parent]
			i = parent
		}
		return heap
	}
	if !better(item, heap[0]) {
		return heap
	}
	heap[0] = item
	n := len(heap)
	i := 0
	for {
		left := 2*i + 1
		if left >= n {
			break
		}
		worst := left
		if right := left + 1; right < n && better(heap[left], heap[right]) {
			worst = right
		}
		if !better(heap[i], heap[worst]) {
			break
		}
		heap[i], heap[worst] = heap[worst], heap[i]
		i = worst
	}
	return heap
}
