package film

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRemoveMidsFromActiveFilmSearchIndex(t *testing.T) {
	testVer := "v_test_remove_mids"
	oldMeta := activeFilmSearchMetas.Load()
	t.Cleanup(func() {
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
	})
	idx := &filmSearchMetaIndex{
		Version: testVer,
		Items: []FilmSearchMeta{
			{Mid: 101, Item: utils.FilmSearchItem{Mid: 101, Name: "仙逆 第一季"}},
			{Mid: 102, Item: utils.FilmSearchItem{Mid: 102, Name: "仙逆 第二季"}},
			{Mid: 103, Item: utils.FilmSearchItem{Mid: 103, Name: "完美世界"}},
		},
	}
	activeFilmSearchMetas.Store(idx)

	// 增量剔除 101
	RemoveMidsFromActiveFilmSearchIndex(testVer, []int64{101})

	cur := activeFilmSearchMetas.Load()
	if cur == nil {
		t.Fatal("expected activeFilmSearchMetas not nil")
	}
	if len(cur.Items) != 2 {
		t.Fatalf("expected 2 items remaining, got %d", len(cur.Items))
	}
	for _, item := range cur.Items {
		if item.Mid == 101 {
			t.Fatalf("expected mid 101 removed, but still found in items")
		}
	}
	if cur.Items[0].Mid != 102 || cur.Items[1].Mid != 103 {
		t.Fatalf("unexpected items remaining: %+v", cur.Items)
	}

	// 再次增量剔除不存在的 mid，索引条目数不变
	RemoveMidsFromActiveFilmSearchIndex(testVer, []int64{999})
	if len(activeFilmSearchMetas.Load().Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(activeFilmSearchMetas.Load().Items))
	}
}

func TestSearchCacheVersion_BumpAndInvalidate(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	defer func() {
		_ = client.Close()
		db.Rdb = origRdb
	}()
	t.Cleanup(func() {
		ResetSearchCacheVersionForTest()
	})

	v1 := GetSearchCacheVersion()
	if v1 == "" {
		t.Fatal("expected non-empty version")
	}

	time.Sleep(10 * time.Millisecond)
	BumpSearchCacheVersion()

	v2 := GetSearchCacheVersion()
	if v2 == "" || v2 == v1 {
		t.Fatalf("expected search cache version changed from %q, got %q", v1, v2)
	}
}

func TestClearSearchCache_DeletesOnlySearchKeys(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	defer func() {
		_ = client.Close()
		db.Rdb = origRdb
	}()
	t.Cleanup(func() {
		ResetSearchCacheVersionForTest()
	})

	// 写入若干搜索缓存与非搜索缓存
	k1 := fmt.Sprintf("%s:v1:sv1:仙逆::p1:s12", config.FilmSearchCachePrefix)
	k2 := fmt.Sprintf("%s:v1:sv1:仙::p1:s12", config.FilmSearchCachePrefix)
	_ = client.Set(db.Cxt, k1, "result1", 0).Err()
	_ = client.Set(db.Cxt, k2, "result2", 0).Err()
	playKey := fmt.Sprintf("%s:101", config.FilmPlayInfoKey)
	tokenKey := fmt.Sprintf(config.UserTokenKey, 123)
	_ = client.Set(db.Cxt, playKey, "play101", 0).Err()
	_ = client.Set(db.Cxt, tokenKey, "token123", 0).Err()

	ClearSearchCache()

	if client.Exists(db.Cxt, k1).Val() != 0 {
		t.Errorf("expected %s to be deleted", k1)
	}
	if client.Exists(db.Cxt, k2).Val() != 0 {
		t.Errorf("expected %s to be deleted", k2)
	}
	if client.Exists(db.Cxt, playKey).Val() != 1 {
		t.Errorf("expected %s to be preserved", playKey)
	}
	if client.Exists(db.Cxt, tokenKey).Val() != 1 {
		t.Errorf("expected %s to be preserved", tokenKey)
	}
}

