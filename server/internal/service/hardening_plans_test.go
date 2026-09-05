package service

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	filmrepo "server/internal/repository/film"
)

func setupTestDBAndRedis(t *testing.T) (*gorm.DB, *miniredis.Miniredis) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared&_busy_timeout=10000", t.Name(), time.Now().UnixNano())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("migrate models: %v", err)
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("run miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	origMdb := db.Mdb
	origRdb := db.Rdb
	db.Mdb = gdb
	db.Rdb = client
	filmrepo.ClearActiveFilmReadModel()

	t.Cleanup(func() {
		filmrepo.WaitActiveFilmSearchIndexBuilt()
		_ = client.Close()
		mr.Close()
		db.Mdb = origMdb
		db.Rdb = origRdb
		filmrepo.ClearActiveFilmReadModel()
	})

	return gdb, mr
}

// 方案 1 测试：hotKeywords 性能加固
func TestPlan1_HotKeywords_Hardening(t *testing.T) {
	gdb, mr := setupTestDBAndRedis(t)
	const version = "v_plan1"

	snaps := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 1, Name: "复仇者联盟", Hits: 500, Pid: 1},
		{SnapshotVersion: version, Mid: 2, Name: "复仇者联盟", Hits: 400, Pid: 1}, // 重复片名
		{SnapshotVersion: version, Mid: 3, Name: "星际穿越", Hits: 300, Pid: 1},
		{SnapshotVersion: version, Mid: 4, Name: "流浪地球", Hits: 200, Pid: 1},
		{SnapshotVersion: version, Mid: 5, Name: "泰坦尼克号", Hits: 100, Pid: 1},
		{SnapshotVersion: version, Mid: 6, Name: "无效分类影片", Hits: 999, Pid: 0}, // pid=0 应被过滤
	}
	for _, s := range snaps {
		if err := gdb.Create(&s).Error; err != nil {
			t.Fatalf("create snapshot: %v", err)
		}
	}

	_ = filmrepo.SetActiveSnapshotVersion(version)
	_ = filmrepo.LoadActiveFilmReadModel(version)
	filmrepo.WaitActiveFilmSearchIndexBuilt()

	// 1. 请求 limit = 3
	kw := IndexSvc.GetHotSearchKeywords(3)
	if len(kw) != 3 {
		t.Fatalf("want 3 keywords, got %d: %v", len(kw), kw)
	}
	if kw[0] != "复仇者联盟" || kw[1] != "星际穿越" || kw[2] != "流浪地球" {
		t.Fatalf("unexpected keywords order: %v", kw)
	}

	// 2. 验证 Redis 缓存已写入
	cacheKey := fmt.Sprintf("EcoHub:hotKeywords:v%s", version)
	val, err := mr.Get(cacheKey)
	if err != nil || val == "" {
		t.Fatalf("expected redis key %s to exist, err: %v", cacheKey, err)
	}
	ttl := mr.TTL(cacheKey)
	if ttl <= 0 || ttl > 30*time.Minute {
		t.Fatalf("expected ttl around 30m, got %v", ttl)
	}

	// 3. 验证内存切片隔离性：修改返回切片不影响缓存与后续调用
	kw[0] = "篡改片名"
	kw2 := IndexSvc.GetHotSearchKeywords(3)
	if kw2[0] != "复仇者联盟" {
		t.Fatalf("cache was mutated by caller modification: got %s", kw2[0])
	}

	// 4. 并发请求防击穿验证
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(limit int) {
			defer wg.Done()
			res := IndexSvc.GetHotSearchKeywords(limit)
			if len(res) == 0 {
				t.Errorf("concurrent GetHotSearchKeywords returned empty")
			}
		}(i%3 + 1)
	}
	wg.Wait()
}

