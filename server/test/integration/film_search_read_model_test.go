package integration

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
	filmrepo "server/internal/repository/film"
	filmsnapshot "server/internal/repository/film/snapshot"
)

// 增量剔除后，内存检索索引应立即不再命中被剔除影片，且不影响其余影片。
func TestRemoveMidsFromActiveFilmSearchIndex(t *testing.T) {
	gdb := newTestDB(t)
	version := "v_it_remove_mids"
	seedSnapshots(t, gdb, version,
		model.FilmListSnapshot{Mid: 101, Pid: 1, Name: "仙逆 第一季"},
		model.FilmListSnapshot{Mid: 102, Pid: 1, Name: "仙逆 第二季"},
		model.FilmListSnapshot{Mid: 103, Pid: 1, Name: "完美世界"},
	)
	activateVersion(t, version)

	rows, page := searchFilms(version, "仙逆", 10)
	assertHit(t, "剔除前", "仙逆", rows, page, 101, 102)

	filmsnapshot.RemoveMidsFromActiveFilmSearchIndex(version, []int64{101})

	rows, page = searchFilms(version, "仙逆", 10)
	assertHit(t, "剔除后", "仙逆", rows, page, 102)

	rows, page = searchFilms(version, "完美世界", 10)
	assertHit(t, "剔除后", "完美世界", rows, page, 103)

	// 再次剔除不存在的 mid：命中集合不变
	filmsnapshot.RemoveMidsFromActiveFilmSearchIndex(version, []int64{999})
	rows, page = searchFilms(version, "仙逆", 10)
	assertHit(t, "剔除不存在的 mid 后", "仙逆", rows, page, 102)
}

// 搜索缓存版本号自增后必须与旧值不同。
func TestSearchCacheVersion_BumpAndInvalidate(t *testing.T) {
	newTestDB(t)
	useRedis(t)

	v1 := filmsnapshot.GetSearchCacheVersion()
	if v1 == "" {
		t.Fatal("搜索缓存版本号不应为空")
	}

	time.Sleep(10 * time.Millisecond)
	filmsnapshot.BumpSearchCacheVersion()

	v2 := filmsnapshot.GetSearchCacheVersion()
	if v2 == "" || v2 == v1 {
		t.Fatalf("推高后版本号应变化，实际 v1=%q v2=%q", v1, v2)
	}
}

// 清空搜索缓存只能删除搜索类 key，不得误删播放信息与用户令牌。
func TestClearSearchCache_DeletesOnlySearchKeys(t *testing.T) {
	newTestDB(t)
	client := useRedis(t)

	searchKey1 := fmt.Sprintf("%s:v1:sv1:仙逆::p1:s12", config.FilmSearchCachePrefix)
	searchKey2 := fmt.Sprintf("%s:v1:sv1:仙::p1:s12", config.FilmSearchCachePrefix)
	playKey := fmt.Sprintf("%s:101", config.FilmPlayInfoKey)
	tokenKey := fmt.Sprintf(config.UserTokenKey, 123)
	for key, val := range map[string]string{
		searchKey1: "result1",
		searchKey2: "result2",
		playKey:    "play101",
		tokenKey:   "token123",
	} {
		if err := client.Set(db.Cxt, key, val, 0).Err(); err != nil {
			t.Fatalf("预置缓存 %s 失败: %v", key, err)
		}
	}

	filmsnapshot.ClearSearchCache()

	for _, key := range []string{searchKey1, searchKey2} {
		if client.Exists(db.Cxt, key).Val() != 0 {
			t.Errorf("搜索缓存 %s 应被删除", key)
		}
	}
	for _, key := range []string{playKey, tokenKey} {
		if client.Exists(db.Cxt, key).Val() != 1 {
			t.Errorf("非搜索缓存 %s 应被保留", key)
		}
	}
}

