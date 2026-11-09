package snapshot

import (
	"cmp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"server/internal/utils"
)

const sourceHotNameCap = 60

// searchPostings 搜索倒排。汉字按字，拼音按连续两字符，请求只给命中的片子打分。
type searchPostings struct {
	runes map[rune][]int32
	ascii map[string][]int32
}

func appendSingleItemPostings(posts *searchPostings, pos int32, item *FilmSearchMeta) {
	appendSingleItemPostingsCOW(posts, pos, item, nil, nil)
}

func appendSingleItemPostingsCOW(posts *searchPostings, pos int32, item *FilmSearchMeta, ownedR map[rune]struct{}, ownedA map[string]struct{}) {
	if posts == nil || item == nil {
		return
	}
	var seenRune [32]rune
	seenRuneCount := 0
	for _, r := range item.Item.CleanName {
		if r < 128 {
			continue
		}
		dup := false
		for _, s := range seenRune[:seenRuneCount] {
			if s == r {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		if seenRuneCount < len(seenRune) {
			seenRune[seenRuneCount] = r
			seenRuneCount++
		}
		if ownedR == nil {
			list := posts.runes[r]
			if len(list) > 0 && list[len(list)-1] == pos {
				continue
			}
			posts.runes[r] = append(list, pos)
			continue
		}
		posts.runes[r] = cowAppendPos(posts.runes[r], ownedR, r, pos)
	}
	addAsciiPostings(posts.ascii, pos, item.Item.PinyinInitials, ownedA)
	if alts := item.Item.PinyinInitialAlts; alts != "" {
		for _, alt := range strings.Fields(alts) {
			addAsciiPostings(posts.ascii, pos, alt, ownedA)
		}
	}
	addAsciiPostings(posts.ascii, pos, item.Item.PinyinFull, ownedA)
}

func buildSearchPostings(items []FilmSearchMeta) *searchPostings {
	posts := &searchPostings{
		runes: make(map[rune][]int32, 4096),
		ascii: make(map[string][]int32, 4096),
	}
	for i := range items {
		appendSingleItemPostings(posts, int32(i), &items[i])
	}
	return posts
}

func cloneAndPatchSearchPostings(cur *filmSearchMetaIndex, items []*FilmSearchMeta, upsertMap map[int64]FilmSearchMeta, posMap map[int64]int) *searchPostings {
	if cur == nil || cur.SearchPosts == nil || len(cur.SearchPosts.runes) == 0 && len(cur.SearchPosts.ascii) == 0 {
		return buildSearchPostingsPtrs(items)
	}
	type changed struct {
		pos  int32
		meta FilmSearchMeta
	}
	var modified []changed
	for mid, meta := range upsertMap {
		pos, ok := posMap[mid]
		if !ok {
			continue
		}
		if pos < len(cur.Items) {
			old := cur.Items[pos]
			if old.Item.CleanName == meta.Item.CleanName &&
				old.Item.PinyinInitials == meta.Item.PinyinInitials &&
				old.Item.PinyinInitialAlts == meta.Item.PinyinInitialAlts &&
				old.Item.PinyinFull == meta.Item.PinyinFull {
				continue
			}
		}
		modified = append(modified, changed{pos: int32(pos), meta: meta})
	}
	if len(modified) == 0 {
		return cur.SearchPosts
	}
	// 按下标从小到大追加。新片下标都在末尾，倒排表只需尾部追加，不会每条都重排。
	slices.SortFunc(modified, func(a, b changed) int { return cmp.Compare(a.pos, b.pos) })
	posts := &searchPostings{
		runes: make(map[rune][]int32, len(cur.SearchPosts.runes)+len(modified)),
		ascii: make(map[string][]int32, len(cur.SearchPosts.ascii)+len(modified)),
	}
	for r, list := range cur.SearchPosts.runes {
		posts.runes[r] = list
	}
	for s, list := range cur.SearchPosts.ascii {
		posts.ascii[s] = list
	}
	ownedR := make(map[rune]struct{})
	ownedA := make(map[string]struct{})
	for _, ch := range modified {
		appendSingleItemPostingsCOW(posts, ch.pos, &ch.meta, ownedR, ownedA)
	}
	return posts
}

func buildSearchPostingsPtrs(items []*FilmSearchMeta) *searchPostings {
	posts := &searchPostings{
		runes: make(map[rune][]int32, 4096),
		ascii: make(map[string][]int32, 4096),
	}
	for i, item := range items {
		appendSingleItemPostings(posts, int32(i), item)
	}
	return posts
}

func cowAppendPos[K comparable](list []int32, owned map[K]struct{}, key K, pos int32) []int32 {
	if len(list) > 0 && list[len(list)-1] == pos {
		return list
	}
	if _, ok := owned[key]; !ok {
		list = append([]int32(nil), list...)
		owned[key] = struct{}{}
	}
	if len(list) == 0 || pos > list[len(list)-1] {
		return append(list, pos)
	}
	i, found := slices.BinarySearch(list, pos)
	if found {
		return list
	}
	return slices.Insert(list, i, pos)
}
func addAsciiPostings(dst map[string][]int32, pos int32, raw string, owned map[string]struct{}) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return
	}
	write := func(gram string) {
		if owned == nil {
			appendPosting(dst, gram, pos)
			return
		}
		dst[gram] = cowAppendPos(dst[gram], owned, gram, pos)
	}
	if len(raw) == 1 && isSearchASCII(raw[0]) {
		write(raw)
		return
	}
	var seen [64]string
	seenCount := 0
	for i := range len(raw) - 1 {
		c1, c2 := raw[i], raw[i+1]
		if !isSearchASCII(c1) || !isSearchASCII(c2) {
			continue
		}
		gram := raw[i : i+2]
		dup := false
		for _, s := range seen[:seenCount] {
			if s == gram {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		if seenCount < len(seen) {
			seen[seenCount] = gram
			seenCount++
		}
		write(gram)
	}
}

func appendPosting(dst map[string][]int32, gram string, pos int32) {
	list := dst[gram]
	if len(list) > 0 && list[len(list)-1] == pos {
		return
	}
	dst[gram] = append(list, pos)
}

func isSearchASCII(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// TopSourceFilmNames 当前站热度最高的片名，来自封口时的热度榜。
func TopSourceFilmNames(sourceID string, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	idx, err := loadCurrentSearchIndex()
	if err != nil {
		return nil, err
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return namesByHits(idx, limit, func(item *FilmSearchMeta) bool { return item.Pid > 0 }), nil
	}
	mids := idx.SourceHot[sourceID]
	if limit > len(mids) {
		limit = len(mids)
	}
	names := make([]string, 0, limit)
	for _, mid := range mids[:limit] {
		pos, ok := idx.Pos[mid]
		if !ok || pos < 0 || pos >= len(idx.Items) {
			continue
		}
		name := strings.TrimSpace(idx.Items[pos].Item.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

func namesByHits(idx *filmSearchMetaIndex, limit int, keep func(*FilmSearchMeta) bool) []string {
	pools := map[string][]rankCand{}
	for i := range idx.Items {
		item := idx.Items[i]
		if item.Mid <= 0 || !keep(item) {
			continue
		}
		keepRank(pools, "k", rankFromMeta(item), 1, limit)
	}
	cands := pools["k"]
	sort.Slice(cands, func(i, j int) bool {
		return rankBetter(1, cands[i], cands[j])
	})
	names := make([]string, 0, len(cands))
	for _, cand := range cands {
		pos, ok := idx.Pos[cand.mid]
		if !ok || pos < 0 || pos >= len(idx.Items) {
			continue
		}
		name := strings.TrimSpace(idx.Items[pos].Item.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func (p *searchPostings) lookup(q utils.QueryContext) ([]int32, bool) {
	if p == nil {
		return nil, false
	}
	if q.IsAsciiOnly {
		key := strings.ToLower(q.CleanKey)
		if key == "" {
			return nil, true
		}
		if len(key) == 1 {
			return p.ascii[key], true
		}
		lists := make([][]int32, 0, len(key)-1)
		for i := 0; i+1 < len(key); i++ {
			if !isSearchASCII(key[i]) || !isSearchASCII(key[i+1]) {
				continue
			}
			list, ok := p.ascii[key[i:i+2]]
			if !ok {
				return nil, true
			}
			lists = append(lists, list)
		}
		if len(lists) == 0 {
			return nil, true
		}
		return intersectPostings(lists), true
	}
	lists := make([][]int32, 0, q.CleanRunes)
	for _, r := range q.CleanKey {
		if r < 128 || unicode.IsSpace(r) {
			continue
		}
		list, ok := p.runes[r]
		if !ok {
			return nil, true
		}
		lists = append(lists, list)
	}
	if len(lists) == 0 {
		return nil, true
	}
	return intersectPostings(lists), true
}

func intersectPostings(lists [][]int32) []int32 {
	if len(lists) == 0 {
		return nil
	}
	smallest := 0
	for i := 1; i < len(lists); i++ {
		if len(lists[i]) < len(lists[smallest]) {
			smallest = i
		}
	}
	base := lists[smallest]
	if len(lists) == 1 {
		out := make([]int32, len(base))
		copy(out, base)
		return out
	}
	out := make([]int32, 0, len(base))
	for _, id := range base {
		ok := true
		for i, list := range lists {
			if i == smallest {
				continue
			}
			if !postingHas(list, id) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, id)
		}
	}
	return out
}

func postingHas(list []int32, id int32) bool {
	lo, hi := 0, len(list)
	for lo < hi {
		mid := (lo + hi) / 2
		if list[mid] < id {
			lo = mid + 1
			continue
		}
		if list[mid] == id {
			return true
		}
		hi = mid
	}
	return false
}
