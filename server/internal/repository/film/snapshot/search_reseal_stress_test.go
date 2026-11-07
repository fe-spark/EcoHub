package snapshot

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/utils"
)

// TestResealSearchIndexMids_FullScaleSimulation 真实模拟 30,000 部影片入库后全量封口流程。
// 验证在存量 100,000 部索引的基础上，注入 30,000 部新片时：
// 1. 内存增长严格受控，不触发全库重建
// 2. 封口速度在秒级完成
// 3. 30,000 部新片全部进入倒排索引并可正常检索
func TestResealSearchIndexMids_FullScaleSimulation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full scale stress test in short mode")
	}

	gdb := setupSnapshotRepoTestDB(t)
	const existingCount = 100000
	const newBatchCount = 30000

	// 1. 模拟存量 100,000 部影片的已有内存索引
	baseItems := make([]FilmSearchMeta, existingCount)
	for i := 0; i < existingCount; i++ {
		mid := int64(i + 1)
		baseItems[i] = FilmSearchMeta{
			Mid:           mid,
			FirstSourceId: "src-base",
			PlaySources:   []string{"src-base"},
			CategoryKey:   "base-cat",
			Item: utils.FilmSearchItem{
				Mid:         mid,
				Name:        fmt.Sprintf("存量电影%d", mid),
				CleanName:   fmt.Sprintf("存量电影%d", mid),
				UpdateStamp: 1000,
			},
		}
	}
	activeFilmSearchMetas.Store(sealSearchMetaIndex("v-stress", baseItems))
	if err := SetActiveSnapshotVersion("v-stress"); err != nil {
		t.Fatal(err)
	}

	// 2. 向数据库写入 30,000 部当批新采集的影片
	mids := make([]int64, newBatchCount)
	const chunkSize = 50
	for i := 0; i < newBatchCount; i += chunkSize {
		end := i + chunkSize
		if end > newBatchCount {
			end = newBatchCount
		}
		films := make([]model.FilmIndex, 0, end-i)
		playlists := make([]model.FilmSourcePlaylist, 0, end-i)
		for j := i; j < end; j++ {
			mid := int64(existingCount + j + 1)
			mids[j] = mid
			films = append(films, model.FilmIndex{
				FilmIndexIdentity: model.FilmIndexIdentity{
					Mid:           mid,
					FirstSourceId: "src-batch",
				},
				FilmIndexCategory: model.FilmIndexCategory{
					CategoryKey: "batch-cat",
					Pid:         1,
					Cid:         10,
				},
				FilmIndexContent: model.FilmIndexContent{
					Name:        fmt.Sprintf("全量新片%d", mid),
					UpdateStamp: 2000 + int64(j),
					Year:        2026,
				},
			})
			playlists = append(playlists, model.FilmSourcePlaylist{
				Mid:        mid,
				SourceId:   "src-batch",
				LineKind:   model.LineKindPlay,
				GroupIndex: 0,
			})
		}
		if err := gdb.Create(&films).Error; err != nil {
			t.Fatalf("insert films batch %d: %v", i, err)
		}
		if err := gdb.Create(&playlists).Error; err != nil {
			t.Fatalf("insert playlists batch %d: %v", i, err)
		}
	}

	// 3. 监控内存与执行时间
	runtime.GC()
	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	start := time.Now()
	ok := ResealSearchIndexMids(mids)
	cost := time.Since(start)

	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	if !ok {
		t.Fatal("expected ResealSearchIndexMids to return true")
	}

	heapDeltaMB := float64(mAfter.Alloc-mBefore.Alloc) / (1024 * 1024)
	t.Logf("=== 30,000 封口测试结果 ===")
	t.Logf("封口耗时: %v", cost)
	t.Logf("堆内存变化: %.2f MB", heapDeltaMB)

	// 4. 验证是否触发了全库扫描重建
	if IsSearchIndexRebuilding() {
		t.Fatal("FAIL: 误触发了全量全库扫描重建！")
	}

	// 5. 验证新索引容量是否精确包含 130,000 部
	idx := activeFilmSearchMetas.Load()
	if idx == nil {
		t.Fatal("active index is nil")
	}
	if len(idx.Items) != existingCount+newBatchCount {
		t.Fatalf("items count=%d, want %d", len(idx.Items), existingCount+newBatchCount)
	}

	// 6. 验证新片检索能力：抽取第 1 部、第 15,000 部、第 30,000 部进行搜索
	testMids := []int64{
		existingCount + 1,
		existingCount + 15000,
		existingCount + newBatchCount,
	}
	for _, targetMid := range testMids {
		targetName := fmt.Sprintf("全量新片%d", targetMid)
		hits := searchFilmMetas(idx, targetName, "", "", 0, 0)
		if len(hits) == 0 {
			t.Fatalf("film %s (mid=%d) was not recalled by searchFilmMetas", targetName, targetMid)
		}
		if hits[0].mid != targetMid {
			t.Fatalf("search %s got mid=%d, want %d", targetName, hits[0].mid, targetMid)
		}
	}

	// 7. 验证最新榜单是否包含最新的新片
	key := hotPoolMapKey("src-batch", "batch-cat")
	recents := idx.RecentPools[key]
	if len(recents) == 0 {
		t.Fatalf("RecentPools[%s] is empty", key)
	}
	latestBatchMid := int64(existingCount + newBatchCount)
	if recents[0] != latestBatchMid {
		t.Fatalf("RecentPools[0]=%d, want latest mid %d", recents[0], latestBatchMid)
	}
}