// 删除影片的全链路：DB 记录、内存索引、搜索缓存版本、聚合缓存同步收敛。
func TestDelFilmSearch_EndToEndWithSnapshotAndCache(t *testing.T) {
	gdb := newTestDB(t)
	client := useRedis(t)

	version := "v_it_del_e2e"
	targetMid := int64(888)
	seedSnapshots(t, gdb, version,
		model.FilmListSnapshot{Mid: targetMid, Pid: 1, Cid: 10, Name: "仙逆"},
		model.FilmListSnapshot{Mid: 999, Pid: 1, Cid: 10, Name: "凡人修仙传"},
	)
	newFilmIndex := &model.FilmIndex{}
	newFilmIndex.Mid = targetMid
	newFilmIndex.Pid = 1
	newFilmIndex.Cid = 10
	newFilmIndex.Name = "仙逆"
	gdb.Create(newFilmIndex)
	gdb.Create(&model.MovieDetailInfo{Mid: targetMid, Content: "{}"})
	gdb.Create(&model.MovieMatchKey{Mid: targetMid, MatchKey: "key_xianni"})
	gdb.Create(&model.MovieSourceMapping{GlobalMid: targetMid, SourceMid: 999, SourceId: "src1"})
	activateVersion(t, version)

	// 模拟前台旧搜索缓存与分类首页聚合缓存
	oldSearchVer := filmsnapshot.GetSearchCacheVersion()
	staleKey := fmt.Sprintf("%s:v%s:sv%s:仙逆::p1:s12", config.FilmSearchCachePrefix, version, oldSearchVer)
	classifyKey := fmt.Sprintf("%s:1:1:12", config.FilmClassifyCacheKey)
	for key, val := range map[string]string{staleKey: "cached_search_content", classifyKey: "classify_cached_data"} {
		if err := client.Set(db.Cxt, key, val, 0).Err(); err != nil {
			t.Fatalf("预置缓存 %s 失败: %v", key, err)
		}
	}

	if err := filmrepo.DelFilmSearch(targetMid); err != nil {
		t.Fatalf("DelFilmSearch 失败: %v", err)
	}

	for _, probe := range []struct {
		model any
		where string
		args  []any
	}{
		{&model.FilmIndex{}, "mid = ?", []any{targetMid}},
		{&model.FilmListSnapshot{}, "snapshot_version = ? AND mid = ?", []any{version, targetMid}},
	} {
		var count int64
		gdb.Model(probe.model).Where(probe.where, probe.args...).Count(&count)
		if count != 0 {
			t.Errorf("删除后仍存在残留记录 %T count=%d", probe.model, count)
		}
	}

	rows, page := searchFilms(version, "仙逆", 10)
	assertMiss(t, "删除后", "仙逆", rows, page)

	rows, page = searchFilms(version, "凡人", 10)
	assertHit(t, "删除后", "凡人", rows, page, 999)

	if newVer := filmsnapshot.GetSearchCacheVersion(); newVer == oldSearchVer {
		t.Errorf("删除后搜索缓存版本号应推高，仍为 %s", newVer)
	}
	if client.Exists(db.Cxt, classifyKey).Val() != 0 {
		t.Errorf("分类首页聚合缓存 %s 应被清理", classifyKey)
	}
}

