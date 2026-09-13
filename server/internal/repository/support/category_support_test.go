package support

import (
	"sync"
	"testing"

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