func TestDelFilmSearch_EndToEndWithSnapshotAndCache(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	defer func() {
		_ = client.Close()
		db.Rdb = origRdb
	}()

	gdb := setupFilmZeroTestDB(t)
	testVer := "v_del_test_1"
	oldVer := GetActiveSnapshotVersion()
	oldRM := GetActiveFilmReadModel()
	oldMeta := activeFilmSearchMetas.Load()
	_ = SetActiveSnapshotVersion(testVer)
	activeFilmReadModel.Store(&FilmReadModel{Version: testVer})
	t.Cleanup(func() {
		WaitAsyncClearSearchCacheDone()
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
		if oldVer != "" {
			_ = SetActiveSnapshotVersion(oldVer)
		} else {
			clearActiveSnapshotVersion()
		}
		if oldRM != nil {
			activeFilmReadModel.Store(oldRM)
		} else {
			activeFilmReadModel.Store(&FilmReadModel{Version: ""})
		}
	})

	// 注入影片数据
	targetMid := int64(888)
	fi := &model.FilmIndex{}
	fi.Mid = targetMid
	fi.Name = "仙逆"
	fi.Pid = 1
	fi.Cid = 10
	gdb.Create(fi)

	gdb.Create(&model.MovieDetailInfo{Mid: targetMid, Content: "{}"})
	gdb.Create(&model.MovieMatchKey{Mid: targetMid, MatchKey: "key_xianni"})
	gdb.Create(&model.MovieSourceMapping{GlobalMid: targetMid, SourceMid: 999, SourceId: "src1"})
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: targetMid, Name: "仙逆", Pid: 1})

	// 初始化内存搜索索引
	initIdx := &filmSearchMetaIndex{
		Version: testVer,
		Items: []FilmSearchMeta{
			{Mid: targetMid, Item: utils.FilmSearchItem{Mid: targetMid, Name: "仙逆"}},
			{Mid: 999, Item: utils.FilmSearchItem{Mid: 999, Name: "凡人修仙传"}},
		},
	}
	activeFilmSearchMetas.Store(initIdx)

	// 模拟前台旧搜索缓存与分类首页聚合缓存
	oldSearchVer := GetSearchCacheVersion()
	oldKey := fmt.Sprintf("%s:v%s:sv%s:仙逆::p1:s12", config.FilmSearchCachePrefix, testVer, oldSearchVer)
	_ = client.Set(db.Cxt, oldKey, "cached_search_content", 0).Err()
	classifyKey := fmt.Sprintf("%s:1:1:12", config.FilmClassifyCacheKey)
	_ = client.Set(db.Cxt, classifyKey, "classify_cached_data", 0).Err()

	// 执行删除
	start := time.Now()
	if err := DelFilmSearch(targetMid); err != nil {
		t.Fatalf("DelFilmSearch failed: %v", err)
	}
	cost := time.Since(start)
	t.Logf("DelFilmSearch cost: %v", cost)

	// 验证数据库记录已删除
	var count int64
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", targetMid).Count(&count)
	if count != 0 {
		t.Errorf("expected FilmIndex deleted, got count=%d", count)
	}
	gdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ? AND mid = ?", testVer, targetMid).Count(&count)
	if count != 0 {
		t.Errorf("expected FilmListSnapshot deleted, got count=%d", count)
	}

	// 验证内存搜索索引已剔除 targetMid
	curIdx := activeFilmSearchMetas.Load()
	if curIdx == nil {
		t.Fatal("expected activeFilmSearchMetas not nil (should not be destroyed)")
	}
	if len(curIdx.Items) != 1 || curIdx.Items[0].Mid != 999 {
		t.Fatalf("expected only mid 999 remaining in memory index, got %+v", curIdx.Items)
	}

	// 验证搜索缓存版本已推高（改由版本号隔离失效，避免无界全量 SCAN 压力）
	newSearchVer := GetSearchCacheVersion()
	if newSearchVer == oldSearchVer {
		t.Fatalf("expected search cache version bumped from %s, but got %s", oldSearchVer, newSearchVer)
	}

	// 验证分类首页等聚合缓存已被 RefreshAccessDataCaches() 清理
	if client.Exists(db.Cxt, classifyKey).Val() != 0 {
		t.Errorf("expected classify cache %s to be deleted by RefreshAccessDataCaches", classifyKey)
	}

	// 验证前台搜索基于新版本号不会命中已删除的影片
	page := &dto.Page{Current: 1, PageSize: 12}
	results := SearchSnapshotsByKeywordAndSortReadModel(testVer, "仙逆", "", page)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for 仙逆 after delete, got %d", len(results))
	}
}

