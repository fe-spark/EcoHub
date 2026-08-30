package film

import (
	"runtime"
	"strings"
	"sync"

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
			out = append(out, a[i])
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

func buildSearchItem(mid, pid, cid int64, name, sub, actor, director string, hits int64, score float64, year, updateStamp int64) filmSearchMemoryItem {
	derived := utils.FilmSearchItem{
		Name:     name,
		SubTitle: sub,
		Actor:    actor,
		Director: director,
	}
	utils.FillSearchDerivedFields(&derived)
	return filmSearchMemoryItem{
		Mid:               mid,
		Pid:               pid,
		Cid:               cid,
		Name:              name,
		CleanName:         derived.CleanName,
		PinyinFull:        derived.PinyinFull,
		PinyinSyllables:   derived.PinyinSyllables,
		PinyinInitials:    derived.PinyinInitials,
		PinyinInitialAlts: derived.PinyinInitialAlts,
		AliasSegs:         derived.AliasSegs,
		AliasWords:        derived.AliasWords,
		PersonSegs:        derived.PersonSegs,
		PersonWords:       derived.PersonWords,
		Hits:              hits,
		Score:             score,
		Year:              year,
		UpdateStamp:       updateStamp,
	}
}

func (item filmSearchMemoryItem) asSearchItem() utils.FilmSearchItem {
	return utils.FilmSearchItem{
		Mid:               item.Mid,
		Name:              item.Name,
		CleanName:         item.CleanName,
		PinyinFull:        item.PinyinFull,
		PinyinSyllables:   item.PinyinSyllables,
		PinyinInitials:    item.PinyinInitials,
		PinyinInitialAlts: item.PinyinInitialAlts,
		AliasSegs:         item.AliasSegs,
		AliasWords:        item.AliasWords,
		PersonSegs:        item.PersonSegs,
		PersonWords:       item.PersonWords,
		Hits:              item.Hits,
		Score:             item.Score,
		Year:              item.Year,
		UpdateStamp:       item.UpdateStamp,
	}
}

func (idx *filmSearchMemoryIndex) buildInverted() {
	n := len(idx.Items)
	idx.nameBigrams = make(map[string][]int32, n)
	idx.nameUnigrams = make(map[rune][]int32, 4096)
	idx.personBigrams = make(map[string][]int32, n)
	idx.personExact = make(map[string][]int32, n)
	idx.aliasBigrams = make(map[string][]int32, n)
	idx.aliasExact = make(map[string][]int32, n)
	idx.personWords = make(map[string][]int32, n)
	idx.aliasWords = make(map[string][]int32, n)
	idx.pinyinFullBigrams = make(map[string][]int32, n)
	idx.pinyinInitialBigrams = make(map[string][]int32, n)

	for i := range idx.Items {
		id := int32(i)
		item := &idx.Items[i]
		addBigrams(idx.nameBigrams, item.CleanName, id)
		addUnigrams(idx.nameUnigrams, item.CleanName, id)
		addBigrams(idx.pinyinFullBigrams, item.PinyinFull, id)
		addBigrams(idx.pinyinInitialBigrams, item.PinyinInitials, id)
		if item.PinyinInitialAlts != "" {
			for _, v := range strings.Fields(item.PinyinInitialAlts) {
				addBigrams(idx.pinyinInitialBigrams, v, id)
			}
		}
		for _, seg := range item.PersonSegs {
			addPosting(idx.personExact, seg, id)
			addBigrams(idx.personBigrams, seg, id)
		}
		for _, w := range item.PersonWords {
			addWordAndPrefixes(idx.personWords, w, id)
		}
		for _, seg := range item.AliasSegs {
			addPosting(idx.aliasExact, seg, id)
			addBigrams(idx.aliasBigrams, seg, id)
		}
		for _, w := range item.AliasWords {
			addWordAndPrefixes(idx.aliasWords, w, id)
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

func (idx *filmSearchMemoryIndex) tokenSideCandidates(tok string) []int32 {
	rs := []rune(tok)
	var extra []int32
	if utils.IsAsciiAlphaNum(tok) {
		if len(tok) >= 2 {
			extra = unionAscending(idx.personWords[tok], idx.aliasWords[tok])
		}
		if len(tok) >= 2 {
			extra = unionAscending(extra, idx.aliasExact[tok])
			extra = unionAscending(extra, intersectBigrams(idx.aliasBigrams, tok))
		}
		return extra
	}
	if len(rs) >= 2 && len(rs) < 5 {
		extra = unionAscending(idx.personExact[tok], intersectBigrams(idx.personBigrams, tok))
	}
	extra = unionAscending(extra, idx.aliasExact[tok])
	extra = unionAscending(extra, intersectBigrams(idx.aliasBigrams, tok))
	return extra
}

func (idx *filmSearchMemoryIndex) tokenCandidates(tok string) []int32 {
	return unionAscending(
		unionAscending(idx.tokenNameCandidates(tok), idx.tokenSideCandidates(tok)),
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

func parallelBuildItems(rows []filmSearchIndexRow) []filmSearchMemoryItem {
	n := len(rows)
	tmp := make([]filmSearchMemoryItem, n)
	valid := make([]bool, n)
	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	if n == 0 {
		return nil
	}
	chunk := (n + workers - 1) / workers
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
		go func(start, end int) {
			defer wg.Done()
			for i := start; i < end; i++ {
				r := rows[i]
				if r.Mid <= 0 || r.Name == "" {
					continue
				}
				tmp[i] = buildSearchItem(r.Mid, r.Pid, r.Cid, r.Name, r.SubTitle, r.Actor, r.Director, r.Hits, r.Score, r.Year, r.UpdateStamp)
				valid[i] = true
			}
		}(start, end)
	}
	wg.Wait()

	items := make([]filmSearchMemoryItem, 0, n)
	for i := range tmp {
		if valid[i] {
			items = append(items, tmp[i])
		}
	}
	return items
}
