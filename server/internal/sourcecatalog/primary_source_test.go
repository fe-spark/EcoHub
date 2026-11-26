package sourcecatalog

import (
	"sync"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"
)

func setupPrimaryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.PrimaryState{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	InvalidatePrimarySourceCache()
	t.Cleanup(func() {
		db.Mdb = prev
		InvalidatePrimarySourceCache()
	})
	return gdb
}

// 存储为空时按排序首位懒初始化并持久化；之后排序变化不再改变主站。
func TestCurrentPrimarySource_LazyPickPersistsAndWinsOverSort(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "a", Name: "A", Uri: "http://a", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "b", Name: "B", Uri: "http://b", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}

	if p := CurrentPrimarySource(); p == nil || p.Id != "b" {
		t.Fatalf("expected lazy-picked b, got %+v", p)
	}
	var rec model.PrimaryState
	if err := gdb.Where("id = ?", 1).First(&rec).Error; err != nil {
		t.Fatalf("primary state should be persisted: %v", err)
	}
	if rec.PrimarySourceId != "b" {
		t.Fatalf("expected persisted b, got %q", rec.PrimarySourceId)
	}

	// 排序把 a 顶到首位，主站不变（S-05 语义）
	if err := gdb.Model(&model.FilmSource{}).Where("id = ?", "a").Update("sort", -1).Error; err != nil {
		t.Fatal(err)
	}
	if p := CurrentPrimarySource(); p == nil || p.Id != "b" {
		t.Fatalf("stored primary must win over sort order, got %+v", p)
	}
	if id := CurrentPrimaryID(); id != "b" {
		t.Fatalf("expected id b, got %q", id)
	}
}

// SetPrimarySourceID 显式写入后，主站以存储为准。
func TestSetPrimarySourceID_StoredWins(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "a", Name: "A", Uri: "http://a", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "b", Name: "B", Uri: "http://b", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := SetPrimarySourceID("b"); err != nil {
		t.Fatal(err)
	}
	if p := CurrentPrimarySource(); p == nil || p.Id != "b" {
		t.Fatalf("expected stored b, got %+v", p)
	}
	var srcB, srcA model.FilmSource
	if err := gdb.Where("id = ?", "b").First(&srcB).Error; err != nil {
		t.Fatal(err)
	}
	if srcB.Sort != 0 {
		t.Fatalf("expected b moved to sort 0, got %d", srcB.Sort)
	}
	if err := gdb.Where("id = ?", "a").First(&srcA).Error; err != nil {
		t.Fatal(err)
	}
	if srcA.Sort != 1 {
		t.Fatalf("expected a shifted to sort 1, got %d", srcA.Sort)
	}
}

// D6：存储指向停用站属异常，返回 nil，不自动换站。
func TestCurrentPrimarySource_StoredDisabledReturnsNil(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "a", Name: "A", Uri: "http://a", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "b", Name: "B", Uri: "http://b", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := SetPrimarySourceID("a"); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&model.FilmSource{}).Where("id = ?", "a").Update("state", false).Error; err != nil {
		t.Fatal(err)
	}
	if p := CurrentPrimarySource(); p != nil {
		t.Fatalf("expected nil for disabled stored primary, got %+v", p)
	}
	if id := CurrentPrimaryID(); id != "" {
		t.Fatalf("expected empty id for disabled stored primary, got %q", id)
	}
}

// D6：存储指向删除中的站同样不自动换站。
func TestCurrentPrimarySource_StoredDeletingReturnsNil(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "a", Name: "A", Uri: "http://a", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := SetPrimarySourceID("a"); err != nil {
		t.Fatal(err)
	}
	RegisterSourceDeletingChecker(func(id string) bool { return id == "a" })
	t.Cleanup(func() { RegisterSourceDeletingChecker(nil) })
	if p := CurrentPrimarySource(); p != nil {
		t.Fatalf("expected nil for deleting stored primary, got %+v", p)
	}
}

// 存储指向不存在的站：告警并返回 nil，等待人工切换。
func TestCurrentPrimarySource_StoredMissingReturnsNil(t *testing.T) {
	setupPrimaryTestDB(t)
	if err := SetPrimarySourceID("ghost"); err != nil {
		t.Fatal(err)
	}
	if p := CurrentPrimarySource(); p != nil {
		t.Fatalf("expected nil for missing stored primary, got %+v", p)
	}
}

// 无任何启用站时为空主站（D5）。
func TestCurrentPrimarySource_NoEnabledReturnsNil(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "a", Name: "A", Uri: "http://a", State: false, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if p := CurrentPrimarySource(); p != nil {
		t.Fatalf("expected nil when no enabled sources, got %+v", p)
	}
	if err := gdb.Model(&model.FilmSource{}).Where("id = ?", "a").Update("state", true).Error; err != nil {
		t.Fatal(err)
	}
	// 主站为空时，第一个被启用的站自动成为主站（D5）
	if p := CurrentPrimarySource(); p == nil || p.Id != "a" {
		t.Fatalf("expected a after enabling, got %+v", p)
	}
}