// TestFilmCRUD_FullLifecycle 覆盖完整增、删、改、查闭环
func TestFilmCRUD_FullLifecycle(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	defer func() {
		_ = client.Close()
		db.Rdb = origRdb
	}()

	gdb := setupFilmZeroTestDB(t)
	testVer := "v_crud_lifecycle"
	oldVer := GetActiveSnapshotVersion()
	oldRM := GetActiveFilmReadModel()
	oldMeta := activeFilmSearchMetas.Load()
	_ = SetActiveSnapshotVersion(testVer)
	activeFilmReadModel.Store(&FilmReadModel{Version: testVer})
	t.Cleanup(func() {
		WaitAsyncClearSearchCacheDone()
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
		if oldVer != "" {
			_ = SetActiveSnapshotVersion(oldVer)
		} else {
			clearActiveSnapshotVersion()
		}
		if oldRM != nil {
			activeFilmReadModel.Store(oldRM)
		} else {
			activeFilmReadModel.Store(&FilmReadModel{Version: ""})
		}
	})

	// 初始化一个空基础索引
	activeFilmSearchMetas.Store(&filmSearchMetaIndex{
		Version: testVer,
		Items: []FilmSearchMeta{
			{Mid: 1, Item: utils.FilmSearchItem{Mid: 1, Name: "已有影片"}},
		},
	})

	mid := int64(2026)

	// 1. 【查】创建前：搜索“大话西游”结果为 0
	page := &dto.Page{Current: 1, PageSize: 10}
	res0 := SearchSnapshotsByKeywordAndSortReadModel(testVer, "大话西游", "", page)
	if len(res0) != 0 {
		t.Fatalf("expected 0 results before create, got %d", len(res0))
	}

	// 2. 【增】新增影片：“大话西游之月光宝盒”
	fi := &model.FilmIndex{}
	fi.Mid = mid
	fi.Name = "大话西游之月光宝盒"
	fi.Pid = 1
	fi.Cid = 10
	gdb.Create(fi)
	gdb.Create(&model.MovieDetailInfo{Mid: mid, Content: "{}"})
	gdb.Create(&model.MovieMatchKey{Mid: mid, MatchKey: "key_dhxy"})

	// 触发快照增量构建
	_, _, err = UpsertActiveSnapshotsByMids(mid)
	if err != nil {
		t.Fatalf("UpsertActiveSnapshotsByMids create failed: %v", err)
	}

	// 验证【增】之后立即能搜到
	page = &dto.Page{Current: 1, PageSize: 10}
	resAdd := SearchSnapshotsByKeywordAndSortReadModel(testVer, "大话西游", "", page)
	if len(resAdd) != 1 || resAdd[0].Mid != mid {
		t.Fatalf("expected 1 result (mid %d) after add, got %+v", mid, resAdd)
	}

	// 3. 【改】修改片名：“大话西游之大圣娶亲”
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", mid).Update("name", "大话西游之大圣娶亲")
	_, _, err = UpsertActiveSnapshotsByMids(mid)
	if err != nil {
		t.Fatalf("UpsertActiveSnapshotsByMids update failed: %v", err)
	}

	// 验证【改】之后立即能搜出新片名
	page = &dto.Page{Current: 1, PageSize: 10}
	resUpdate := SearchSnapshotsByKeywordAndSortReadModel(testVer, "大圣娶亲", "", page)
	if len(resUpdate) != 1 || resUpdate[0].Mid != mid {
		t.Fatalf("expected 1 result (mid %d) for 大圣娶亲 after update, got %+v", mid, resUpdate)
	}

	// 验证旧搜索词“月光宝盒”搜不到
	page = &dto.Page{Current: 1, PageSize: 10}
	resOldKeyword := SearchSnapshotsByKeywordAndSortReadModel(testVer, "月光宝盒", "", page)
	if len(resOldKeyword) != 0 {
		t.Fatalf("expected 0 results for 月光宝盒 after rename, got %d", len(resOldKeyword))
	}

	// 4. 【删】删除该影片
	if err := DelFilmSearch(mid); err != nil {
		t.Fatalf("DelFilmSearch failed: %v", err)
	}

	// 验证【删】之后搜索“大话西游”或“大圣娶亲”结果为 0
	page = &dto.Page{Current: 1, PageSize: 10}
	resDel := SearchSnapshotsByKeywordAndSortReadModel(testVer, "大话西游", "", page)
	if len(resDel) != 0 {
		t.Fatalf("expected 0 results for 大话西游 after delete, got %d", len(resDel))
	}
}