// 方案 2 测试：filmPlayInfo 完整缓存读写与防穿透/击穿/雪崩加固
func TestPlan2_FilmPlayInfo_Hardening(t *testing.T) {
	gdb, mr := setupTestDBAndRedis(t)
	const version = "v_plan2"

	// 1. 测试不存在影片的防穿透空值哨兵
	nonExistentMid := 99999
	res, err := IndexSvc.GetFilmDetail(nonExistentMid)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Id != 0 {
		t.Fatalf("expected empty detail for non-existent film, got %d", res.Id)
	}

	sentinelKey := fmt.Sprintf("EcoHub:filmPlayInfo:%d", nonExistentMid)
	sentinelVal, err := mr.Get(sentinelKey)
	if err != nil || sentinelVal != "{}" {
		t.Fatalf("expected sentinel '{}' in redis, got %q, err: %v", sentinelVal, err)
	}
	sentinelTTL := mr.TTL(sentinelKey)
	if sentinelTTL <= 0 || sentinelTTL > 60*time.Second {
		t.Fatalf("expected sentinel ttl <= 60s, got %v", sentinelTTL)
	}

	// 再次请求该不存在的影片，确认命中哨兵快速返回
	res2, _ := IndexSvc.GetFilmDetail(nonExistentMid)
	if res2.Id != 0 {
		t.Fatalf("expected empty detail from sentinel")
	}

	// 2. 插入正常影片与详情
	const validMid = 101
	snap := model.FilmListSnapshot{
		SnapshotVersion: version,
		Mid:             validMid,
		Name:            "盗梦空间",
		Pid:             1,
		Cid:             10,
	}
	if err := gdb.Create(&snap).Error; err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	detail := model.MovieDetail{
		Id:       validMid,
		Name:     "盗梦空间",
		PlayList: [][]model.MovieUrlInfo{{{Episode: "正片", Link: "https://test.com/play.m3u8"}}},
		PlayFrom: []string{"默认主源"},
	}
	rawDetail, _ := json.Marshal(detail)
	if err := gdb.Create(&model.MovieDetailInfo{Mid: validMid, Content: string(rawDetail)}).Error; err != nil {
		t.Fatalf("create detail: %v", err)
	}

	_ = filmrepo.SetActiveSnapshotVersion(version)
	_ = filmrepo.LoadActiveFilmReadModel(version)
	filmrepo.WaitActiveFilmSearchIndexBuilt()

	// 3. 读取正常详情并验证缓存写入与 Jitter 防雪崩
	detailVo, err := IndexSvc.GetFilmDetail(validMid)
	if err != nil || detailVo.Id != validMid {
		t.Fatalf("failed to get film detail: %v, detail: %+v", err, detailVo)
	}

	validKey := fmt.Sprintf("EcoHub:filmPlayInfo:%d", validMid)
	cachedVal, err := mr.Get(validKey)
	if err != nil || cachedVal == "" {
		t.Fatalf("expected redis key %s to exist", validKey)
	}
	ttl := mr.TTL(validKey)
	// 期望 TTL 介于 12h 与 12h30m 之间 (12h + 0~1800s 随机 Jitter)
	if ttl < 12*time.Hour || ttl > 12*time.Hour+1800*time.Second {
		t.Fatalf("expected ttl between 12h and 12.5h, got %v", ttl)
	}

	// 4. 并发安全验证与切片隔离验证（模拟 FilmPlayInfo handler 并发就地修改 LinkList）
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			v, err := IndexSvc.GetFilmDetail(validMid)
			if err != nil || v.Id != validMid {
				t.Errorf("concurrent GetFilmDetail failed: %v", err)
				return
			}
			// 模拟 handler 在本地修改 LinkList，若未做深拷贝则会触发竞态检测报警
			for j := range v.List {
				var validLinks []model.MovieUrlInfo
				for _, ep := range v.List[j].LinkList {
					if ep.Link != "" {
						validLinks = append(validLinks, ep)
					}
				}
				v.List[j].LinkList = append(validLinks, model.MovieUrlInfo{Episode: fmt.Sprintf("W%d", workerIdx), Link: "test"})
			}
		}(i)
	}
	wg.Wait()
}