// 高并发调用 CurrentPrimarySource 与 CurrentPrimaryID 不死锁、无竞态
func TestCurrentPrimarySource_ConcurrentReadSafe(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "main_src", Name: "Main", Uri: "http://main", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	const workers = 50
	const iters = 200
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				p := CurrentPrimarySource()
				if p == nil || p.Id != "main_src" {
					t.Errorf("unexpected primary: %+v", p)
					return
				}
				if id := CurrentPrimaryID(); id != "main_src" {
					t.Errorf("unexpected primary id: %s", id)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// 缓存已预热后若主站进入删除态，CurrentPrimarySource 必须立即返回 nil，撤回后恢复
func TestCurrentPrimarySource_WarmedCacheRespectsDeletingState(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "warm_a", Name: "WarmA", Uri: "http://a", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := SetPrimarySourceID("warm_a"); err != nil {
		t.Fatal(err)
	}
	// 先预热缓存
	if p := CurrentPrimarySource(); p == nil || p.Id != "warm_a" {
		t.Fatalf("expected warm_a before deleting, got %+v", p)
	}

	deleting := true
	RegisterSourceDeletingChecker(func(id string) bool {
		return deleting && id == "warm_a"
	})
	t.Cleanup(func() { RegisterSourceDeletingChecker(nil) })

	if p := CurrentPrimarySource(); p != nil {
		t.Fatalf("expected nil while warm_a is deleting even with warm cache, got %+v", p)
	}

	deleting = false
	if p := CurrentPrimarySource(); p == nil || p.Id != "warm_a" {
		t.Fatalf("expected warm_a restored after cancel, got %+v", p)
	}
}

// 并发切换主站与单飞回源交叉执行时，缓存不会写入旧主站快照
func TestCurrentPrimarySource_ConcurrentInvalidateNoStaleCache(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "s1", Name: "S1", Uri: "http://1", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "s2", Name: "S2", Uri: "http://2", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := SetPrimarySourceID("s1"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = SetPrimarySourceID("s2")
			_ = SetPrimarySourceID("s1")
		}
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = CurrentPrimarySource()
			}
		}()
	}
	wg.Wait()

	if err := SetPrimarySourceID("s2"); err != nil {
		t.Fatal(err)
	}
	if p := CurrentPrimarySource(); p == nil || p.Id != "s2" {
		t.Fatalf("expected final primary s2, got %+v", p)
	}
}

func TestCurrentPrimarySource_DisabledReturnsNil(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	InvalidatePrimarySourceCache()
	t.Cleanup(func() {
		db.Mdb = prev
		InvalidatePrimarySourceCache()
	})

	// 1. 无任何站点
	if primary := CurrentPrimarySource(); primary != nil {
		t.Fatalf("expected nil when no sources, got %+v", primary)
	}

	// 2. 插入两个站点，但全部为停用 (state = false)
	if err := gdb.Create(&model.FilmSource{Id: "src_1", Name: "站1", Uri: "http://src1.test", State: false, Sort: 0}).Error; err != nil {
		t.Fatalf("create src_1 error: %v", err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src_2", Name: "站2", Uri: "http://src2.test", State: false, Sort: 1}).Error; err != nil {
		t.Fatalf("create src_2 error: %v", err)
	}

	if primary := CurrentPrimarySource(); primary != nil {
		t.Fatalf("expected nil when all sources are disabled, got %+v", primary)
	}

	// 3. 启用站2
	if err := gdb.Model(&model.FilmSource{}).Where("id = ?", "src_2").Update("state", true).Error; err != nil {
		t.Fatalf("update error: %v", err)
	}
	primary := CurrentPrimarySource()
	if primary == nil || primary.Id != "src_2" {
		t.Fatalf("expected src_2 as primary, got %+v", primary)
	}
}

func TestCollectSourceCatalog_InvalidateAndConcurrency(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	InvalidateCollectSourceCatalog()
	t.Cleanup(InvalidateCollectSourceCatalog)

	if err := gdb.Create(&model.FilmSource{Id: "cat_a", Name: "站A", Uri: "http://a.test", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "cat_b", Name: "站B", Uri: "http://b.test", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}

	// 并发冷加载 + ForgetCollectSource + RefreshCollectSourceCatalog 在 -race 下不得出现竞态或复活
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = GetCollectSourceList()
			_ = FindCollectSourceForDelete("cat_a")
		}()
	}
	wg.Wait()

	if list := GetCollectSourceList(); len(list) != 2 {
		t.Fatalf("expected 2 sources in catalog, got %+v", list)
	}

	// 清表并调用 InvalidateCollectSourceCatalog（模拟 siteops.FilmZero 同 db.Mdb 重置场景）
	if err := gdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.FilmSource{}).Error; err != nil {
		t.Fatal(err)
	}
	InvalidateCollectSourceCatalog()
	if list := GetCollectSourceList(); len(list) != 0 {
		t.Fatalf("expected empty catalog after invalidate, got %+v", list)
	}
	if src := FindCollectSourceForDelete("cat_a"); src != nil {
		t.Fatalf("expected nil after invalidate, got %+v", src)
	}

	// 重新写入后验证 ForgetCollectSource 与并发 Refresh 无覆盖丢失
	if err := gdb.Create(&model.FilmSource{Id: "cat_c", Name: "站C", Uri: "http://c.test", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "cat_d", Name: "站D", Uri: "http://d.test", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	_ = RefreshCollectSourceCatalog()

	if err := gdb.Where("id = ?", "cat_c").Delete(&model.FilmSource{}).Error; err != nil {
		t.Fatal(err)
	}
	var opWG sync.WaitGroup
	opWG.Add(2)
	go func() {
		defer opWG.Done()
		ForgetCollectSource("cat_c")
	}()
	go func() {
		defer opWG.Done()
		_ = RefreshCollectSourceCatalog()
	}()
	opWG.Wait()

	list := GetCollectSourceList()
	if len(list) != 1 || list[0].Id != "cat_d" {
		t.Fatalf("expected only cat_d remaining after concurrent forget+refresh, got %+v", list)
	}
}