// TestUpsertMidsToActiveFilmSearchIndex_RemovesGhostItems 验证增量更新时自动剔除不存在于快照的幽灵条目
func TestUpsertMidsToActiveFilmSearchIndex_RemovesGhostItems(t *testing.T) {
	gdb := setupFilmZeroTestDB(t)
	testVer := "v_test_ghost_removal"

	oldMeta := activeFilmSearchMetas.Load()
	oldVer := GetActiveSnapshotVersion()
	_ = SetActiveSnapshotVersion(testVer)
	t.Cleanup(func() {
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
		if oldVer != "" {
			_ = SetActiveSnapshotVersion(oldVer)
		} else {
			clearActiveSnapshotVersion()
		}
	})

	// 准备 DB 中的快照数据：只有 mid 101 和 mid 103，mid 102 是快照中不存在的幽灵数据
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: 101, Name: "存活影片101", Pid: 1})
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: 103, Name: "无关影片103", Pid: 1})

	// 初始化内存索引包含 101, 102, 103
	activeFilmSearchMetas.Store(&filmSearchMetaIndex{
		Version: testVer,
		Items: []FilmSearchMeta{
			{Mid: 101, Item: utils.FilmSearchItem{Mid: 101, Name: "旧影片101"}},
			{Mid: 102, Item: utils.FilmSearchItem{Mid: 102, Name: "幽灵影片102"}},
			{Mid: 103, Item: utils.FilmSearchItem{Mid: 103, Name: "无关影片103"}},
		},
	})

	// 1. 增量更新包含 101 和 102。101 存在于 DB 应被更新，102 不存在于 DB 应被剔除，103 不在传入列表中应保持不变
	UpsertMidsToActiveFilmSearchIndex(testVer, []int64{101, 102})

	cur := activeFilmSearchMetas.Load()
	if cur == nil {
		t.Fatal("expected activeFilmSearchMetas not nil")
	}
	if len(cur.Items) != 2 {
		t.Fatalf("expected 2 items, got %d: %+v", len(cur.Items), cur.Items)
	}
	found101 := false
	found102 := false
	found103 := false
	for _, it := range cur.Items {
		if it.Mid == 101 {
			found101 = true
			if it.Item.Name != "存活影片101" {
				t.Errorf("expected 101 updated name '存活影片101', got %s", it.Item.Name)
			}
		}
		if it.Mid == 102 {
			found102 = true
		}
		if it.Mid == 103 {
			found103 = true
		}
	}
	if !found101 {
		t.Error("expected mid 101 in active items")
	}
	if found102 {
		t.Error("expected ghost mid 102 to be removed from active items")
	}
	if !found103 {
		t.Error("expected untouched mid 103 to remain in active items")
	}

	// 2. 边界测试：DB 查出 0 行场景（len(rows) == 0）。再次 upsert mid 101，但在 DB 中先删除 mid 101
	gdb.Where("snapshot_version = ? AND mid = ?", testVer, 101).Delete(&model.FilmListSnapshot{})
	UpsertMidsToActiveFilmSearchIndex(testVer, []int64{101})

	cur2 := activeFilmSearchMetas.Load()
	if len(cur2.Items) != 1 || cur2.Items[0].Mid != 103 {
		t.Fatalf("expected only mid 103 remaining after 0-row DB upsert, got %+v", cur2.Items)
	}
}

// TestUpsertMidsToActiveFilmSearchIndex_BatchChunking 验证大批量 MIDs 分批查询正常工作
func TestUpsertMidsToActiveFilmSearchIndex_BatchChunking(t *testing.T) {
	gdb := setupFilmZeroTestDB(t)
	testVer := "v_test_chunking"

	oldMeta := activeFilmSearchMetas.Load()
	oldVer := GetActiveSnapshotVersion()
	_ = SetActiveSnapshotVersion(testVer)
	t.Cleanup(func() {
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
		if oldVer != "" {
			_ = SetActiveSnapshotVersion(oldVer)
		} else {
			clearActiveSnapshotVersion()
		}
	})

	// 插入 600 条快照记录（超过 batchSize 500）
	const count = 600
	var snaps []model.FilmListSnapshot
	var mids []int64
	for i := int64(1); i <= count; i++ {
		mid := 10000 + i
		mids = append(mids, mid)
		snaps = append(snaps, model.FilmListSnapshot{
			SnapshotVersion: testVer,
			Mid:             mid,
			Name:            fmt.Sprintf("分批影片%d", i),
			Pid:             1,
		})
	}
	if err := gdb.CreateInBatches(snaps, 200).Error; err != nil {
		t.Fatalf("CreateInBatches failed: %v", err)
	}

	activeFilmSearchMetas.Store(&filmSearchMetaIndex{
		Version: testVer,
		Items:   []FilmSearchMeta{},
	})

	UpsertMidsToActiveFilmSearchIndex(testVer, mids)

	cur := activeFilmSearchMetas.Load()
	if cur == nil || len(cur.Items) != count {
		t.Fatalf("expected %d items loaded via chunking, got %v", count, len(cur.Items))
	}
}

