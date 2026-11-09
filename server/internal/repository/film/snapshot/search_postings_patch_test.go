package snapshot

import (
	"fmt"
	"math/rand"
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

// 新站整批入库：十万底库追加一万部新片，外加少量改名旧片。
// 倒排按下标顺序补，回退到每条重排时这里会慢一两个数量级。
func BenchmarkUpdateSearchMetaIndexIncremental(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	meta := func(mid int64) FilmSearchMeta {
		name := make([]rune, 3+r.Intn(5))
		for i := range name {
			name[i] = rune(0x4e00 + r.Intn(800)*r.Intn(4)) // 常用字扎堆，倒排表够长
		}
		py := make([]byte, 10)
		for i := range py {
			py[i] = byte('a' + r.Intn(26))
		}
		return FilmSearchMeta{Mid: mid, Item: utils.FilmSearchItem{
			CleanName: string(name), PinyinInitials: string(py[:4]), PinyinFull: string(py),
		}}
	}
	const baseN, addN, renameN = 100000, 10000, 500
	base := make([]FilmSearchMeta, baseN)
	for i := range base {
		base[i] = meta(int64(i + 1))
	}
	cur := sealSearchMetaIndex("v", base)
	upsert := make(map[int64]FilmSearchMeta, addN+renameN)
	for mid := int64(baseN + 1); mid <= baseN+addN; mid++ {
		upsert[mid] = meta(mid)
	}
	for range renameN {
		mid := int64(1 + r.Intn(baseN))
		upsert[mid] = meta(mid)
	}
	b.ResetTimer()
	for b.Loop() {
		updateSearchMetaIndexIncremental(cur, "v", upsert)
	}
}
