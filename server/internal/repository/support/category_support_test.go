package support

import (
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"server/internal/infra/db"
	"server/internal/model"
)

// setupMockCategoryTree 设置内存模拟分类树，模拟非硬编码 ID 环境（例如：短剧=105, 动漫=99, 电影=50, 电视剧=10, 纪录片=70）
func setupMockCategoryTree() {
	catMu.Lock()
	idToPid = map[int64]int64{
		10:  0,  // 电视剧 (root)
		50:  0,  // 电影 (root)
		70:  0,  // 纪录片 (root)
		99:  0,  // 动漫 (root)
		105: 0,  // 短剧 (root)
		120: 0,  // 综艺 (root)
		501: 50, // 动作片 (子分类，归属电影 50)
		502: 50, // 剧情片 (子分类，归属电影 50)
		101: 10, // 国产剧 (子分类，归属电视剧 10)
	}
	catMu.Unlock()

	ResetCategoryNameCache()
	SetCategoryNameCache(10, model.BigCategoryTV)
	SetCategoryNameCache(50, model.BigCategoryMovie)
	SetCategoryNameCache(70, model.BigCategoryDocumentary)
	SetCategoryNameCache(99, model.BigCategoryAnimation)
	SetCategoryNameCache(105, model.BigCategoryShortFilm)
	SetCategoryNameCache(120, model.BigCategoryVariety)
	SetCategoryNameCache(501, "动作片")
	SetCategoryNameCache(502, "剧情片")
	SetCategoryNameCache(101, "国产剧")

	ClearRootCategoryCNameCache()
}

func TestResolveRootCategoryIDByCName_DynamicAndPrecise(t *testing.T) {
	setupMockCategoryTree()

	tests := []struct {
		name     string
		cName    string
		expected int64
	}{
		// 1. 精确匹配内存已有子类与大类
		{"Exact root: 电影", "电影", 50},
		{"Exact root: 电视剧", "电视剧", 10},
		{"Exact root: 动漫", "动漫", 99},
		{"Exact root: 短剧", "短剧", 105},
		{"Exact sub: 剧情片 (归属电影)", "剧情片", 50},
		{"Exact sub: 动作片 (归属电影)", "动作片", 50},
		{"Exact sub: 国产剧 (归属电视剧)", "国产剧", 10},

		// 2. 外部未规范分类：不盲猜，按主站为准，直接返回 0
		{"Unmapped external: 喜剧片 (库内无该子类，不盲猜)", "喜剧片", 0},
		{"Unmapped external: 惊悚恐怖片 (不盲猜)", "惊悚恐怖片", 0},
		{"Unmapped external: 4K微电影 (不盲猜)", "4K微电影", 0},
		{"Unmapped external: 反转爽剧 (不盲猜)", "反转爽剧", 0},
		{"Unmapped external: 国产动漫 (不盲猜)", "国产动漫", 0},
		{"Unmapped external: 热门综艺 (不盲猜)", "热门综艺", 0},

		// 3. 空值或脏数据
		{"Empty string", "", 0},
		{"Whitespace only", "   ", 0},
		{"Unknown noise", "未知分类xyz", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRootCategoryIDByCName(tt.cName)
			if got != tt.expected {
				t.Errorf("ResolveRootCategoryIDByCName(%q) = %d, expected %d", tt.cName, got, tt.expected)
			}
		})
	}
}

func TestResolveRootCategoryIDByCName_CacheAndInvalidation(t *testing.T) {
	setupMockCategoryTree()

	// 首次解析并缓存
	res1 := ResolveRootCategoryIDByCName("短剧")
	if res1 != 105 {
		t.Fatalf("Expected 105, got %d", res1)
	}

	// 缓存命中检查
	cached, ok := rootCategoryCNameCache.Load("短剧")
	if !ok || cached.(int64) != 105 {
		t.Fatalf("Cache store failed: ok=%v, val=%v", ok, cached)
	}

	// 模拟分类大类 ID 变更（如短剧重建后 ID 变为 205）
	SetCategoryTreeForTest(map[int64]int64{
		205: 0,
	}, map[int64]string{
		205: model.BigCategoryShortFilm,
	})
	res2 := ResolveRootCategoryIDByCName("短剧")
	if res2 != 205 {
		t.Fatalf("Expected updated root ID 205 after cache clear, got %d", res2)
	}
}

