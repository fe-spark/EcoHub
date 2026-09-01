package film

import (
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"server/internal/utils"
)

func addPosting(m map[string][]int32, key string, idx int32) {
	if key == "" {
		return
	}
	list := m[key]
	if n := len(list); n > 0 && list[n-1] == idx {
		return
	}
	m[key] = append(list, idx)
}

func addRunePosting(m map[rune][]int32, r rune, idx int32) {
	list := m[r]
	if n := len(list); n > 0 && list[n-1] == idx {
		return
	}
	m[r] = append(list, idx)
}

func addBigrams(m map[string][]int32, s string, idx int32) {
	rs := []rune(s)
	if len(rs) < 2 {
		return
	}
	for i := 0; i < len(rs)-1; i++ {
		addPosting(m, string(rs[i:i+2]), idx)
	}
}

func addUnigrams(m map[rune][]int32, s string, idx int32) {
	for _, r := range s {
		addRunePosting(m, r, idx)
	}
}

func addWordAndPrefixes(m map[string][]int32, w string, idx int32) {
	addPosting(m, w, idx)
	if len(w) < 5 {
		return
	}
	for L := 4; L < len(w); L++ {
		addPosting(m, w[:L], idx)
	}
}

func clonePostings(src []int32) []int32 {
	if len(src) == 0 {
		return nil
	}
	out := make([]int32, len(src))
	copy(out, src)
	return out
}

