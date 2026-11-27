package access

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestResolveClassifyMemberMemoryOnly_ZeroSQLWhenDBClosed(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	// 故意关闭底层数据库连接，任何意外 SQL 都会触发报错或无法查询；且确保走内存分支。
	_ = sqlDB.Close()
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	taxonomy.SetCategoryTreeForTest(map[int64]int64{10: 0}, map[int64]string{10: "科幻片"})

	catNameCacheMu.Lock()
	catNameCache = map[int64]string{}
	catIdByNameCache = map[string]int64{}
	catNameCacheAt = time.Time{}
	catNameCacheMu.Unlock()

	if got := resolveClassifyMemberMemoryOnly(&AccessEvent{Action: ActionClassify, Resource: "科幻片"}); got != "10" {
		t.Fatalf("期望从内存分类快照命中 ID=10，实际=%q", got)
	}
	if got := resolveClassifyMemberMemoryOnly(&AccessEvent{Action: ActionClassify, Resource: "不存在分类"}); got != "不存在分类" {
		t.Fatalf("未知分类应直接回退原始文本且不查库，实际=%q", got)
	}
}

func TestPageCollectSource_UsesInMemoryCatalog(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.PrimaryState{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src_mem_1", Name: "Mem1", Uri: "https://example.com", State: true}).Error; err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() {
		db.Mdb = prev
		sourcecatalog.InvalidateCollectSourceCatalog()
	})
	if err := sourcecatalog.RefreshCollectSourceCatalog(); err != nil {
		t.Fatal(err)
	}
	// 删掉 film_sources 表，验证 pageCollectSource 完全依赖内存目录而非直接查表
	if err := gdb.Migrator().DropTable(&model.FilmSource{}); err != nil {
		t.Fatal(err)
	}
	if got := pageCollectSource(ActionPlay, "src_mem_1", "100"); got != "src_mem_1" {
		t.Fatalf("期望命中内存目录 src_mem_1，实际=%q", got)
	}
}

func TestPageTooFast_LimitsDistinctPagesByIP(t *testing.T) {
	resetPageHits()
	t.Cleanup(resetPageHits)

	ip := "203.0.113.55"
	for i := 0; i < maxPageEventsPerIPWindow; i++ {
		if pageTooFast(ip, fmt.Sprintf("/film/%d", i)) {
			t.Fatalf("第 %d 次不同路径请求不应被单 IP 窗口拦截", i)
		}
	}
	if !pageTooFast(ip, "/film/overflow") {
		t.Fatal("单 IP 短时间超过上限后即使更换 page 也应被限流")
	}
}

func TestAdmitBoundedHashFieldAndCompactTopIntMap(t *testing.T) {
	hashFieldGateMu.Lock()
	hashFieldGate = map[string]map[string]struct{}{}
	hashFieldGateMu.Unlock()

	key := "test:app:models:20261127"
	big := map[string]int64{}
	for i := 0; i < maxBoundedHashFields+100; i++ {
		name := fmt.Sprintf("Vendor-Device-Model-VeryLongName-%04d", i)
		got := admitBoundedHashField(key, name)
		if i < maxBoundedHashFields && got != name {
			t.Fatalf("上限内应保留原字段名，实际=%q", got)
		}
		if i >= maxBoundedHashFields && got != "other" {
			t.Fatalf("超出上限应折叠到 other，实际=%q", got)
		}
		big[name] = int64(i + 1)
	}

	compacted := compactTopIntMap(big, maxPersistedModels, maxDailyTextJSONBytes)
	raw, err := json.Marshal(compacted)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > maxDailyTextJSONBytes {
		t.Fatalf("压缩后 JSON 超过安全字节上限 %d: got=%d", maxDailyTextJSONBytes, len(raw))
	}
	if compacted["other"] <= 0 {
		t.Fatal("被折叠的尾部机型计数应汇总进 other")
	}
}