func TestResolveRootCategoryIDByCName_ConcurrencyDeadlockFree(t *testing.T) {
	setupMockCategoryTree()

	const readers = 20
	const iterations = 500
	done := make(chan struct{})

	// 模拟后台并发刷新分类缓存（写锁获取）
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				SetCategoryTreeForTest(map[int64]int64{
					10:  0,
					50:  0,
					70:  0,
					99:  0,
					105: 0,
					120: 0,
					501: 50,
					502: 50,
					101: 10,
				}, map[int64]string{
					10:  model.BigCategoryTV,
					50:  model.BigCategoryMovie,
					70:  model.BigCategoryDocumentary,
					99:  model.BigCategoryAnimation,
					105: model.BigCategoryShortFilm,
					120: model.BigCategoryVariety,
					501: "动作片",
					502: "剧情片",
					101: "国产剧",
				})
			}
		}
	}()

	// 模拟并发读
	var wg sync.WaitGroup
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func(idx int) {
			defer wg.Done()
			cNames := []string{"短剧", "电影", "动漫", "电视剧", "剧情片", "未定义分类xyz"}
			for j := 0; j < iterations; j++ {
				cName := cNames[(idx+j)%len(cNames)]
				_ = ResolveRootCategoryIDByCName(cName)
			}
		}(i)
	}

	wg.Wait()
	close(done)
}

func TestPickPrimarySourceForCategory_DisabledReturnsNil(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
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
	t.Cleanup(func() { db.Mdb = prev })

	// 1. 无任何站点
	if primary := PickPrimarySourceForCategory(); primary != nil {
		t.Fatalf("expected nil when no sources, got %+v", primary)
	}

	// 2. 插入两个站点，但全部为停用 (state = false)
	if err := gdb.Create(&model.FilmSource{Id: "src_1", Name: "站1", Uri: "http://src1.test", State: false, Sort: 0}).Error; err != nil {
		t.Fatalf("create src_1 error: %v", err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src_2", Name: "站2", Uri: "http://src2.test", State: false, Sort: 1}).Error; err != nil {
		t.Fatalf("create src_2 error: %v", err)
	}

	if primary := PickPrimarySourceForCategory(); primary != nil {
		t.Fatalf("expected nil when all sources are disabled, got %+v", primary)
	}

	// 3. 启用站2
	if err := gdb.Model(&model.FilmSource{}).Where("id = ?", "src_2").Update("state", true).Error; err != nil {
		t.Fatalf("update error: %v", err)
	}
	primary := PickPrimarySourceForCategory()
	if primary == nil || primary.Id != "src_2" {
		t.Fatalf("expected src_2 as primary, got %+v", primary)
	}
}

func TestIsCategoryVisible_Cascade(t *testing.T) {
	catMu.Lock()
	idToPid = map[int64]int64{
		1:  0, // 电影 (root)
		2:  0, // 电视剧 (root)
		11: 1, // 动作片 (子分类，归属电影)
		12: 1, // 喜剧片 (子分类，归属电影)
		21: 2, // 国产剧 (子分类，归属电视剧)
	}
	categoryShowMap = map[int64]bool{
		1:  true,  // 电影开启
		2:  false, // 电视剧停用
		11: true,  // 动作片开启
		12: false, // 喜剧片停用
		21: true,  // 国产剧虽然自身标记 true，但父级电视剧停用
	}
	catInitialized = true
	catMu.Unlock()
	t.Cleanup(func() {
		catMu.Lock()
		idToPid = make(map[int64]int64)
		categoryShowMap = make(map[int64]bool)
		catInitialized = false
		catMu.Unlock()
	})

	// 1. 根分类与子分类均开启
	if !IsCategoryVisible(11, 1) {
		t.Fatalf("expected category 11 under 1 to be visible")
	}

	// 2. 根分类开启，但子分类停用
	if IsCategoryVisible(12, 1) {
		t.Fatalf("expected category 12 (disabled) to be hidden")
	}

	// 3. 根分类停用，哪怕子分类自身 show=true，整体也必须不可见
	if IsCategoryVisible(21, 2) {
		t.Fatalf("expected category 21 under disabled root 2 to be hidden")
	}

	// 4. 单独传入停用的 pid
	if IsCategoryVisible(0, 2) {
		t.Fatalf("expected disabled pid 2 to be hidden")
	}

	// 5. 单独传入停用的 cid
	if IsCategoryVisible(12, 0) {
		t.Fatalf("expected disabled cid 12 to be hidden")
	}

	// 6. 传入未在后台显式禁用的未知 cid，默认放行
	if !IsCategoryVisible(999, 0) {
		t.Fatalf("expected unconfigured cid 999 to be visible by default")
	}
}