// 方案 3 测试：provide/vod TVBox 详情批量化与 Pipeline MGet
func TestPlan3_ProvideVodDetail_BatchAndPipeline(t *testing.T) {
	gdb, mr := setupTestDBAndRedis(t)
	const version = "v_plan3"

	// 创建 3 部影片：201、202、203
	mids := []int64{201, 202, 203}
	for _, mid := range mids {
		snap := model.FilmListSnapshot{
			SnapshotVersion: version,
			Mid:             mid,
			Name:            fmt.Sprintf("电影%d", mid),
			Pid:             1,
			Cid:             10,
			Hits:            mid * 10,
		}
		if err := gdb.Create(&snap).Error; err != nil {
			t.Fatalf("create snapshot: %v", err)
		}

		d := model.MovieDetail{
			Id:       mid,
			Name:     fmt.Sprintf("电影%d", mid),
			PlayList: [][]model.MovieUrlInfo{{{Episode: "HD", Link: fmt.Sprintf("http://video/%d.m3u8", mid)}}},
			PlayFrom: []string{"默认主源"},
		}
		raw, _ := json.Marshal(d)
		if err := gdb.Create(&model.MovieDetailInfo{Mid: mid, Content: string(raw)}).Error; err != nil {
			t.Fatalf("create detail: %v", err)
		}
	}

	_ = filmrepo.SetActiveSnapshotVersion(version)
	_ = filmrepo.LoadActiveFilmReadModel(version)
	filmrepo.WaitActiveFilmSearchIndexBuilt()

	// 预先将 201 写入 Redis 模拟缓存命中
	vo201 := model.MovieDetailVo{
		MovieDetail: model.MovieDetail{Id: 201, Name: "电影201"},
		List: []model.PlayLinkVo{
			{Name: "默认主源", LinkList: []model.MovieUrlInfo{{Episode: "HD", Link: "http://video/201.m3u8"}}},
		},
	}
	raw201, _ := json.Marshal(vo201)
	_ = mr.Set(fmt.Sprintf("EcoHub:filmPlayInfo:%d", 201), string(raw201))

	// 将 9999 写入哨兵 "{}"
	_ = mr.Set(fmt.Sprintf("EcoHub:filmPlayInfo:%d", 9999), "{}")

	// 请求批量详情：包含命中的 201、未命中的 202 和 203、哨兵 9999、完全不存在的 8888
	ids := []string{"201", "202", "203", "9999", "8888"}
	details := ProvideSvc.GetVodDetail(ids)

	if len(details) != 3 {
		t.Fatalf("want 3 valid film details, got %d", len(details))
	}
	if details[0].VodID != 201 || details[1].VodID != 202 || details[2].VodID != 203 {
		t.Fatalf("unexpected vod detail ids: %+v", details)
	}

	// 验证未命中的 202 与 203 是否在 GetVodDetail 后被补齐写入了 Redis
	for _, mid := range []int64{202, 203} {
		val, err := mr.Get(fmt.Sprintf("EcoHub:filmPlayInfo:%d", mid))
		if err != nil || val == "" || val == "{}" {
			t.Fatalf("expected film %d to be cached after call, got %q, err: %v", mid, val, err)
		}
	}
}