// 增删改查闭环：每次变更后前台检索立即可见，删除后立即不可见。
func TestFilmCRUD_FullLifecycle(t *testing.T) {
	gdb := newTestDB(t)
	useRedis(t)

	version := "v_it_crud_lifecycle"
	activateVersion(t, version)
	mid := int64(2026)

	rows, page := searchFilms(version, "大话西游", 10)
	assertMiss(t, "创建前", "大话西游", rows, page)

	newFilmIndex := &model.FilmIndex{}
	newFilmIndex.Mid = mid
	newFilmIndex.Pid = 1
	newFilmIndex.Cid = 10
	newFilmIndex.Name = "大话西游之月光宝盒"
	gdb.Create(newFilmIndex)
	gdb.Create(&model.MovieDetailInfo{Mid: mid, Content: "{}"})
	gdb.Create(&model.MovieMatchKey{Mid: mid, MatchKey: "key_dhxy"})
	if _, _, err := filmsnapshot.UpsertActiveSnapshotsByMids(mid); err != nil {
		t.Fatalf("新增影片后更新快照失败: %v", err)
	}

	rows, page = searchFilms(version, "大话西游", 10)
	assertHit(t, "新增后", "大话西游", rows, page, mid)

	gdb.Model(&model.FilmIndex{}).Where("mid = ?", mid).Update("name", "大话西游之大圣娶亲")
	if _, _, err := filmsnapshot.UpsertActiveSnapshotsByMids(mid); err != nil {
		t.Fatalf("改名后更新快照失败: %v", err)
	}

	rows, page = searchFilms(version, "大圣娶亲", 10)
	assertHit(t, "改名后", "大圣娶亲", rows, page, mid)

	rows, page = searchFilms(version, "月光宝盒", 10)
	assertMiss(t, "改名后", "月光宝盒", rows, page)

	if err := filmrepo.DelFilmSearch(mid); err != nil {
		t.Fatalf("DelFilmSearch 失败: %v", err)
	}

	rows, page = searchFilms(version, "大话西游", 10)
	assertMiss(t, "删除后", "大话西游", rows, page)
}

// 增量更新时必须剔除「快照中已不存在」的幽灵条目，并保留未参与本次更新的条目。
func TestUpsertMidsToActiveFilmSearchIndex_RemovesGhostItems(t *testing.T) {
	gdb := newTestDB(t)
	version := "v_it_ghost_removal"
	seedSnapshots(t, gdb, version,
		model.FilmListSnapshot{Mid: 101, Pid: 1, Name: "旧影片101"},
		model.FilmListSnapshot{Mid: 102, Pid: 1, Name: "幽灵影片102"},
		model.FilmListSnapshot{Mid: 103, Pid: 1, Name: "无关影片103"},
	)
	activateVersion(t, version)

	// 让 102 从库中消失（快照链路硬删），此时内存索引仍残留 102，构成幽灵条目
	gdb.Unscoped().Where("snapshot_version = ? AND mid = ?", version, 102).Delete(&model.FilmListSnapshot{})
	gdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ? AND mid = ?", version, 101).Update("name", "存活影片101")

	filmsnapshot.UpsertMidsToActiveFilmSearchIndex(version, []int64{101, 102})

	rows, page := searchFilms(version, "存活影片101", 10)
	assertHit(t, "增量更新后", "存活影片101", rows, page, 101)

	rows, page = searchFilms(version, "旧影片101", 10)
	assertMiss(t, "增量更新后", "旧影片101", rows, page)

	rows, page = searchFilms(version, "幽灵影片102", 10)
	assertMiss(t, "增量更新后", "幽灵影片102", rows, page)

	rows, page = searchFilms(version, "无关影片103", 10)
	assertHit(t, "增量更新后", "无关影片103", rows, page, 103)

	// 边界：DB 查询 0 行时，对应 mid 必须从索引中剔除
	gdb.Unscoped().Where("snapshot_version = ? AND mid = ?", version, 101).Delete(&model.FilmListSnapshot{})
	filmsnapshot.UpsertMidsToActiveFilmSearchIndex(version, []int64{101})

	rows, page = searchFilms(version, "存活影片101", 10)
	assertMiss(t, "库中 0 行时", "存活影片101", rows, page)

	rows, page = searchFilms(version, "无关影片103", 10)
	assertHit(t, "库中 0 行时", "无关影片103", rows, page, 103)
}

