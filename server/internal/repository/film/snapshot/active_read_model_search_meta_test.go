package snapshot

import (
	"testing"
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
