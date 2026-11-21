package film

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupResetStatsDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.Category{},
		&model.CategoryMapping{},
		&model.CollectRecord{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	db.Rdb = nil
	return gdb
}

func TestGetInventoryStats_EnabledLibraryAndRealSourceCounts(t *testing.T) {
	gdb := setupResetStatsDB(t)

	gdb.Create(&model.FilmSource{Id: "src_a", Name: "主站A", Uri: "http://a.test", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "停用站B", Uri: "http://b.test", State: false, Sort: 1})
	gdb.Create(&model.FilmSource{Id: "src_c", Name: "启用空站C", Uri: "http://c.test", State: true, Sort: 2})

	for _, row := range []model.FilmIndex{
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src_a"}, FilmIndexContent: model.FilmIndexContent{Name: "片1"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src_b"}, FilmIndexContent: model.FilmIndexContent{Name: "停用站片"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "src_a"}, FilmIndexContent: model.FilmIndexContent{Name: "片3"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 4, FirstSourceId: "src_a"}, FilmIndexContent: model.FilmIndexContent{Name: "片4"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 5, FirstSourceId: "src_a"}, FilmIndexContent: model.FilmIndexContent{Name: "已删片"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 6, FirstSourceId: "src_a"}, FilmIndexContent: model.FilmIndexContent{Name: "片6"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 7, FirstSourceId: "src_gone"}, FilmIndexContent: model.FilmIndexContent{Name: "孤儿站片"}},
	} {
		gdb.Create(&row)
	}
	gdb.Delete(&model.FilmIndex{}, "mid = ?", int64(5))

	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", StableKey: "movie", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 2, Pid: 0, Name: "隐藏", StableKey: "hidden", Show: false, Sort: 2})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 2, CategoryId: 2})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 9, CategoryId: 999})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 1, CategoryId: 1})

	now := time.Now()
	gdb.Create(&model.CollectRecord{
		BatchID:    "yesterday",
		Trigger:    model.CollectTriggerCron,
		Status:     model.CollectRecordStatusPartial,
		StartedAt:  now.AddDate(0, 0, -2),
		PageFailed: 100,
		DetailJson: `[{"sourceId":"src_a","status":"partial","failedCnt":100,"failedPages":[1]}]`,
	})
	gdb.Create(&model.CollectRecord{
		BatchID:    "today",
		Trigger:    model.CollectTriggerCron,
		Status:     model.CollectRecordStatusPartial,
		StartedAt:  now,
		PageFailed: 6,
		DetailJson: `[{"sourceId":"src_a","status":"partial","failedCnt":4,"failedPages":[11,12,13,14]},` +
			`{"sourceId":"src_b","status":"partial","failedCnt":2}]`,
	})

	filmsnapshot.ClearActiveFilmReadModel()
	stats, err := GetInventoryStats(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Status != InventoryStatsStatusReady {
		t.Fatalf("status = %q, want %q", stats.Status, InventoryStatsStatusReady)
	}
	// 顶部只汇总已启用站（src_a + src_c），不计入已停用的 src_b 与不存在的 src_gone
	if stats.Library.Films != 4 || stats.Library.Categories != 2 || stats.Library.Failures != 4 {
		t.Fatalf("library = %+v, want {Films:4 Categories:2 Failures:4}", stats.Library)
	}
	if len(stats.Sources) != 3 {
		t.Fatalf("sources = %+v", stats)
	}
	if s := stats.Sources[0]; s.Id != "src_a" || !s.IsPrimary || !s.Enabled || s.Films != 4 || s.Categories != 2 || s.Failures != 4 {
		t.Fatalf("src_a = %+v", s)
	}
	if s := stats.Sources[1]; s.Id != "src_b" || s.IsPrimary || s.Enabled || s.Films != 1 || s.Categories != 1 || s.Failures != 2 {
		t.Fatalf("src_b = %+v", s)
	}
	if s := stats.Sources[2]; s.Id != "src_c" || s.IsPrimary || !s.Enabled || s.Films != 0 || s.Categories != 0 || s.Failures != 0 {
		t.Fatalf("src_c = %+v", s)
	}
}