// 超过单批上限（500）的增量更新必须分批查询后完整入库。
func TestUpsertMidsToActiveFilmSearchIndex_BatchChunking(t *testing.T) {
	gdb := newTestDB(t)
	version := "v_it_chunking"

	const count = 600
	mids := make([]int64, 0, count)
	rows := make([]model.FilmListSnapshot, 0, count)
	for i := int64(1); i <= count; i++ {
		mid := 10000 + i
		mids = append(mids, mid)
		rows = append(rows, model.FilmListSnapshot{SnapshotVersion: version, Mid: mid, Pid: 1, Name: fmt.Sprintf("分批影片%d", i)})
	}
	if err := gdb.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatalf("批量写入快照失败: %v", err)
	}
	activateVersion(t, version)

	// 冷启动（内存索引为空）后一次性增量更新全部 mid
	filmsnapshot.ClearActiveFilmReadModel()
	filmsnapshot.UpsertMidsToActiveFilmSearchIndex(version, mids)

	hits, page := searchFilms(version, "分批影片", count+1)
	if page.Total != count || len(hits) != count {
		t.Fatalf("分批更新后应命中 %d 条，实际 %d 条（total=%d）", count, len(hits), page.Total)
	}
}

// 并发同关键词检索必须稳定命中，且 Redis 中生成正确版本号的缓存 key。
func TestSearchSnapshotsByKeywordAndSortReadModel_SingleFlightStability(t *testing.T) {
	gdb := newTestDB(t)
	client := useRedis(t)

	version := "v_it_singleflight"
	seedSnapshots(t, gdb, version,
		model.FilmListSnapshot{Mid: 501, Pid: 1, Name: "斗罗大陆 第一季"},
		model.FilmListSnapshot{Mid: 502, Pid: 1, Name: "斗罗大陆 第二季"},
	)
	activateVersion(t, version)

	const concurrency = 50
	var wg sync.WaitGroup
	wg.Add(concurrency)
	errChan := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			page := &dto.Page{Current: 1, PageSize: 10}
			results := filmsnapshot.SearchSnapshotsByKeywordAndSortReadModel(version, "斗罗大陆", "", page)
			if len(results) != 2 {
				errChan <- fmt.Errorf("期望命中 2 条，实际 %d 条", len(results))
				return
			}
			if page.Total != 2 {
				errChan <- fmt.Errorf("期望 page.Total=2，实际 %d", page.Total)
			}
		}()
	}
	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Fatal(err)
	}

	keys := client.Keys(db.Cxt, "*").Val()
	foundCache := false
	for _, key := range keys {
		if strings.HasPrefix(key, config.FilmSearchCachePrefix+":v"+version+":sv") {
			foundCache = true
			break
		}
	}
	if !foundCache {
		t.Fatalf("Redis 中应存在前缀 %s 的搜索缓存 key，实际 keys=%v", config.FilmSearchCachePrefix, keys)
	}
}

// FilmIndex 缺失时，孤儿快照仍应被删除且内存索引同步剔除。
func TestDelFilmSearch_CleansSnapshotWhenFilmIndexMissing(t *testing.T) {
	gdb := newTestDB(t)
	version := "v_it_del_orphan"
	targetMid := int64(777)
	seedSnapshots(t, gdb, version,
		model.FilmListSnapshot{Mid: targetMid, Pid: 1, Name: "孤儿快照影片"},
		model.FilmListSnapshot{Mid: 888, Pid: 1, Name: "其他影片"},
	)
	activateVersion(t, version)

	if err := filmrepo.DelFilmSearch(targetMid); err != nil {
		t.Fatalf("DelFilmSearch 失败: %v", err)
	}

	var count int64
	gdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ? AND mid = ?", version, targetMid).Count(&count)
	if count != 0 {
		t.Errorf("FilmIndex 缺失时孤儿快照仍应被删除，实际残留 count=%d", count)
	}

	rows, page := searchFilms(version, "孤儿快照影片", 10)
	assertMiss(t, "删除孤儿快照后", "孤儿快照影片", rows, page)

	rows, page = searchFilms(version, "其他影片", 10)
	assertHit(t, "删除孤儿快照后", "其他影片", rows, page, 888)
}

