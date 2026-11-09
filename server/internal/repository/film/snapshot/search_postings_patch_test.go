package snapshot

import (
	"fmt"
	"slices"
	"testing"

	"server/internal/utils"
)

// 增量补倒排要和整库重建一致，且每条倒排保持升序。
func TestCloneAndPatchSearchPostingsMatchesFullBuild(t *testing.T) {
	names := []string{"西游记", "红楼梦", "三国演义", "水浒传", "西游降魔"}
	meta := func(mid int64) FilmSearchMeta {
		name := names[int(mid)%len(names)]
		return FilmSearchMeta{Mid: mid, Item: utils.FilmSearchItem{
			CleanName: name, PinyinInitials: fmt.Sprintf("xy%d", mid%3), PinyinFull: "xiyouji",
		}}
	}
	base := make([]FilmSearchMeta, 50)
	for i := range base {
		base[i] = meta(int64(i + 1))
	}
	cur := sealSearchMetaIndex("v", base)
	upsert := map[int64]FilmSearchMeta{}
	for mid := int64(51); mid <= 120; mid++ {
		upsert[mid] = meta(mid)
	}
	// 改一部旧片的名字，走中间插入。
	renamed := meta(3)
	renamed.Item.CleanName = "西游记续"
	upsert[3] = renamed

	next := updateSearchMetaIndexIncremental(cur, "v", upsert)
	all := make([]FilmSearchMeta, len(next.Items))
	for i, it := range next.Items {
		all[i] = *it
	}
	want := buildSearchPostings(all)
	for r, list := range next.SearchPosts.runes {
		if !slices.IsSorted(list) {
			t.Fatalf("rune %q 倒排未排序: %v", r, list)
		}
		// 旧名字的条目不回收，只比较新索引应有的下标都在。
		for _, p := range want.runes[r] {
			if _, ok := slices.BinarySearch(list, p); !ok {
				t.Fatalf("rune %q 缺下标 %d", r, p)
			}
		}
	}
	for g, list := range next.SearchPosts.ascii {
		if !slices.IsSorted(list) {
			t.Fatalf("gram %q 倒排未排序: %v", g, list)
		}
	}
	for g, list := range want.ascii {
		if !slices.Equal(next.SearchPosts.ascii[g], list) {
			t.Fatalf("gram %q 不一致: got %v want %v", g, next.SearchPosts.ascii[g], list)
		}
	}
}
