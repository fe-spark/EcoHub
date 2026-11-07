package snapshot

import (
	"fmt"
	"sort"
	"testing"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/support"
	"server/internal/utils"
)

// 空快照索引必须被缓存，二次载入直接复用同一对象，不得因 len(Items)==0 反复穿透查库。
// 该断言依赖内存索引指针同一性，无法通过公开 API 黑盒观测，故保留为白盒单测。
func TestLoadFilmSearchMetaIndex_EmptySnapshotCached(t *testing.T) {
	_ = setupSnapshotRepoTestDB(t)
	testVer := "v_empty_snapshot_cache"

	oldMeta := activeFilmSearchMetas.Load()
	t.Cleanup(func() {
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
	})

	activeFilmSearchMetas.Store(nil)

	idx1, err := loadFilmSearchMetaIndex(testVer)
	if err != nil {
		t.Fatal(err)
	}
	if idx1 == nil {
		t.Fatal("空快照应返回非 nil 索引")
	}
	if idx1.Version != testVer || len(idx1.Items) != 0 {
		t.Fatalf("期望 version=%s 且 items=0，实际 version=%s items=%d", testVer, idx1.Version, len(idx1.Items))
	}

	idx2, err := loadFilmSearchMetaIndex(testVer)
	if err != nil {
		t.Fatal(err)
	}
	if idx2 != idx1 {
		t.Fatalf("二次载入应复用缓存对象（%p != %p）", idx1, idx2)
	}
}

func TestSealSearchMetaIndexCopiesPlaySources(t *testing.T) {
	shared := []string{"src-a", "src-b"}
	idx := sealSearchMetaIndex("v", []FilmSearchMeta{{
		Mid:           1,
		PlaySources:   shared,
		FirstSourceId: "src-a",
	}})
	if len(idx.Items) != 1 || len(idx.Items[0].PlaySources) != 2 {
		t.Fatalf("封口播放源 = %#v", idx.Items)
	}
	if &idx.Items[0].PlaySources[0] == &shared[0] {
		t.Fatal("封口后的播放源仍和原切片共享底层数组")
	}
	if shared[0] != "src-a" || shared[1] != "src-b" {
		t.Fatalf("原播放源被改写: %#v", shared)
	}
}

func TestSealSearchMetaIndexSharesSinglePlaySource(t *testing.T) {
	idx := sealSearchMetaIndex("v", []FilmSearchMeta{
		{Mid: 1, PlaySources: []string{"src-a"}},
		{Mid: 2, PlaySources: []string{"src-a"}},
		{Mid: 3, PlaySources: []string{"src-b"}},
		{Mid: 4, PlaySources: []string{"src-a", "src-b"}},
		{Mid: 5, PlaySources: []string{"src-a", "src-b"}},
	})
	a := idx.Items[0].PlaySources
	b := idx.Items[1].PlaySources
	if len(a) != 1 || len(b) != 1 || &a[0] != &b[0] {
		t.Fatalf("同一单源应共用切片: %#v %#v", a, b)
	}
	if &idx.Items[2].PlaySources[0] == &a[0] {
		t.Fatal("不同单源不应共用切片")
	}
	left := idx.Items[3].PlaySources
	right := idx.Items[4].PlaySources
	if len(left) != 2 || &left[0] == &a[0] || &left[0] == &right[0] {
		t.Fatalf("多源应各自分配: %#v %#v", left, right)
	}

	again := make([]FilmSearchMeta, len(idx.Items))
	for i, it := range idx.Items {
		again[i] = *it
	}
	idx2 := sealSearchMetaIndex("v", again)
	if idx.Items[0].PlaySources[0] != "src-a" || &idx2.Items[0].PlaySources[0] == &a[0] {
		t.Fatal("再次封口改写了已发布切片")
	}
	if &idx2.Items[0].PlaySources[0] != &idx2.Items[1].PlaySources[0] {
		t.Fatal("再次封口后同一单源应共用切片")
	}
}