// TestSearchSnapshotsByKeywordAndSortReadModel_SingleFlightStability 验证 SingleFlight 并发防击穿及冷启动稳定性
func TestSearchSnapshotsByKeywordAndSortReadModel_SingleFlightStability(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	defer func() {
		_ = client.Close()
		db.Rdb = origRdb
	}()

	gdb := setupFilmZeroTestDB(t)
	testVer := "v_sf_stability"
	oldMeta := activeFilmSearchMetas.Load()
	oldVer := GetActiveSnapshotVersion()
	_ = SetActiveSnapshotVersion(testVer)
	t.Cleanup(func() {
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
		if oldVer != "" {
			_ = SetActiveSnapshotVersion(oldVer)
		} else {
			clearActiveSnapshotVersion()
		}
	})

	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: 501, Name: "斗罗大陆 第一季", Pid: 1})
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: 502, Name: "斗罗大陆 第二季", Pid: 1})

	activeFilmSearchMetas.Store(&filmSearchMetaIndex{
		Version: testVer,
		Items: []FilmSearchMeta{
			{Mid: 501, Item: utils.FilmSearchItem{Mid: 501, Name: "斗罗大陆 第一季"}},
			{Mid: 502, Item: utils.FilmSearchItem{Mid: 502, Name: "斗罗大陆 第二季"}},
		},
	})

	// 50 个并发协程同时查询相同关键字
	const concurrency = 50
	var wg sync.WaitGroup
	wg.Add(concurrency)
	errChan := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			page := &dto.Page{Current: 1, PageSize: 10}
			results := SearchSnapshotsByKeywordAndSortReadModel(testVer, "斗罗大陆", "", page)
			if len(results) != 2 {
				errChan <- fmt.Errorf("expected 2 results, got %d", len(results))
				return
			}
			if page.Total != 2 {
				errChan <- fmt.Errorf("expected page.Total 2, got %d", page.Total)
				return
			}
		}()
	}
	wg.Wait()
	close(errChan)

	for e := range errChan {
		t.Fatal(e)
	}

	// 验证 Redis 中生成了正确版本的缓存 key
	keys := client.Keys(db.Cxt, "*").Val()
	foundCache := false
	for _, k := range keys {
		if strings.HasPrefix(k, config.FilmSearchCachePrefix+":v"+testVer+":sv") {
			foundCache = true
			break
		}
	}
	if !foundCache {
		t.Fatalf("expected search cache key with prefix %s in Redis, got keys: %v", config.FilmSearchCachePrefix, keys)
	}
}

// TestDelFilmSearch_CleansSnapshotWhenFilmIndexMissing 验证当 FilmIndex 不存在时仍能正确删除快照及清理内存索引
func TestDelFilmSearch_CleansSnapshotWhenFilmIndexMissing(t *testing.T) {
	gdb := setupFilmZeroTestDB(t)
	testVer := "v_del_orphan_test"

	oldMeta := activeFilmSearchMetas.Load()
	oldVer := GetActiveSnapshotVersion()
	_ = SetActiveSnapshotVersion(testVer)
	t.Cleanup(func() {
		WaitAsyncClearSearchCacheDone()
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
		if oldVer != "" {
			_ = SetActiveSnapshotVersion(oldVer)
		} else {
			clearActiveSnapshotVersion()
		}
	})

	targetMid := int64(777)
	// 在 FilmListSnapshot 中存在孤儿快照记录，但 FilmIndex 已经不存在（info == nil）
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: targetMid, Name: "孤儿快照影片", Pid: 1})

	activeFilmSearchMetas.Store(&filmSearchMetaIndex{
		Version: testVer,
		Items: []FilmSearchMeta{
			{Mid: targetMid, Item: utils.FilmSearchItem{Mid: targetMid, Name: "孤儿快照影片"}},
			{Mid: 888, Item: utils.FilmSearchItem{Mid: 888, Name: "其他影片"}},
		},
	})

	if err := DelFilmSearch(targetMid); err != nil {
		t.Fatalf("DelFilmSearch failed: %v", err)
	}

	// 验证即使 FilmIndex 为 nil，FilmListSnapshot 依然被删除
	var count int64
	gdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ? AND mid = ?", testVer, targetMid).Count(&count)
	if count != 0 {
		t.Errorf("expected FilmListSnapshot deleted even when FilmIndex was missing, got count=%d", count)
	}

	// 验证内存索引也被清理
	curIdx := activeFilmSearchMetas.Load()
	if curIdx == nil || len(curIdx.Items) != 1 || curIdx.Items[0].Mid != 888 {
		t.Fatalf("expected only mid 888 remaining in memory index, got %+v", curIdx.Items)
	}
}

