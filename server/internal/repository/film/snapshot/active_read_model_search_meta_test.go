package snapshot

import (
	"sort"
	"testing"

	"server/internal/model"
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

	again := append([]FilmSearchMeta(nil), idx.Items...)
	idx2 := sealSearchMetaIndex("v", again)
	if idx.Items[0].PlaySources[0] != "src-a" || &idx2.Items[0].PlaySources[0] == &a[0] {
		t.Fatal("再次封口改写了已发布切片")
	}
	if &idx2.Items[0].PlaySources[0] != &idx2.Items[1].PlaySources[0] {
		t.Fatal("再次封口后同一单源应共用切片")
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