// 版本号推高后，检索只写入新版本缓存 key，旧版本 key 不得被污染。
func TestSearchCacheVersion_NoCrossVersionPollution(t *testing.T) {
	gdb := newTestDB(t)
	client := useRedis(t)

	version := "v_it_pollution"
	seedSnapshots(t, gdb, version, model.FilmListSnapshot{Mid: 101, Pid: 1, Name: "斗罗大陆 第一季"})
	activateVersion(t, version)

	v1 := filmsnapshot.GetSearchCacheVersion()
	filmsnapshot.BumpSearchCacheVersion()
	v2 := filmsnapshot.GetSearchCacheVersion()
	if v1 == v2 {
		t.Fatalf("推高前后版本号应不同，实际均为 %s", v1)
	}

	rows, page := searchFilms(version, "斗罗大陆", 10)
	assertHit(t, "版本推高后", "斗罗大陆", rows, page, 101)

	v2Key := fmt.Sprintf("%s:v%s:sv%s:斗罗大陆::p1:s10", config.FilmSearchCachePrefix, version, v2)
	v1Key := fmt.Sprintf("%s:v%s:sv%s:斗罗大陆::p1:s10", config.FilmSearchCachePrefix, version, v1)
	if client.Exists(db.Cxt, v2Key).Val() != 1 {
		t.Errorf("新版本缓存 key %s 应存在", v2Key)
	}
	if client.Exists(db.Cxt, v1Key).Val() != 0 {
		t.Errorf("旧版本缓存 key %s 不应被写入", v1Key)
	}
}

// 冷启动（内存索引为空）与版本不匹配两种场景下，增量更新都不得静默丢弃数据。
func TestUpsertMidsToActiveFilmSearchIndex_ColdStartAndReload(t *testing.T) {
	gdb := newTestDB(t)
	version := "v_it_upsert_cold"
	seedSnapshots(t, gdb, version,
		model.FilmListSnapshot{Mid: 101, Pid: 1, Name: "遮天 第一季"},
		model.FilmListSnapshot{Mid: 102, Pid: 1, Name: "遮天 第二季"},
	)
	activateVersion(t, version)

	// 1) 冷启动：内存索引为空
	filmsnapshot.ClearActiveFilmReadModel()
	filmsnapshot.UpsertMidsToActiveFilmSearchIndex(version, []int64{101})

	rows, page := searchFilms(version, "遮天", 10)
	assertHit(t, "冷启动增量更新", "遮天", rows, page, 101, 102)

	// 2) 版本不匹配：内存索引停留在旧版本，必须重载为新版本后再合并
	const staleVersion = "v_it_upsert_stale"
	seedSnapshots(t, gdb, staleVersion, model.FilmListSnapshot{Mid: 999, Pid: 1, Name: "旧版本影片"})
	activateVersion(t, staleVersion)

	filmsnapshot.UpsertMidsToActiveFilmSearchIndex(version, []int64{102})

	rows, page = searchFilms(version, "遮天", 10)
	assertHit(t, "版本切换后增量更新", "遮天", rows, page, 101, 102)

	rows, page = searchFilms(version, "旧版本影片", 10)
	assertMiss(t, "版本切换后增量更新", "旧版本影片", rows, page)
}