// 方案 4 测试：filmClassify 剔除 COUNT 并实现 3 路并行
func TestPlan4_FilmClassify_FastSortAndParallel(t *testing.T) {
	gdb, mr := setupTestDBAndRedis(t)
	const version = "v_plan4"

	const pid = int64(1)
	// 插入几条数据，分别赋予不同 hits, year, update_stamp
	items := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 301, Pid: pid, Name: "影片A", Hits: 100, Year: 2021, UpdateStamp: 1000},
		{SnapshotVersion: version, Mid: 302, Pid: pid, Name: "影片B", Hits: 900, Year: 2020, UpdateStamp: 2000},
		{SnapshotVersion: version, Mid: 303, Pid: pid, Name: "影片C", Hits: 500, Year: 2023, UpdateStamp: 500},
	}
	for _, it := range items {
		if err := gdb.Create(&it).Error; err != nil {
			t.Fatalf("create item: %v", err)
		}
	}

	_ = filmrepo.SetActiveSnapshotVersion(version)
	_ = filmrepo.LoadActiveFilmReadModel(version)
	filmrepo.WaitActiveFilmSearchIndexBuilt()

	// 测试 GetSnapshotTopMoviesBySortFast:
	// sortType 0 (news: year DESC, update_stamp DESC) -> 应最先是 303 (2023)
	news := filmrepo.GetSnapshotTopMoviesBySortFast(version, 0, pid, 10)
	if len(news) != 3 || news[0].Id != 303 {
		t.Fatalf("sortType 0 unexpected top: %+v", news)
	}

	// sortType 1 (top: hits DESC) -> 应最先是 302 (hits 900)
	top := filmrepo.GetSnapshotTopMoviesBySortFast(version, 1, pid, 10)
	if len(top) != 3 || top[0].Id != 302 {
		t.Fatalf("sortType 1 unexpected top: %+v", top)
	}

	// sortType 2 (recent: update_stamp DESC) -> 应最先是 302 (update_stamp 2000)
	recent := filmrepo.GetSnapshotTopMoviesBySortFast(version, 2, pid, 10)
	if len(recent) != 3 || recent[0].Id != 302 {
		t.Fatalf("sortType 2 unexpected top: %+v", recent)
	}

	// 测试 IndexSvc.GetFilmClassify 并发组装
	page := &dto.Page{PageSize: 10}
	res := IndexSvc.GetFilmClassify(pid, page)
	if res == nil {
		t.Fatal("expected non-nil classify response")
	}
	if _, ok := res["news"]; !ok {
		t.Fatal("missing 'news' in classify response")
	}
	if _, ok := res["top"]; !ok {
		t.Fatal("missing 'top' in classify response")
	}
	if _, ok := res["recent"]; !ok {
		t.Fatal("missing 'recent' in classify response")
	}

	// 确认顶层分类缓存已生成
	cacheKey := filmrepo.SnapshotClassifyCacheKey(version, pid, page)
	if val, err := mr.Get(cacheKey); err != nil || val == "" {
		t.Fatalf("expected classify cache key %s to be set", cacheKey)
	}
}

// 方案 5 测试：filmClassifySearch 深分页截断与 SingleFlight 并发安全返回
func TestPlan5_FilmClassifySearch_LimitsAndSingleFlight(t *testing.T) {
	gdb, _ := setupTestDBAndRedis(t)
	const version = "v_plan5"

	// 1. 验证 normalizeIndexPage 边界截断
	p1 := &dto.Page{Current: 100, PageSize: 100}
	p1 = normalizeIndexPage(p1)
	if p1.Current != 50 {
		t.Fatalf("expected Current capped at 50, got %d", p1.Current)
	}
	if p1.PageSize != 48 {
		t.Fatalf("expected PageSize capped at 48, got %d", p1.PageSize)
	}

	p2 := &dto.Page{Current: -5, PageSize: 0}
	p2 = normalizeIndexPage(p2)
	if p2.Current != 1 || p2.PageSize != 20 {
		t.Fatalf("expected default 1/20, got %d/%d", p2.Current, p2.PageSize)
	}

	// 2. 插入测试数据
	for i := int64(1); i <= 15; i++ {
		s := model.FilmListSnapshot{
			SnapshotVersion: version,
			Mid:             400 + i,
			Pid:             1,
			Cid:             10,
			Name:            fmt.Sprintf("测试片%d", i),
			Hits:            i * 10,
		}
		if err := gdb.Create(&s).Error; err != nil {
			t.Fatalf("create test film: %v", err)
		}
	}

	_ = filmrepo.SetActiveSnapshotVersion(version)
	_ = filmrepo.LoadActiveFilmReadModel(version)
	filmrepo.WaitActiveFilmSearchIndexBuilt()

	st := model.SearchTagsVO{Pid: 1, Cid: 10}

	// 3. 并发调用 ListFilmSnapshotsByTagsReadModel，测试 SingleFlight 返回后每个调用方 page 的 Total/PageCount 均被正确同步
	const concurrency = 10
	var wg sync.WaitGroup
	pages := make([]*dto.Page, concurrency)
	results := make([][]model.FilmListSnapshot, concurrency)

	for i := 0; i < concurrency; i++ {
		pages[i] = &dto.Page{Current: 1, PageSize: 5}
	}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = filmrepo.ListFilmSnapshotsByTagsReadModel(version, st, pages[idx])
		}(i)
	}
	wg.Wait()

	for i := 0; i < concurrency; i++ {
		if len(results[i]) != 5 {
			t.Errorf("worker %d: want 5 snapshots, got %d", i, len(results[i]))
		}
		if pages[i].Total != 15 {
			t.Errorf("worker %d: want Total=15, got %d", i, pages[i].Total)
		}
		if pages[i].PageCount != 3 {
			t.Errorf("worker %d: want PageCount=3, got %d", i, pages[i].PageCount)
		}
	}
}