// TestSearchCacheVersion_NoCrossVersionPollution 验证并发版本失效时旧检索不会污染新版本 Redis Key
func TestSearchCacheVersion_NoCrossVersionPollution(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	defer func() {
		WaitAsyncClearSearchCacheDone()
		_ = client.Close()
		db.Rdb = origRdb
	}()

	gdb := setupFilmZeroTestDB(t)
	testVer := "v_pollution_test"
	oldMeta := activeFilmSearchMetas.Load()
	oldVer := GetActiveSnapshotVersion()
	_ = SetActiveSnapshotVersion(testVer)
	t.Cleanup(func() {
		WaitAsyncClearSearchCacheDone()
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
		if oldVer != "" {
			_ = SetActiveSnapshotVersion(oldVer)
		} else {
			clearActiveSnapshotVersion()
		}
	})

	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: 101, Name: "斗罗大陆 第一季", Pid: 1})
	activeFilmSearchMetas.Store(&filmSearchMetaIndex{
		Version: testVer,
		Items: []FilmSearchMeta{
			{Mid: 101, Item: utils.FilmSearchItem{Mid: 101, Name: "斗罗大陆 第一季"}},
		},
	})

	// 记录初始版本 v1
	v1 := GetSearchCacheVersion()

	// 模拟在检索执行前版本被推高至 v2
	BumpSearchCacheVersion()
	v2 := GetSearchCacheVersion()
	if v1 == v2 {
		t.Fatalf("expected v1 != v2, got v1=%s v2=%s", v1, v2)
	}

	// 检索执行，其缓存 key 应当基于当前最新的 v2，且不会有任何 v1 的脏 key 泄露进 v2
	page := &dto.Page{Current: 1, PageSize: 10}
	results := SearchSnapshotsByKeywordAndSortReadModel(testVer, "斗罗大陆", "", page)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// 验证 v2 缓存正常生成
	v2Key := fmt.Sprintf("%s:v%s:sv%s:斗罗大陆::p1:s10", config.FilmSearchCachePrefix, testVer, v2)
	if client.Exists(db.Cxt, v2Key).Val() != 1 {
		t.Errorf("expected v2 key %s to exist", v2Key)
	}

	// 验证 v1 key 不会被生成
	v1Key := fmt.Sprintf("%s:v%s:sv%s:斗罗大陆::p1:s10", config.FilmSearchCachePrefix, testVer, v1)
	if client.Exists(db.Cxt, v1Key).Val() != 0 {
		t.Errorf("expected v1 key %s NOT to exist", v1Key)
	}
}

// TestUpsertMidsToActiveFilmSearchIndex_ColdStartAndReload 验证冷启动或版本不匹配时同步加载基础索引并合并增量，不发生静默丢弃
func TestUpsertMidsToActiveFilmSearchIndex_ColdStartAndReload(t *testing.T) {
	gdb := setupFilmZeroTestDB(t)
	testVer := "v_upsert_cold_start"
	oldMeta := activeFilmSearchMetas.Load()
	oldVer := GetActiveSnapshotVersion()
	_ = SetActiveSnapshotVersion(testVer)
	t.Cleanup(func() {
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
		if oldVer != "" {
			_ = SetActiveSnapshotVersion(oldVer)
		} else {
			clearActiveSnapshotVersion()
		}
	})

	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: 101, Name: "遮天 第一季", Pid: 1})
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: 102, Name: "遮天 第二季", Pid: 1})

	// 1. 冷启动场景：内存索引为 nil
	activeFilmSearchMetas.Store(nil)
	UpsertMidsToActiveFilmSearchIndex(testVer, []int64{101})

	cur := activeFilmSearchMetas.Load()
	if cur == nil {
		t.Fatal("expected activeFilmSearchMetas not nil after cold start upsert")
	}
	if cur.Version != testVer {
		t.Fatalf("expected version %s, got %s", testVer, cur.Version)
	}
	if len(cur.Items) != 2 {
		t.Fatalf("expected 2 items loaded and merged, got %d", len(cur.Items))
	}

	// 2. 版本不匹配场景：当前内存索引版本为旧版本
	activeFilmSearchMetas.Store(&filmSearchMetaIndex{
		Version: "v_old_version",
		Items:   []FilmSearchMeta{{Mid: 999, Item: utils.FilmSearchItem{Mid: 999, Name: "旧影片"}}},
	})
	UpsertMidsToActiveFilmSearchIndex(testVer, []int64{102})

	curReload := activeFilmSearchMetas.Load()
	if curReload == nil {
		t.Fatal("expected activeFilmSearchMetas not nil after version reload upsert")
	}
	if curReload.Version != testVer {
		t.Fatalf("expected version %s, got %s", testVer, curReload.Version)
	}
	if len(curReload.Items) != 2 {
		t.Fatalf("expected 2 items loaded for new version, got %d", len(curReload.Items))
	}
}