func intersectAscending(a, b []int32) []int32 {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	out := make([]int32, 0, min(len(a), len(b)))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			out = append(out, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return out
}

func unionAscending(a, b []int32) []int32 {
	if len(a) == 0 {
		return clonePostings(b)
	}
	if len(b) == 0 {
		return clonePostings(a)
	}
	out := make([]int32, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			out = append(out, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}
	if i < len(a) {
		out = append(out, a[i:]...)
	}
	if j < len(b) {
		out = append(out, b[j:]...)
	}
	return out
}

func intersectSortedLists(lists [][]int32) []int32 {
	if len(lists) == 0 {
		return nil
	}
	smallest := 0
	for i := 1; i < len(lists); i++ {
		if len(lists[i]) < len(lists[smallest]) {
			smallest = i
		}
	}
	if len(lists) == 1 {
		return clonePostings(lists[0])
	}
	acc := lists[smallest]
	for i, list := range lists {
		if i == smallest {
			continue
		}
		acc = intersectAscending(acc, list)
		if len(acc) == 0 {
			return nil
		}
	}
	return acc
}

func intersectBigrams(m map[string][]int32, text string) []int32 {
	rs := []rune(text)
	if len(rs) < 2 {
		return nil
	}
	lists := make([][]int32, 0, len(rs)-1)
	for i := 0; i < len(rs)-1; i++ {
		list := m[string(rs[i:i+2])]
		if len(list) == 0 {
			return nil
		}
		lists = append(lists, list)
	}
	return intersectSortedLists(lists)
}

func intersectUnigrams(m map[rune][]int32, text string) []int32 {
	seen := make(map[rune]struct{}, len(text))
	var lists [][]int32
	for _, r := range text {
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		list := m[r]
		if len(list) == 0 {
			return nil
		}
		lists = append(lists, list)
	}
	return intersectSortedLists(lists)
}

func (idx *filmSearchMemoryIndex) getString(offset uint32, length uint16) string {
	if length == 0 || int(offset)+int(length) > len(idx.StringPool) {
		return ""
	}
	return unsafe.String(&idx.StringPool[offset], length)
}

func (idx *filmSearchMemoryIndex) ItemName(item *filmSearchMemoryItem) string {
	if item == nil {
		return ""
	}
	return idx.getString(item.NameOffset, item.NameLen)
}

func (idx *filmSearchMemoryIndex) asSearchItem(itemIndex int) utils.FilmSearchItem {
	if itemIndex < 0 || itemIndex >= len(idx.Items) {
		return utils.FilmSearchItem{}
	}
	item := &idx.Items[itemIndex]
	return utils.FilmSearchItem{
		Mid:               item.Mid,
		Name:              idx.getString(item.NameOffset, item.NameLen),
		CleanName:         idx.getString(item.CleanNameOffset, item.CleanNameLen),
		PinyinFull:        idx.getString(item.PinyinFullOffset, item.PinyinFullLen),
		PinyinInitials:    idx.getString(item.PinyinInitOffset, item.PinyinInitLen),
		PinyinInitialAlts: idx.getString(item.PinyinAltOffset, item.PinyinAltLen),
		Hits:              item.Hits,
		Score:             item.Score,
		Year:              item.Year,
		UpdateStamp:       item.UpdateStamp,
	}
}

func (idx *filmSearchMemoryIndex) buildInverted() {
	n := len(idx.Items)
	// 针对百万级片库优化 map 预估容量：实际中文影视剧名唯一 2-gram 通常在 30000 - 80000 左右
	mapCap := 65536
	if n < mapCap {
		mapCap = n
	}
	idx.nameBigrams = make(map[string][]int32, mapCap)
	idx.nameUnigrams = make(map[rune][]int32, 4096)
	idx.pinyinFullBigrams = make(map[string][]int32, mapCap)
	idx.pinyinInitialBigrams = make(map[string][]int32, mapCap/2)

	for i := range idx.Items {
		id := int32(i)
		item := &idx.Items[i]
		cleanName := idx.getString(item.CleanNameOffset, item.CleanNameLen)
		pinyinFull := idx.getString(item.PinyinFullOffset, item.PinyinFullLen)
		pinyinInit := idx.getString(item.PinyinInitOffset, item.PinyinInitLen)
		pinyinAlt := idx.getString(item.PinyinAltOffset, item.PinyinAltLen)

		addBigrams(idx.nameBigrams, cleanName, id)
		addUnigrams(idx.nameUnigrams, cleanName, id)
		addBigrams(idx.pinyinFullBigrams, pinyinFull, id)
		addBigrams(idx.pinyinInitialBigrams, pinyinInit, id)
		if pinyinAlt != "" {
			for _, v := range strings.Fields(pinyinAlt) {
				addBigrams(idx.pinyinInitialBigrams, v, id)
			}
		}
	}
}

func (idx *filmSearchMemoryIndex) tokenNameCandidates(tok string) []int32 {
	rs := []rune(tok)
	switch {
	case len(rs) >= 2:
		cands := intersectBigrams(idx.nameBigrams, tok)
		if len(cands) == 0 && len(rs) >= 3 && !utils.IsAsciiAlphaNum(tok) {
			return intersectUnigrams(idx.nameUnigrams, tok)
		}
		return cands
	case len(rs) == 1:
		return clonePostings(idx.nameUnigrams[rs[0]])
	default:
		return nil
	}
}

func (idx *filmSearchMemoryIndex) tokenPinyinCandidates(tok string) []int32 {
	if !utils.IsAsciiAlphaNum(tok) || len(tok) < 2 {
		return nil
	}
	return unionAscending(
		intersectBigrams(idx.pinyinFullBigrams, tok),
		intersectBigrams(idx.pinyinInitialBigrams, tok),
	)
}

func (idx *filmSearchMemoryIndex) tokenCandidates(tok string) []int32 {
	return unionAscending(
		idx.tokenNameCandidates(tok),
		idx.tokenPinyinCandidates(tok),
	)
}

func indexQueryTokens(q utils.QueryContext) []string {
	tokens := q.CleanTokens
	if len(tokens) == 0 && q.CleanKey != "" {
		tokens = []string{q.CleanKey}
	}
	if len(tokens) <= 1 {
		return tokens
	}
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if len([]rune(tok)) < 2 {
			continue
		}
		if utils.IsAsciiAlphaNum(tok) && len(tok) < 3 {
			continue
		}
		out = append(out, tok)
	}
	if len(out) == 0 {
		return []string{q.CleanKey}
	}
	return out
}

func (idx *filmSearchMemoryIndex) collectCandidates(q utils.QueryContext) []int32 {
	if q.CleanKey == "" {
		return []int32{}
	}
	if idx.nameBigrams == nil {
		return nil
	}

	tokens := indexQueryTokens(q)
	var acc []int32
	has := false
	for _, tok := range tokens {
		part := idx.tokenCandidates(tok)
		if !has {
			acc = part
			has = true
			continue
		}
		acc = intersectAscending(acc, part)
		if len(acc) == 0 {
			return []int32{}
		}
	}
	if !has {
		acc = idx.tokenCandidates(q.CleanKey)
	}
	if len(acc) == 0 && !q.IsAsciiOnly && q.CleanRunes >= 3 && len(tokens) <= 1 {
		acc = intersectUnigrams(idx.nameUnigrams, q.CleanKey)
	}
	if acc == nil {
		return []int32{}
	}
	return acc
}

type workerBuildResult struct {
	pool  []byte
	items []filmSearchMemoryItem
}

func buildSearchIndexFromRows(version string, rows []filmSearchIndexRow) *filmSearchMemoryIndex {
	n := len(rows)
	if n == 0 {
		return &filmSearchMemoryIndex{Version: version}
	}

	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	chunk := (n + workers - 1) / workers

	results := make([]workerBuildResult, workers)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if start >= n {
			break
		}
		if end > n {
			end = n
		}
		wg.Add(1)
		go func(workerIdx, start, end int) {
			defer wg.Done()
			localPool := make([]byte, 0, (end-start)*64)
			localItems := make([]filmSearchMemoryItem, 0, end-start)

			appendStr := func(s string) (uint32, uint16) {
				if s == "" {
					return 0, 0
				}
				offset := uint32(len(localPool))
				localPool = append(localPool, s...)
				return offset, uint16(len(s))
			}

			for i := start; i < end; i++ {
				r := rows[i]
				if r.Mid <= 0 || r.Name == "" {
					continue
				}
				derived := utils.FilmSearchItem{Name: r.Name}
				utils.FillSearchDerivedFields(&derived)

				nameOff, nameLen := appendStr(r.Name)
				cleanOff, cleanLen := appendStr(derived.CleanName)
				pyFullOff, pyFullLen := appendStr(derived.PinyinFull)
				pyInitOff, pyInitLen := appendStr(derived.PinyinInitials)
				pyAltOff, pyAltLen := appendStr(derived.PinyinInitialAlts)

				localItems = append(localItems, filmSearchMemoryItem{
					Mid:              r.Mid,
					Pid:              r.Pid,
					Cid:              r.Cid,
					Hits:             r.Hits,
					Score:            r.Score,
					Year:             r.Year,
					UpdateStamp:      r.UpdateStamp,
					NameOffset:       nameOff,
					NameLen:          nameLen,
					CleanNameOffset:  cleanOff,
					CleanNameLen:     cleanLen,
					PinyinFullOffset: pyFullOff,
					PinyinFullLen:    pyFullLen,
					PinyinInitOffset: pyInitOff,
					PinyinInitLen:    pyInitLen,
					PinyinAltOffset:  pyAltOff,
					PinyinAltLen:     pyAltLen,
				})
			}
			results[workerIdx] = workerBuildResult{pool: localPool, items: localItems}
		}(w, start, end)
	}
	wg.Wait()

	totalPoolSize := 0
	totalItems := 0
	for _, res := range results {
		totalPoolSize += len(res.pool)
		totalItems += len(res.items)
	}

	finalPool := make([]byte, 0, totalPoolSize)
	finalItems := make([]filmSearchMemoryItem, 0, totalItems)

	for _, res := range results {
		baseOffset := uint32(len(finalPool))
		finalPool = append(finalPool, res.pool...)
		for _, it := range res.items {
			it.NameOffset += baseOffset
			if it.CleanNameLen > 0 {
				it.CleanNameOffset += baseOffset
			}
			if it.PinyinFullLen > 0 {
				it.PinyinFullOffset += baseOffset
			}
			if it.PinyinInitLen > 0 {
				it.PinyinInitOffset += baseOffset
			}
			if it.PinyinAltLen > 0 {
				it.PinyinAltOffset += baseOffset
			}
			finalItems = append(finalItems, it)
		}
	}

	idx := &filmSearchMemoryIndex{
		Version:    version,
		StringPool: finalPool,
		Items:      finalItems,
	}
	idx.buildInverted()
	return idx
}