// 方案 6 测试：filmRelate 缓存前置与空值哨兵
func TestPlan6_FilmRelate_FrontCacheAndSentinel(t *testing.T) {
	gdb, mr := setupTestDBAndRedis(t)
	const version = "v_plan6"

	// 1. 插入存在影片与相关候选集
	const validMid = int64(501)
	const relMid = int64(502)
	s1 := model.FilmListSnapshot{
		SnapshotVersion: version,
		Mid:             validMid,
		Name:            "流浪地球1",
		Pid:             1,
		Cid:             10,
	}
	s2 := model.FilmListSnapshot{
		SnapshotVersion: version,
		Mid:             relMid,
		Name:            "流浪地球2",
		Pid:             1,
		Cid:             10,
	}
	if err := gdb.Create(&s1).Error; err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if err := gdb.Create(&s2).Error; err != nil {
		t.Fatalf("create s2: %v", err)
	}
	if err := gdb.Create(&model.MovieDetailInfo{Mid: validMid, Content: `{"id":501,"name":"流浪地球1"}`}).Error; err != nil {
		t.Fatalf("create d1: %v", err)
	}
	if err := gdb.Create(&model.MovieDetailInfo{Mid: relMid, Content: `{"id":502,"name":"流浪地球2"}`}).Error; err != nil {
		t.Fatalf("create d2: %v", err)
	}

	_ = filmrepo.SetActiveSnapshotVersion(version)
	_ = filmrepo.LoadActiveFilmReadModel(version)
	filmrepo.WaitActiveFilmSearchIndexBuilt()

	page := &dto.Page{Current: 1, PageSize: 10}

	// 2. 不存在影片的推荐请求 -> 返回空并写入 "[]" 哨兵（60s）
	const nonExistentMid = int64(77777)
	resEmpty := IndexSvc.RelateMovie(nonExistentMid, page)
	if len(resEmpty) != 0 {
		t.Fatalf("expected empty relate for non-existent film, got %d", len(resEmpty))
	}

	sentinelKey := fmt.Sprintf("EcoHub:relate:vo:v%s:%d:p%d:s%d", version, nonExistentMid, 1, 10)
	val, err := mr.Get(sentinelKey)
	if err != nil || val != "[]" {
		t.Fatalf("expected '[]' sentinel in redis, got %q, err: %v", val, err)
	}
	ttl := mr.TTL(sentinelKey)
	if ttl <= 0 || ttl > 60*time.Second {
		t.Fatalf("expected sentinel ttl <= 60s, got %v", ttl)
	}

	// 再次请求该不存在的影片，验证直接命中 "[]" 哨兵
	resEmpty2 := IndexSvc.RelateMovie(nonExistentMid, page)
	if len(resEmpty2) != 0 {
		t.Fatalf("expected empty relate from sentinel")
	}

	// 3. 请求相关推荐
	relList := IndexSvc.RelateMovie(validMid, page)
	relCacheKey := fmt.Sprintf("EcoHub:relate:vo:v%s:%d:p%d:s%d", version, validMid, 1, 10)
	cachedVal, err := mr.Get(relCacheKey)
	if err != nil || cachedVal == "" {
		t.Fatalf("expected relate cache key %s to exist", relCacheKey)
	}
	relTTL := mr.TTL(relCacheKey)
	if len(relList) > 0 {
		// 正常结果缓存 1 小时
		if relTTL <= 50*time.Minute || relTTL > time.Hour {
			t.Fatalf("expected normal relate cache ttl around 1h, got %v", relTTL)
		}
	} else {
		// 无推荐缓存 60 秒
		if relTTL <= 0 || relTTL > 60*time.Second {
			t.Fatalf("expected empty relate ttl <= 60s, got %v", relTTL)
		}
	}
}