// TestRemoveMidsFromActiveFilmSearchIndex_ColdStartAndReload 验证冷启动或版本不匹配时剔除增量数据不会被静默丢弃
func TestRemoveMidsFromActiveFilmSearchIndex_ColdStartAndReload(t *testing.T) {
	gdb := setupFilmZeroTestDB(t)
	testVer := "v_remove_cold_start"
	oldMeta := activeFilmSearchMetas.Load()
	oldVer := GetActiveSnapshotVersion()
	_ = SetActiveSnapshotVersion(testVer)
	t.Cleanup(func() {
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
		if oldVer != "" {
			_ = SetActiveSnapshotVersion(oldVer)
		} else {
			clearActiveSnapshotVersion()
		}
	})

	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: 201, Name: "凡人修仙传 仙界篇", Pid: 1})
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: 202, Name: "凡人修仙传 重置版", Pid: 1})

	// 1. 冷启动场景：内存索引为 nil，增量剔除 201
	activeFilmSearchMetas.Store(nil)
	RemoveMidsFromActiveFilmSearchIndex(testVer, []int64{201})

	cur := activeFilmSearchMetas.Load()
	if cur == nil {
		t.Fatal("expected activeFilmSearchMetas not nil after cold start remove")
	}
	if cur.Version != testVer {
		t.Fatalf("expected version %s, got %s", testVer, cur.Version)
	}
	if len(cur.Items) != 1 || cur.Items[0].Mid != 202 {
		t.Fatalf("expected only mid 202 remaining after cold start remove, got %+v", cur.Items)
	}

	// 2. 版本不匹配场景：当前内存索引为旧版本，剔除 202
	activeFilmSearchMetas.Store(&filmSearchMetaIndex{
		Version: "v_old_version",
		Items:   []FilmSearchMeta{{Mid: 888, Item: utils.FilmSearchItem{Mid: 888, Name: "旧数据"}}},
	})
	RemoveMidsFromActiveFilmSearchIndex(testVer, []int64{202})

	curReload := activeFilmSearchMetas.Load()
	if curReload == nil {
		t.Fatal("expected activeFilmSearchMetas not nil after reload remove")
	}
	if curReload.Version != testVer {
		t.Fatalf("expected version %s, got %s", testVer, curReload.Version)
	}
	// 基础索引原有 201 和 202，剔除 202 后应只剩 201
	if len(curReload.Items) != 1 || curReload.Items[0].Mid != 201 {
		t.Fatalf("expected only mid 201 remaining after reload remove, got %+v", curReload.Items)
	}
}

// TestDeleteActiveSnapshotsByMids_RefreshesAggregateCaches 验证删除快照时清理分类首页等聚合缓存
func TestDeleteActiveSnapshotsByMids_RefreshesAggregateCaches(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	defer func() {
		_ = client.Close()
		db.Rdb = origRdb
	}()

	gdb := setupFilmZeroTestDB(t)
	testVer := "v_del_aggregate_cache"
	oldVer := GetActiveSnapshotVersion()
	_ = SetActiveSnapshotVersion(testVer)
	t.Cleanup(func() {
		ResetSearchCacheVersionForTest()
		if oldVer != "" {
			_ = SetActiveSnapshotVersion(oldVer)
		} else {
			clearActiveSnapshotVersion()
		}
	})

	targetMid := int64(301)
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: targetMid, Name: "分类测试片", Pid: 1, Cid: 10})

	// 模拟写入分类首页聚合缓存与首页缓存
	classifyKey := fmt.Sprintf("%s:1:10:12", config.FilmClassifyCacheKey)
	indexKey := fmt.Sprintf("%s:home_page", config.IndexPageCacheKey)
	_ = client.Set(db.Cxt, classifyKey, "classify_data", 0).Err()
	_ = client.Set(db.Cxt, indexKey, "index_data", 0).Err()

	DeleteActiveSnapshotsByMids(targetMid)

	// 验证分类首页等聚合缓存已被清空
	if client.Exists(db.Cxt, classifyKey).Val() != 0 {
		t.Errorf("expected classify cache %s to be deleted", classifyKey)
	}
	if client.Exists(db.Cxt, indexKey).Val() != 0 {
		t.Errorf("expected index cache %s to be deleted", indexKey)
	}
}