func TestSealSearchMetaIndex_MultiSourceCounts(t *testing.T) {
	idx := sealSearchMetaIndex("v", []FilmSearchMeta{
		{Mid: 1, FirstSourceId: "src-a", Item: utils.FilmSearchItem{CleanName: "嫁金钗"}},
		{Mid: 2, FirstSourceId: "src-b", Item: utils.FilmSearchItem{CleanName: "嫁金钗"}},
		{Mid: 3, FirstSourceId: "src-a", Item: utils.FilmSearchItem{CleanName: "独家影片"}},
		{Mid: 4, FirstSourceId: "src-a", PlaySources: []string{"src-a", "src-c"}, Item: utils.FilmSearchItem{CleanName: "多线路影片"}},
	})
	if idx.MultiSourceBySource["src-a"] != 2 {
		t.Fatalf("src-a multiSource=%d, want 2", idx.MultiSourceBySource["src-a"])
	}
	if idx.MultiSourceBySource["src-b"] != 1 {
		t.Fatalf("src-b multiSource=%d, want 1", idx.MultiSourceBySource["src-b"])
	}
}

func TestIncrementalSeal_RefreshesMultiSourceCounts(t *testing.T) {
	cur := sealSearchMetaIndex("v", []FilmSearchMeta{
		{Mid: 1, FirstSourceId: "src-a", Item: utils.FilmSearchItem{CleanName: "嫁金钗"}},
		{Mid: 3, FirstSourceId: "src-a", Item: utils.FilmSearchItem{CleanName: "独家影片"}},
	})
	if cur.MultiSourceBySource["src-a"] != 0 {
		t.Fatalf("封口前 src-a 多源 = %d", cur.MultiSourceBySource["src-a"])
	}

	next := updateSearchMetaIndexIncremental(cur, "v", map[int64]FilmSearchMeta{
		2: {Mid: 2, FirstSourceId: "src-b", Item: utils.FilmSearchItem{CleanName: "嫁金钗"}},
		3: {Mid: 3, FirstSourceId: "src-a", PlaySources: []string{"src-a", "src-c"}, Item: utils.FilmSearchItem{CleanName: "独家影片"}},
	})
	if next.MultiSourceBySource["src-a"] != 2 {
		t.Fatalf("src-a 多源 = %d，want 2", next.MultiSourceBySource["src-a"])
	}
	if next.MultiSourceBySource["src-b"] != 1 {
		t.Fatalf("src-b 多源 = %d，want 1", next.MultiSourceBySource["src-b"])
	}
}

