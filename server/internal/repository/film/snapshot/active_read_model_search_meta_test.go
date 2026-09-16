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

	idx1 := loadFilmSearchMetaIndex(testVer)
	if idx1 == nil {
		t.Fatal("空快照应返回非 nil 索引")
	}
	if idx1.Version != testVer || len(idx1.Items) != 0 {
		t.Fatalf("期望 version=%s 且 items=0，实际 version=%s items=%d", testVer, idx1.Version, len(idx1.Items))
	}

	idx2 := loadFilmSearchMetaIndex(testVer)
	if idx2 != idx1 {
		t.Fatalf("二次载入应复用缓存对象（%p != %p）", idx1, idx2)
	}
}