// TestLoadFilmSearchMetaIndex_EmptySnapshotCached 验证空快照索引被正常缓存，不会因 len(Items)==0 而重复穿透查询数据库
func TestLoadFilmSearchMetaIndex_EmptySnapshotCached(t *testing.T) {
	_ = setupFilmZeroTestDB(t)
	testVer := "v_empty_snapshot_cache"
	oldMeta := activeFilmSearchMetas.Load()
	t.Cleanup(func() {
		activeFilmSearchMetas.Store(oldMeta)
		ResetSearchCacheVersionForTest()
	})

	activeFilmSearchMetas.Store(nil)

	// 首次载入：数据库中无任何记录，应成功载入并缓存一个非 nil 且 Items 为空的索引
	idx1 := loadFilmSearchMetaIndex(testVer)
	if idx1 == nil {
		t.Fatal("expected non-nil idx1 for empty snapshot")
	}
	if idx1.Version != testVer || len(idx1.Items) != 0 {
		t.Fatalf("expected version %s with 0 items, got version=%s items=%d", testVer, idx1.Version, len(idx1.Items))
	}

	// 二次载入：应直接返回内存中已缓存的指针对象，不重复穿透 SingleFlight 查库
	idx2 := loadFilmSearchMetaIndex(testVer)
	if idx2 != idx1 {
		t.Fatalf("expected idx2 to be identical pointer to cached idx1 (%p != %p)", idx1, idx2)
	}
}

// TestSearchCacheVersion_ConcurrentSetNXSafety 验证并发冷启动获取搜索版本号时版本一致性
func TestSearchCacheVersion_ConcurrentSetNXSafety(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	defer func() {
		_ = client.Close()
		db.Rdb = origRdb
	}()
	t.Cleanup(func() {
		ResetSearchCacheVersionForTest()
	})

	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)
	versions := make([]string, concurrency)

	for i := 0; i < concurrency; i++ {
		idx := i
		go func() {
			defer wg.Done()
			versions[idx] = GetSearchCacheVersion()
		}()
	}
	wg.Wait()

	firstVer := versions[0]
	if firstVer == "" {
		t.Fatal("expected non-empty version")
	}
	for i, ver := range versions {
		if ver != firstVer {
			t.Fatalf("version mismatch at goroutine %d: %q vs %q", i, ver, firstVer)
		}
	}
}

// TestDeleteActiveSnapshotsByCategory_BumpsSearchCacheVersion 验证分类删除时同步推高搜索版本号并刷新聚合缓存
func TestDeleteActiveSnapshotsByCategory_BumpsSearchCacheVersion(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	defer func() {
		_ = client.Close()
		db.Rdb = origRdb
	}()

	gdb := setupFilmZeroTestDB(t)
	testVer := "v_cat_delete_bump"
	oldVer := GetActiveSnapshotVersion()
	_ = SetActiveSnapshotVersion(testVer)
	t.Cleanup(func() {
		ResetSearchCacheVersionForTest()
		if oldVer != "" {
			_ = SetActiveSnapshotVersion(oldVer)
		} else {
			clearActiveSnapshotVersion()
		}
	})

	gdb.Create(&model.Category{Id: 50, Show: true})
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: testVer, Mid: 401, Name: "分类影片401", Pid: 1, Cid: 50})

	vBefore := GetSearchCacheVersion()

	DeleteActiveSnapshotsByCategory("cid", 50)

	vAfter := GetSearchCacheVersion()
	if vBefore == vAfter {
		t.Fatalf("expected search cache version bumped after DeleteActiveSnapshotsByCategory, got %s", vAfter)
	}
}



