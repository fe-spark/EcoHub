package snapshot

import (
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
	if posts == nil || item == nil {
		return
	}
	seenRune := map[rune]struct{}{}
	for _, r := range item.Item.CleanName {
		if r < 128 {
			continue
		}
		if _, ok := seenRune[r]; ok {
			continue
		}
		seenRune[r] = struct{}{}
		posts.runes[r] = append(posts.runes[r], pos)
	}
	addAsciiPostings(posts.ascii, pos, item.Item.PinyinInitials)
	for _, alt := range strings.Fields(item.Item.PinyinInitialAlts) {
		addAsciiPostings(posts.ascii, pos, alt)
	}
	addAsciiPostings(posts.ascii, pos, item.Item.PinyinFull)
}

func cloneSearchPostings(orig *searchPostings) *searchPostings {
	if orig == nil {
		return &searchPostings{
			runes: map[rune][]int32{},
			ascii: map[string][]int32{},
		}
	}
	p := &searchPostings{
		runes: make(map[rune][]int32, len(orig.runes)),
		ascii: make(map[string][]int32, len(orig.ascii)),
	}
	for k, v := range orig.runes {
		p.runes[k] = append([]int32(nil), v...)
	}
	for k, v := range orig.ascii {
		p.ascii[k] = append([]int32(nil), v...)
	}
	return p
}

func buildSearchPostings(items []FilmSearchMeta) *searchPostings {
	posts := &searchPostings{
		runes: map[rune][]int32{},
		ascii: map[string][]int32{},
	}
	for i := range items {
		appendSingleItemPostings(posts, int32(i), &items[i])
	}
	return posts
}

func addAsciiPostings(dst map[string][]int32, pos int32, raw string) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return
	}
	seen := map[string]struct{}{}
	if len(raw) == 1 && isSearchASCII(raw[0]) {
		appendPosting(dst, raw, pos)
		return
	}
	for i := 0; i+1 < len(raw); i++ {
		if !isSearchASCII(raw[i]) || !isSearchASCII(raw[i+1]) {
			continue
		}
		gram := raw[i : i+2]
		if _, ok := seen[gram]; ok {
			continue
		}
		seen[gram] = struct{}{}
		appendPosting(dst, gram, pos)
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
		item := &idx.Items[i]
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