// 冷启动与版本不匹配场景下，增量剔除同样不得静默丢弃。
func TestRemoveMidsFromActiveFilmSearchIndex_ColdStartAndReload(t *testing.T) {
	gdb := newTestDB(t)
	version := "v_it_remove_cold"
	seedSnapshots(t, gdb, version,
		model.FilmListSnapshot{Mid: 201, Pid: 1, Name: "凡人修仙传 仙界篇"},
		model.FilmListSnapshot{Mid: 202, Pid: 1, Name: "凡人修仙传 重置版"},
	)
	activateVersion(t, version)

	// 1) 冷启动：内存索引为空
	filmsnapshot.ClearActiveFilmReadModel()
	filmsnapshot.RemoveMidsFromActiveFilmSearchIndex(version, []int64{201})

	rows, page := searchFilms(version, "凡人修仙传", 10)
	assertHit(t, "冷启动增量剔除", "凡人修仙传", rows, page, 202)

	// 2) 版本不匹配：内存索引停留在旧版本，重载后剔除 202
	const staleVersion = "v_it_remove_stale"
	seedSnapshots(t, gdb, staleVersion, model.FilmListSnapshot{Mid: 888, Pid: 1, Name: "旧版本数据"})
	activateVersion(t, staleVersion)

	filmsnapshot.RemoveMidsFromActiveFilmSearchIndex(version, []int64{202})

	rows, page = searchFilms(version, "凡人修仙传", 10)
	assertHit(t, "版本切换后增量剔除", "凡人修仙传", rows, page, 201)

	rows, page = searchFilms(version, "旧版本数据", 10)
	assertMiss(t, "版本切换后增量剔除", "旧版本数据", rows, page)
}

// 删除快照时同步清理分类首页等聚合缓存。
func TestDeleteActiveSnapshotsByMids_RefreshesAggregateCaches(t *testing.T) {
	gdb := newTestDB(t)
	client := useRedis(t)

	version := "v_it_del_aggregate_cache"
	targetMid := int64(301)
	seedSnapshots(t, gdb, version, model.FilmListSnapshot{Mid: targetMid, Pid: 1, Cid: 10, Name: "分类测试片"})
	activateVersion(t, version)

	classifyKey := fmt.Sprintf("%s:1:10:12", config.FilmClassifyCacheKey)
	indexKey := config.IndexPageCacheKey + ":home_page"
	for key, val := range map[string]string{classifyKey: "classify_data", indexKey: "index_data"} {
		if err := client.Set(db.Cxt, key, val, 0).Err(); err != nil {
			t.Fatalf("预置缓存 %s 失败: %v", key, err)
		}
	}

	filmsnapshot.DeleteActiveSnapshotsByMids(targetMid)

	for _, key := range []string{classifyKey, indexKey} {
		if client.Exists(db.Cxt, key).Val() != 0 {
			t.Errorf("聚合缓存 %s 应被清理", key)
		}
	}
}

// 并发冷启动获取搜索版本号必须得到一致结果。
func TestSearchCacheVersion_ConcurrentSetNXSafety(t *testing.T) {
	newTestDB(t)
	useRedis(t)

	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)
	versions := make([]string, concurrency)

	for i := 0; i < concurrency; i++ {
		idx := i
		go func() {
			defer wg.Done()
			versions[idx] = filmsnapshot.GetSearchCacheVersion()
		}()
	}
	wg.Wait()

	if versions[0] == "" {
		t.Fatal("搜索缓存版本号不应为空")
	}
	for i, ver := range versions {
		if ver != versions[0] {
			t.Fatalf("第 %d 个协程版本号 %q 与首值 %q 不一致", i, ver, versions[0])
		}
	}
}

// 按分类删除快照时必须推高搜索缓存版本号。
func TestDeleteActiveSnapshotsByCategory_BumpsSearchCacheVersion(t *testing.T) {
	gdb := newTestDB(t)
	useRedis(t)

	version := "v_it_cat_delete_bump"
	activateVersion(t, version)

	gdb.Create(&model.Category{Id: 50, Show: true})
	seedSnapshots(t, gdb, version, model.FilmListSnapshot{Mid: 401, Pid: 1, Cid: 50, Name: "分类影片401"})

	vBefore := filmsnapshot.GetSearchCacheVersion()
	filmsnapshot.DeleteActiveSnapshotsByCategory("cid", 50)
	vAfter := filmsnapshot.GetSearchCacheVersion()

	if vBefore == vAfter {
		t.Fatalf("按分类删除后搜索缓存版本号应推高，仍为 %s", vAfter)
	}
}