// 缓存清理测试：验证 ClearAllSnapshotDynamicCaches 清理 EcoHub:filmPlayInfo:* 与 EcoHub:hotKeywords:*
func TestPlan2_ClearAllSnapshotDynamicCaches_Invalidation(t *testing.T) {
	_, mr := setupTestDBAndRedis(t)

	key1 := "EcoHub:filmPlayInfo:123"
	key2 := "EcoHub:hotKeywords:v999"
	key3 := "EcoHub:relate:vo:v999:123:p1:s10"

	_ = mr.Set(key1, `{"id":123}`)
	_ = mr.Set(key2, `["片名"]`)
	_ = mr.Set(key3, `[]`)

	filmrepo.ClearAllSnapshotDynamicCaches()

	for _, k := range []string{key1, key2, key3} {
		if mr.Exists(k) {
			t.Fatalf("expected key %s to be cleared by ClearAllSnapshotDynamicCaches", k)
		}
	}
}

// 方案 1 边缘测试：验证 ensureSnapshotPerformanceIndexes 执行正常且幂等
func TestPlan1_EnsureSnapshotPerformanceIndexes(t *testing.T) {
	_, _ = setupTestDBAndRedis(t)

	// 首次调用执行 DDL
	ensureSnapshotPerformanceIndexes()
	// 二次调用验证幂等性
	ensureSnapshotPerformanceIndexes()
}

// 方案 1 边缘测试：无数据时写入 60s 空值缓存防穿透
func TestPlan1_EmptyHotKeywords_Sentinel(t *testing.T) {
	_, mr := setupTestDBAndRedis(t)
	const version = "v_plan1_empty"
	_ = filmrepo.SetActiveSnapshotVersion(version)

	kw := IndexSvc.GetHotSearchKeywords(10)
	if len(kw) != 0 {
		t.Fatalf("expected 0 keywords, got %d", len(kw))
	}

	cacheKey := fmt.Sprintf("EcoHub:hotKeywords:v%s", version)
	val, err := mr.Get(cacheKey)
	if err != nil || val != "[]" {
		t.Fatalf("expected '[]' in redis, got %q, err: %v", val, err)
	}
	ttl := mr.TTL(cacheKey)
	if ttl <= 0 || ttl > 60*time.Second {
		t.Fatalf("expected ttl <= 60s, got %v", ttl)
	}
}

// 方案 3 边缘测试：批量截断（超过 100 个截断为 100）及 Redis 宕机（nil）优雅降级
func TestPlan3_BatchClampingAndNilRedis(t *testing.T) {
	gdb, _ := setupTestDBAndRedis(t)
	const version = "v_plan3_batch"

	// 插入影片 601
	const mid = int64(601)
	s := model.FilmListSnapshot{
		SnapshotVersion: version,
		Mid:             mid,
		Name:            "降级测试影片",
		Pid:             1,
		Cid:             10,
	}
	if err := gdb.Create(&s).Error; err != nil {
		t.Fatalf("create snap: %v", err)
	}
	d := model.MovieDetail{Id: mid, Name: "降级测试影片", PlayList: [][]model.MovieUrlInfo{{{Episode: "1", Link: "url"}}}}
	raw, _ := json.Marshal(d)
	if err := gdb.Create(&model.MovieDetailInfo{Mid: mid, Content: string(raw)}).Error; err != nil {
		t.Fatalf("create detail: %v", err)
	}

	_ = filmrepo.SetActiveSnapshotVersion(version)

	// 1. 模拟超过 100 个 ID 请求，测试 batch clamping
	manyIDs := make([]string, 150)
	for i := 0; i < 150; i++ {
		manyIDs[i] = fmt.Sprintf("%d", 601+i)
	}
	res := ProvideSvc.GetVodDetail(manyIDs)
	if len(res) != 1 || res[0].VodID != 601 {
		t.Fatalf("expected exactly 1 found detail, got %d", len(res))
	}

	// 2. 模拟 Redis 宕机 (db.Rdb = nil) 优雅降级直接走 DB
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() { db.Rdb = origRdb }()

	resNilRedis := ProvideSvc.GetVodDetail([]string{"601"})
	if len(resNilRedis) != 1 || resNilRedis[0].VodID != 601 {
		t.Fatalf("expected 1 detail on nil redis, got %d", len(resNilRedis))
	}
}