func TestGetInventoryStats_AllDisabledZeroLibraryKeepsSources(t *testing.T) {
	gdb := setupResetStatsDB(t)

	gdb.Create(&model.FilmSource{Id: "src_off", Name: "停用站", Uri: "http://off.test", State: false, Sort: 0})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 11, FirstSourceId: "src_off"},
		FilmIndexContent:  model.FilmIndexContent{Name: "停用库影片"},
	})
	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", StableKey: "movie", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_off", SourceTypeId: 1, CategoryId: 1})

	filmsnapshot.ClearActiveFilmReadModel()
	stats, err := GetInventoryStats(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Status != InventoryStatsStatusReady {
		t.Fatalf("status = %q, want %q", stats.Status, InventoryStatsStatusReady)
	}
	if stats.Library.Films != 0 || stats.Library.Categories != 0 || stats.Library.Failures != 0 {
		t.Fatalf("expected zero library when no source is enabled, got %+v", stats.Library)
	}
	if len(stats.Sources) != 1 || stats.Sources[0].Films != 1 || stats.Sources[0].Categories != 1 {
		t.Fatalf("expected disabled source real counts preserved, got %+v", stats.Sources)
	}
}

func TestGetInventoryStats_CacheSingleflightAndContextCancel(t *testing.T) {
	gdb := setupResetStatsDB(t)
	gdb.Create(&model.FilmSource{Id: "src_a", Name: "主站A", Uri: "http://a.test", State: true, Sort: 0})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src_a"},
		FilmIndexContent:  model.FilmIndexContent{Name: "片1"},
	})

	// 1. 已取消的 ctx 应立即返回 context.Canceled，且不污染缓存
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GetInventoryStats(canceledCtx, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// 2. 并发首次请求应全部拿到一致结果
	const workers = 6
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	counts := make(chan int64, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stats, err := GetInventoryStats(context.Background(), false)
			if err != nil {
				errs <- err
				return
			}
			counts <- stats.Library.Films
		}()
	}
	wg.Wait()
	close(errs)
	close(counts)
	for err := range errs {
		t.Fatalf("concurrent GetInventoryStats failed: %v", err)
	}
	for c := range counts {
		if c != 1 {
			t.Fatalf("expected concurrent Films=1, got %d", c)
		}
	}

	// 3. 已出结果后，未触发采集/变更事件且不传 refresh 时应直接读内存结果（不重查库，仍返回 1）
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src_a"},
		FilmIndexContent:  model.FilmIndexContent{Name: "片2"},
	})
	cachedStats, err := GetInventoryStats(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if cachedStats.Library.Films != 1 {
		t.Fatalf("expected steady memory Films=1 without event/refresh, got %d", cachedStats.Library.Films)
	}

	// 4. 模拟采集完成/数据变更触发异步重算后，拿到最新准确结果（返回 2）
	RefreshInventoryStatsAsync()
	var afterEvent InventoryStats
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		afterEvent, err = GetInventoryStats(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		if afterEvent.Status == InventoryStatsStatusReady && afterEvent.Library.Films == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if afterEvent.Status != InventoryStatsStatusReady || afterEvent.Library.Films != 2 {
		t.Fatalf("expected Films=2 after RefreshInventoryStatsAsync, got %+v", afterEvent)
	}

	// 5. 手动传 refresh=true 立即重算（返回 3）
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "src_a"},
		FilmIndexContent:  model.FilmIndexContent{Name: "片3"},
	})
	refreshedStats, err := GetInventoryStats(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if refreshedStats.Library.Films != 3 {
		t.Fatalf("expected refreshed Films=3 when refresh=true, got %d", refreshedStats.Library.Films)
	}
}