func TestLoadFilmSearchMetaIndex_DedupesPlayLines(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	createTestFilm(t, gdb, 7, "慶餘年", 10, "")
	if err := gdb.Create(&model.FilmSourcePlaylist{
		Mid: 7, SourceId: "source_master", LineKind: "play", GroupIndex: 1, Content: "[]",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSourcePlaylist{
		Mid: 7, SourceId: "source_b", LineKind: "play", GroupIndex: 0, Content: "[]",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSourcePlaylist{
		Mid: 7, SourceId: "source_down", LineKind: "download", GroupIndex: 0, Content: "[]",
	}).Error; err != nil {
		t.Fatal(err)
	}

	activeFilmSearchMetas.Store(nil)
	idx, err := loadFilmSearchMetaIndex("v_dedupe_play")
	if err != nil {
		t.Fatal(err)
	}
	if idx == nil || len(idx.Items) != 1 {
		t.Fatalf("items=%v", idx)
	}
	item := idx.Items[0]
	if item.Item.CleanName != "庆余年" {
		t.Fatalf("CleanName=%q", item.Item.CleanName)
	}
	got := append([]string(nil), item.PlaySources...)
	sort.Strings(got)
	want := []string{"source_b", "source_master"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("PlaySources=%v", got)
	}
}

func TestSearchFilmMetas_DisabledCategoryHidden(t *testing.T) {
	support.SetCategoryTreeForTest(map[int64]int64{1: 0, 2: 0}, map[int64]string{1: "电影", 2: "电视剧"})
	support.SetCategoryShowForTest(map[int64]bool{1: true, 2: false}) // 分类 2 停用
	t.Cleanup(func() {
		support.SetCategoryShowForTest(nil)
		support.SetCategoryTreeForTest(nil, nil)
	})

	item1 := utils.FilmSearchItem{Mid: 101, Name: "复仇者联盟"}
	utils.FillSearchDerivedFields(&item1)
	item2 := utils.FilmSearchItem{Mid: 102, Name: "复仇者电视剧"}
	utils.FillSearchDerivedFields(&item2)

	items := []FilmSearchMeta{
		{Mid: 101, Pid: 1, Cid: 1, Item: item1}, // 电影（可见）
		{Mid: 102, Pid: 2, Cid: 2, Item: item2}, // 电视剧（已停用）
	}
	idx := sealSearchMetaIndex("v_test_hidden", items)

	hits := searchFilmMetas(idx, "复仇者", "relate", "", 0, 0)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d hits", len(hits))
	}
	if hits[0].mid != 101 {
		t.Fatalf("expected mid 101, got %d", hits[0].mid)
	}
}

func TestSearchFilmMetas_HiddenSourceCategory(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	if err := gdb.AutoMigrate(&model.MappingRule{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := support.CurrentMappingSnapshotForTest()
	t.Cleanup(func() {
		support.StoreMappingSnapshotForTest(prev)
	})
	if err := gdb.Create([]model.Category{
		{Id: 1, Pid: 0, Name: "电影", Show: true, StableKey: "src_search_hide:1"},
		{Id: 17, Pid: 0, Name: "公告", Show: true, StableKey: "src_search_hide:17"},
	}).Error; err != nil {
		t.Fatalf("create categories: %v", err)
	}
	if err := gdb.Model(&model.Category{}).Where("id = ?", 17).Update("show", false).Error; err != nil {
		t.Fatalf("hide 公告: %v", err)
	}
	if err := gdb.Create([]model.CategoryMapping{
		{SourceId: "src_search_hide", SourceTypeId: 1, CategoryId: 1},
		{SourceId: "src_search_hide", SourceTypeId: 17, CategoryId: 17},
	}).Error; err != nil {
		t.Fatalf("create mappings: %v", err)
	}
	if err := gdb.Create([]model.SourceCategory{
		{SourceId: "src_search_hide", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0},
		{SourceId: "src_search_hide", SourceTypeId: 17, ParentSourceTypeId: 0, RawName: "公告", Sort: 2, Depth: 0},
	}).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}
	repository.ReloadMappingRules()

	item1 := utils.FilmSearchItem{Mid: 101, Name: "复仇者联盟"}
	utils.FillSearchDerivedFields(&item1)
	item2 := utils.FilmSearchItem{Mid: 102, Name: "复仇者公告"}
	utils.FillSearchDerivedFields(&item2)
	idx := sealSearchMetaIndex("v_src_hidden", []FilmSearchMeta{
		{Mid: 101, CategoryKey: "source:src_search_hide:1", Item: item1},
		{Mid: 102, CategoryKey: "source:src_search_hide:17", Item: item2},
	})
	hits := searchFilmMetas(idx, "复仇者", "relate", "src_search_hide", 0, 0)
	if len(hits) != 1 || hits[0].mid != 101 {
		t.Fatalf("hits=%v, want only 101", hits)
	}
}

func TestTagMetaMatch_DisabledCategoryHidden(t *testing.T) {
	support.SetCategoryTreeForTest(map[int64]int64{1: 0, 2: 0}, map[int64]string{1: "电影", 2: "电视剧"})
	support.SetCategoryShowForTest(map[int64]bool{1: true, 2: false}) // 分类 2 停用
	t.Cleanup(func() {
		support.SetCategoryShowForTest(nil)
		support.SetCategoryTreeForTest(nil, nil)
	})

	matcher, err := tagMetaMatch("v_test", model.SearchTagsVO{})
	if err != nil {
		t.Fatal(err)
	}

	visibleMeta := &FilmSearchMeta{Mid: 101, Pid: 1, Cid: 1}
	disabledMeta := &FilmSearchMeta{Mid: 102, Pid: 2, Cid: 2}

	if !matcher(visibleMeta) {
		t.Fatal("expected visibleMeta to match")
	}
	if matcher(disabledMeta) {
		t.Fatal("expected disabledMeta to be filtered out")
	}
}

func TestIncrementalSeal_ReusesUnchangedObjects(t *testing.T) {
	cur := sealSearchMetaIndex("v", []FilmSearchMeta{
		{Mid: 1, FirstSourceId: "src-a", CategoryKey: "cat-a", Item: utils.FilmSearchItem{Mid: 1, Name: "甲", UpdateStamp: 10}},
		{Mid: 2, FirstSourceId: "src-a", CategoryKey: "cat-a", Item: utils.FilmSearchItem{Mid: 2, Name: "乙", UpdateStamp: 20}},
	})
	kept := cur.Items[1]
	next := updateSearchMetaIndexIncremental(cur, "v", map[int64]FilmSearchMeta{
		1: {Mid: 1, FirstSourceId: "src-a", CategoryKey: "cat-a", Item: utils.FilmSearchItem{Mid: 1, Name: "甲改", UpdateStamp: 30}},
	})
	if next.Pos != nil && fmtPos(next.Pos) != fmtPos(cur.Pos) {
		t.Fatal("只更新已有影片时不应复制 Pos")
	}
	if fmtFilmCounts(next.FilmsBySource) != fmtFilmCounts(cur.FilmsBySource) {
		t.Fatal("只更新已有影片时不应复制来源计数")
	}
	if fmtCategory(next.ByCategory) != fmtCategory(cur.ByCategory) {
		t.Fatal("只更新已有影片时不应复制分类列表")
	}
	if next.Items[1] != kept {
		t.Fatal("未变更条目应共用对象")
	}
	if next.Items[0] == cur.Items[0] {
		t.Fatal("变更条目应换成新对象")
	}
	if cur.Items[0].Item.Name != "甲" || next.Items[0].Item.Name != "甲改" {
		t.Fatalf("旧索引被改写: old=%q new=%q", cur.Items[0].Item.Name, next.Items[0].Item.Name)
	}
	if _, ok := cur.Pos[3]; ok {
		t.Fatal("旧 Pos 被写入")
	}

	grown := updateSearchMetaIndexIncremental(next, "v", map[int64]FilmSearchMeta{
		3: {Mid: 3, FirstSourceId: "src-b", CategoryKey: "cat-b", Item: utils.FilmSearchItem{Mid: 3, Name: "丙", UpdateStamp: 40}},
	})
	if fmtPos(grown.Pos) == fmtPos(next.Pos) {
		t.Fatal("新增影片应复制 Pos")
	}
	if _, ok := next.Pos[3]; ok {
		t.Fatal("新增影片写进了上一版 Pos")
	}
	if grown.Items[1] != kept {
		t.Fatal("新增时未变更条目应继续共用对象")
	}
	if grown.FilmsBySource["src-b"] != 1 || next.FilmsBySource["src-b"] != 0 {
		t.Fatalf("来源计数 old=%d new=%d", next.FilmsBySource["src-b"], grown.FilmsBySource["src-b"])
	}
}

func TestIncrementalSeal_MovesCategoryAndSource(t *testing.T) {
	cur := sealSearchMetaIndex("v", []FilmSearchMeta{
		{
			Mid: 1, FirstSourceId: "src-a", PlaySources: []string{"src-a"},
			CategoryKey: "cat-a", RootCategoryKey: "root-a",
			Item: utils.FilmSearchItem{Mid: 1, Name: "甲", UpdateStamp: 10, Hits: 5},
		},
		{
			Mid: 2, FirstSourceId: "src-a", PlaySources: []string{"src-a"},
			CategoryKey: "cat-a",
			Item:        utils.FilmSearchItem{Mid: 2, Name: "乙", UpdateStamp: 20, Hits: 1},
		},
	})
	oldCat := append([]int(nil), cur.ByCategory["cat-a"]...)
	oldHot := append([]int64(nil), cur.HotPools[hotPoolMapKey("src-a", "cat-a")]...)
	moved := updateSearchMetaIndexIncremental(cur, "v", map[int64]FilmSearchMeta{
		1: {
			Mid: 1, FirstSourceId: "src-a", PlaySources: []string{"src-a"},
			CategoryKey: "cat-b", RootCategoryKey: "root-b",
			Item: utils.FilmSearchItem{Mid: 1, Name: "甲", UpdateStamp: 30, Hits: 9},
		},
	})
	if fmtPos(moved.Pos) != fmtPos(cur.Pos) {
		t.Fatal("只改分类不应复制 Pos")
	}
	if fmtFilmCounts(moved.FilmsBySource) != fmtFilmCounts(cur.FilmsBySource) {
		t.Fatal("分类变化不应复制来源计数")
	}
	if fmtCategory(moved.ByCategory) == fmtCategory(cur.ByCategory) {
		t.Fatal("分类变化应复制分类列表")
	}
	if !sameInts(cur.ByCategory["cat-a"], oldCat) {
		t.Fatalf("旧分类列表被改写 %v", cur.ByCategory["cat-a"])
	}
	if intsContain(moved.ByCategory["cat-a"], 0) || !intsContain(moved.ByCategory["cat-a"], 1) {
		t.Fatalf("旧分类应只留下未变更影片 %v", moved.ByCategory["cat-a"])
	}
	if !intsContain(moved.ByCategory["cat-b"], 0) {
		t.Fatalf("新分类缺少下标 %v", moved.ByCategory["cat-b"])
	}
	oldKey := hotPoolMapKey("src-a", "cat-a")
	newKey := hotPoolMapKey("src-a", "cat-b")
	oldRoot := hotPoolMapKey("src-a", "root-a")
	newRoot := hotPoolMapKey("src-a", "root-b")
	if midsContain(moved.HotPools[oldKey], 1) || !midsContain(moved.HotPools[newKey], 1) {
		t.Fatalf("热榜未搬家 old=%v new=%v", moved.HotPools[oldKey], moved.HotPools[newKey])
	}
	if midsContain(moved.HotPools[oldRoot], 1) || !midsContain(moved.HotPools[newRoot], 1) {
		t.Fatalf("一级分类热榜未搬家 old=%v new=%v", moved.HotPools[oldRoot], moved.HotPools[newRoot])
	}
	if !midsContain(cur.HotPools[oldKey], 1) || !sameMids(cur.HotPools[oldKey], oldHot) {
		t.Fatalf("旧热榜被改写 %v", cur.HotPools[oldKey])
	}

	sourced := updateSearchMetaIndexIncremental(moved, "v", map[int64]FilmSearchMeta{
		1: {
			Mid: 1, FirstSourceId: "src-b", PlaySources: []string{"src-b"},
			CategoryKey: "cat-b", RootCategoryKey: "root-b",
			Item: utils.FilmSearchItem{Mid: 1, Name: "甲", UpdateStamp: 40, Hits: 9},
		},
	})
	if fmtCategory(sourced.ByCategory) != fmtCategory(moved.ByCategory) {
		t.Fatal("只改来源不应复制分类列表")
	}
	if fmtFilmCounts(sourced.FilmsBySource) == fmtFilmCounts(moved.FilmsBySource) {
		t.Fatal("来源变化应复制来源计数")
	}
	if moved.FilmsBySource["src-a"] != 2 {
		t.Fatalf("旧来源计数被改写 %d", moved.FilmsBySource["src-a"])
	}
	if sourced.FilmsBySource["src-a"] != 1 || sourced.FilmsBySource["src-b"] != 1 {
		t.Fatalf("来源计数 a=%d b=%d", sourced.FilmsBySource["src-a"], sourced.FilmsBySource["src-b"])
	}
	if midsContain(sourced.SourceHot["src-a"], 1) || !midsContain(sourced.SourceHot["src-b"], 1) {
		t.Fatalf("来源热榜未搬家 a=%v b=%v", sourced.SourceHot["src-a"], sourced.SourceHot["src-b"])
	}
	if !midsContain(moved.SourceHot["src-a"], 1) {
		t.Fatal("旧来源热榜被改写")
	}
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func intsContain(list []int, v int) bool {
	for _, n := range list {
		if n == v {
			return true
		}
	}
	return false
}

func sameMids(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func midsContain(list []int64, v int64) bool {
	for _, n := range list {
		if n == v {
			return true
		}
	}
	return false
}

func fmtPos(m map[int64]int) string {
	return fmt.Sprintf("%p", m)
}

func fmtFilmCounts(m map[string]int64) string {
	return fmt.Sprintf("%p", m)
}

func fmtCategory(m map[string][]int) string {
	return fmt.Sprintf("%p", m)
}

func TestUpdateSearchMetaIndexIncremental_UpdatesRankPools(t *testing.T) {
	initial := sealSearchMetaIndex("v1", []FilmSearchMeta{
		{
			Mid:           1,
			FirstSourceId: "src-1",
			PlaySources:   []string{"src-1"},
			CategoryKey:   "cat-1",
			Item: utils.FilmSearchItem{
				Mid:         1,
				Name:        "旧电影",
				CleanName:   "旧电影",
				UpdateStamp: 100,
			},
		},
	})

	key := hotPoolMapKey("src-1", "cat-1")
	if len(initial.RecentPools[key]) != 1 || initial.RecentPools[key][0] != 1 {
		t.Fatalf("initial RecentPools=%v, want [1]", initial.RecentPools[key])
	}

	updated := updateSearchMetaIndexIncremental(initial, "v1", map[int64]FilmSearchMeta{
		2: {
			Mid:           2,
			FirstSourceId: "src-1",
			PlaySources:   []string{"src-1"},
			CategoryKey:   "cat-1",
			Item: utils.FilmSearchItem{
				Mid:         2,
				Name:        "新电影",
				CleanName:   "新电影",
				UpdateStamp: 200,
			},
		},
	})

	recents := updated.RecentPools[key]
	if len(recents) != 2 {
		t.Fatalf("updated RecentPools length=%d, want 2, got %v", len(recents), recents)
	}
	if recents[0] != 2 || recents[1] != 1 {
		t.Fatalf("updated RecentPools order=%v, want [2, 1]", recents)
	}
}