// 方案 4 边缘测试：read model version 为空时自动回退 active snapshot version
func TestPlan4_EmptyReadModelFallback(t *testing.T) {
	gdb, _ := setupTestDBAndRedis(t)
	const version = "v_plan4_fallback"
	_ = filmrepo.SetActiveSnapshotVersion(version)

	s := model.FilmListSnapshot{
		SnapshotVersion: version,
		Mid:             701,
		Pid:             1,
		Name:            "回退影片",
		Hits:            888,
		Year:            2025,
		UpdateStamp:     100,
	}
	if err := gdb.Create(&s).Error; err != nil {
		t.Fatalf("create s: %v", err)
	}

	res := IndexSvc.GetFilmClassify(1, &dto.Page{PageSize: 10})
	if res == nil {
		t.Fatal("expected non-nil classify")
	}
	news, ok := res["news"].([]model.MovieBasicInfo)
	if !ok || len(news) != 1 || news[0].Id != 701 {
		t.Fatalf("expected 1 news item from fallback version, got: %+v", news)
	}
}

// 方案 5 边缘测试：无匹配筛选标签写入 60s 空值缓存防穿透
func TestPlan5_EmptyTagsSearch_Sentinel(t *testing.T) {
	_, mr := setupTestDBAndRedis(t)
	const version = "v_plan5_empty"
	_ = filmrepo.SetActiveSnapshotVersion(version)

	st := model.SearchTagsVO{Pid: 999, Cid: 888}
	page := &dto.Page{Current: 1, PageSize: 10}
	snaps := filmrepo.ListFilmSnapshotsByTagsReadModel(version, st, page)
	if len(snaps) != 0 {
		t.Fatalf("expected 0 snapshots, got %d", len(snaps))
	}

	cacheKey := fmt.Sprintf("EcoHub:tags_search:v%s:%d:%d:%s:%s:%s:%s:%s:p%d:s%d",
		version, st.Pid, st.Cid, "", "", "", "", "", 1, 10)
	val, err := mr.Get(cacheKey)
	if err != nil || val == "" {
		t.Fatalf("expected empty search cache key %s to exist", cacheKey)
	}
	ttl := mr.TTL(cacheKey)
	if ttl <= 0 || ttl > 60*time.Second {
		t.Fatalf("expected ttl <= 60s, got %v", ttl)
	}
}

// 方案 6 边缘测试：相关推荐切片返回隔离性验证
func TestPlan6_RelateMovie_SliceIsolation(t *testing.T) {
	gdb, _ := setupTestDBAndRedis(t)
	const version = "v_plan6_iso"

	s1 := model.FilmListSnapshot{SnapshotVersion: version, Mid: 801, Name: "电影A", Pid: 1, Cid: 10}
	s2 := model.FilmListSnapshot{SnapshotVersion: version, Mid: 802, Name: "电影B", Pid: 1, Cid: 10}
	_ = gdb.Create(&s1)
	_ = gdb.Create(&s2)
	_ = gdb.Create(&model.MovieDetailInfo{Mid: 801, Content: `{"id":801,"name":"电影A"}`})
	_ = gdb.Create(&model.MovieDetailInfo{Mid: 802, Content: `{"id":802,"name":"电影B"}`})

	_ = filmrepo.SetActiveSnapshotVersion(version)
	_ = filmrepo.LoadActiveFilmReadModel(version)
	filmrepo.WaitActiveFilmSearchIndexBuilt()

	page := &dto.Page{Current: 1, PageSize: 10}
	list1 := IndexSvc.RelateMovie(801, page)
	if len(list1) > 0 {
		origName := list1[0].Name
		list1[0].Name = "篡改推荐片名"
		list2 := IndexSvc.RelateMovie(801, page)
		if list2[0].Name != origName {
			t.Fatalf("relateMovie slice isolation failed: mutated to %s", list2[0].Name)
		}
	}
}