func TestGetInventoryStats_SlowQueryReturnsComputingThenReady(t *testing.T) {
	gdb := setupResetStatsDB(t)
	origWait := inventoryStatsFastWait
	origFetch := fetchInventoriesFn
	t.Cleanup(func() {
		inventoryStatsFastWait = origWait
		fetchInventoriesFn = origFetch
		inventoryStatsMu.Lock()
		inventoryStatsData = inventoryStatsSnapshot{}
		inventoryStatsInflight = nil
		inventoryStatsPending = nil
		inventoryStatsDirty = false
		inventoryStatsMu.Unlock()
	})

	inventoryStatsFastWait = 20 * time.Millisecond
	release := make(chan struct{})
	var fetchCalls int
	var mu sync.Mutex
	fetchInventoriesFn = func(_ context.Context, _ *gorm.DB) ([]SourceInventory, error) {
		mu.Lock()
		fetchCalls++
		n := fetchCalls
		mu.Unlock()
		if n == 1 {
			<-release
		}
		return []SourceInventory{{Id: "src_a", Name: "主站A", Enabled: true, Films: int64(40 + n), Categories: 5, Failures: 1}}, nil
	}

	// 1. 模拟采集完成后异步启动统计，尚未算完时前端请求返回 computing
	RefreshInventoryStatsAsync()
	first, err := GetInventoryStats(context.Background(), false)
	if err != nil {
		t.Fatalf("expected computing without error, got %v", err)
	}
	if first.Status != InventoryStatsStatusComputing {
		t.Fatalf("status = %q, want %q", first.Status, InventoryStatsStatusComputing)
	}

	// 2. 计算中再次轮询不重复启动，继续返回 computing；期间若又发生采集完成则登记补算
	second, err := GetInventoryStats(context.Background(), false)
	if err != nil {
		t.Fatalf("poll computing failed: %v", err)
	}
	if second.Status != InventoryStatsStatusComputing {
		t.Fatalf("second status = %q, want %q", second.Status, InventoryStatsStatusComputing)
	}
	RefreshInventoryStatsAsync()

	// 3. 本轮及补算轮完成后，应返回最新第 2 轮准确 ready 数据，且之后再次请求不重扫数据库
	close(release)
	var ready InventoryStats
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		ready, err = GetInventoryStats(context.Background(), false)
		if err != nil {
			t.Fatalf("ready poll failed: %v", err)
		}
		if ready.Status == InventoryStatsStatusReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ready.Status != InventoryStatsStatusReady || ready.Library.Films != 42 || len(ready.Sources) != 1 || ready.Sources[0].Films != 42 {
		t.Fatalf("unexpected ready stats: %+v", ready)
	}
	if _, err := GetInventoryStats(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if fetchCalls != 2 {
		mu.Unlock()
		t.Fatalf("expected exactly 2 fetch calls (initial + queued pending), got %d", fetchCalls)
	}
	mu.Unlock()
	_ = gdb
}

func TestGetInventoryStats_BackgroundErrorCooldownAndRefreshReset(t *testing.T) {
	setupResetStatsDB(t)
	origWait := inventoryStatsFastWait
	origCooldown := inventoryStatsErrCooldown
	origFetch := fetchInventoriesFn
	t.Cleanup(func() {
		inventoryStatsFastWait = origWait
		inventoryStatsErrCooldown = origCooldown
		fetchInventoriesFn = origFetch
		inventoryStatsMu.Lock()
		inventoryStatsData = inventoryStatsSnapshot{}
		inventoryStatsInflight = nil
		inventoryStatsPending = nil
		inventoryStatsDirty = false
		inventoryStatsMu.Unlock()
	})

	inventoryStatsFastWait = 20 * time.Millisecond
	inventoryStatsErrCooldown = 60 * time.Millisecond
	release := make(chan struct{})
	failNext := true
	var calls int
	var mu sync.Mutex
	fetchInventoriesFn = func(_ context.Context, _ *gorm.DB) ([]SourceInventory, error) {
		mu.Lock()
		calls++
		shouldFail := failNext
		mu.Unlock()
		<-release
		if shouldFail {
			return nil, errors.New("模拟数据库异常")
		}
		return []SourceInventory{{Id: "src_a", Name: "主站A", Enabled: true, Films: 9}}, nil
	}

	first, err := GetInventoryStats(context.Background(), false)
	if err != nil || first.Status != InventoryStatsStatusComputing {
		t.Fatalf("expected computing, got stats=%+v err=%v", first, err)
	}
	close(release)

	// 等待后台失败落盘后，冷却期内普通请求拿到真实错误且不无限重查
	var pollErr error
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		_, pollErr = GetInventoryStats(context.Background(), false)
		if pollErr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pollErr == nil || pollErr.Error() != "模拟数据库异常" {
		t.Fatalf("expected background error, got %v", pollErr)
	}
	if _, err := GetInventoryStats(context.Background(), false); err == nil {
		t.Fatal("expected preserved error without re-fetching inside cooldown")
	}
	mu.Lock()
	if calls != 1 {
		mu.Unlock()
		t.Fatalf("expected calls=1 inside cooldown, got %d", calls)
	}
	failNext = false
	mu.Unlock()

	// 超过冷却期后，普通请求（refresh=false）也应自动重试恢复，不再永久停在报错态
	time.Sleep(inventoryStatsErrCooldown + 15*time.Millisecond)
	autoRecovered, err := GetInventoryStats(context.Background(), false)
	if err != nil {
		t.Fatalf("expected auto recovery after error cooldown, got %v", err)
	}
	if autoRecovered.Status != InventoryStatsStatusReady || autoRecovered.Library.Films != 9 {
		t.Fatalf("unexpected auto recovered stats: %+v", autoRecovered)
	}

	// 再次失败时，冷却期内显式 refresh=true 也应立即清除错误状态并恢复
	mu.Lock()
	failNext = true
	mu.Unlock()
	if _, err := GetInventoryStats(context.Background(), true); err == nil || err.Error() != "模拟数据库异常" {
		t.Fatalf("expected forced refresh error, got %v", err)
	}
	mu.Lock()
	failNext = false
	mu.Unlock()
	recovered, err := GetInventoryStats(context.Background(), true)
	if err != nil {
		t.Fatalf("refresh after error failed: %v", err)
	}
	if recovered.Status != InventoryStatsStatusReady || recovered.Library.Films != 9 {
		t.Fatalf("unexpected recovered stats: %+v", recovered)
	}
}

func TestGetInventoryStats_CollectingSkipsQueryAndResumesAfterDone(t *testing.T) {
	setupResetStatsDB(t)
	origWait := inventoryStatsFastWait
	origFetch := fetchInventoriesFn
	origChecker := inventoryStatsCollectingFn
	t.Cleanup(func() {
		inventoryStatsFastWait = origWait
		fetchInventoriesFn = origFetch
		SetInventoryStatsCollectingChecker(origChecker)
		inventoryStatsMu.Lock()
		inventoryStatsData = inventoryStatsSnapshot{}
		inventoryStatsInflight = nil
		inventoryStatsPending = nil
		inventoryStatsDirty = false
		inventoryStatsMu.Unlock()
	})

	inventoryStatsFastWait = 20 * time.Millisecond
	collecting := true
	var mu sync.Mutex
	var fetchCalls int
	release := make(chan struct{})
	SetInventoryStatsCollectingChecker(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return collecting
	})
	fetchInventoriesFn = func(_ context.Context, _ *gorm.DB) ([]SourceInventory, error) {
		mu.Lock()
		fetchCalls++
		mu.Unlock()
		<-release
		return []SourceInventory{{Id: "src_a", Name: "主站A", Enabled: true, Films: 88}}, nil
	}

	// 1. 采集中（无论普通加载、手动刷新还是触发重算）都不查库，直接返回 collecting（数据入库中）
	RefreshInventoryStatsAsync()
	s1, err := GetInventoryStats(context.Background(), false)
	if err != nil || s1.Status != InventoryStatsStatusCollecting {
		t.Fatalf("expected collecting, got stats=%+v err=%v", s1, err)
	}
	s2, err := GetInventoryStats(context.Background(), true)
	if err != nil || s2.Status != InventoryStatsStatusCollecting {
		t.Fatalf("expected collecting even on refresh=true, got stats=%+v err=%v", s2, err)
	}
	mu.Lock()
	if fetchCalls != 0 {
		mu.Unlock()
		t.Fatalf("expected 0 DB queries while collecting, got %d", fetchCalls)
	}
	// 2. 采集结束并触发收尾统计：未出结果前返回 computing，算完后返回 ready
	collecting = false
	mu.Unlock()

	RefreshInventoryStatsAsync()
	s3, err := GetInventoryStats(context.Background(), false)
	if err != nil || s3.Status != InventoryStatsStatusComputing {
		t.Fatalf("expected computing after collection ends, got stats=%+v err=%v", s3, err)
	}

	close(release)
	var ready InventoryStats
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		ready, err = GetInventoryStats(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		if ready.Status == InventoryStatsStatusReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ready.Status != InventoryStatsStatusReady || ready.Library.Films != 88 {
		t.Fatalf("unexpected ready stats: %+v", ready)
	}
	mu.Lock()
	if fetchCalls != 1 {
		mu.Unlock()
		t.Fatalf("expected 1 DB query after collection ended, got %d", fetchCalls)
	}
	mu.Unlock()
}

func TestGetInventoryStats_DirtySkipsDayCacheAfterCollect(t *testing.T) {
	setupResetStatsDB(t)
	origWait := inventoryStatsFastWait
	origFetch := fetchInventoriesFn
	origChecker := inventoryStatsCollectingFn
	t.Cleanup(func() {
		inventoryStatsFastWait = origWait
		fetchInventoriesFn = origFetch
		SetInventoryStatsCollectingChecker(origChecker)
		inventoryStatsMu.Lock()
		inventoryStatsData = inventoryStatsSnapshot{}
		inventoryStatsInflight = nil
		inventoryStatsPending = nil
		inventoryStatsDirty = false
		inventoryStatsMu.Unlock()
	})

	inventoryStatsFastWait = 20 * time.Millisecond
	var mu sync.Mutex
	films := int64(1)
	collecting := false
	var fetchCalls int
	SetInventoryStatsCollectingChecker(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return collecting
	})
	fetchInventoriesFn = func(_ context.Context, _ *gorm.DB) ([]SourceInventory, error) {
		mu.Lock()
		defer mu.Unlock()
		fetchCalls++
		return []SourceInventory{{Id: "src_a", Name: "主站A", Enabled: true, Films: films}}, nil
	}

	var warm InventoryStats
	var err error
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		warm, err = GetInventoryStats(context.Background(), true)
		if err != nil {
			t.Fatal(err)
		}
		if warm.Status == InventoryStatsStatusReady && warm.Library.Films == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if warm.Library.Films != 1 || warm.Status != InventoryStatsStatusReady {
		t.Fatalf("expected warm cache Films=1, got %+v", warm)
	}

	mu.Lock()
	collecting = true
	films = 2
	callsWhileCollecting := fetchCalls
	mu.Unlock()
	RefreshInventoryStatsAsync()
	mu.Lock()
	if fetchCalls != callsWhileCollecting {
		mu.Unlock()
		t.Fatalf("collecting refresh queried the db: %d -> %d", callsWhileCollecting, fetchCalls)
	}
	collecting = false
	mu.Unlock()

	got, err := GetInventoryStats(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == InventoryStatsStatusReady && got.Library.Films == 1 {
		t.Fatalf("stale day cache served after a skipped refresh: %+v", got)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		got, err = GetInventoryStats(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == InventoryStatsStatusReady && got.Library.Films == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Status != InventoryStatsStatusReady || got.Library.Films != 2 {
		t.Fatalf("expected refreshed Films=2 without another explicit refresh, got %+v", got)
	}
}
