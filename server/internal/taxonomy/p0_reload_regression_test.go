package taxonomy

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReloadMappingRules_PreservesSnapshotOnDBError(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.MappingRule{}, &model.CategoryMapping{}, &model.SourceCategory{}, &model.Category{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.MappingRule{
		Group:     "Area",
		Raw:       "美国",
		Target:    "欧美",
		MatchType: "exact",
	}).Error; err != nil {
		t.Fatal(err)
	}

	prevDB := db.Mdb
	db.Mdb = gdb
	prevSnap := CurrentMappingSnapshotForTest()
	t.Cleanup(func() {
		db.Mdb = prevDB
		StoreMappingSnapshotForTest(prevSnap)
	})

	if err := ReloadMappingRules(); err != nil {
		t.Fatalf("首次加载不应报错: %v", err)
	}
	if got := getMappingSnapshot().Area["美国"]; got != "欧美" {
		t.Fatalf("首次加载规则未生效，Area[美国]=%q", got)
	}

	// 删除 mapping_rules 模拟数据库故障/表不可用
	if err := gdb.Migrator().DropTable(&model.MappingRule{}); err != nil {
		t.Fatal(err)
	}
	if err := ReloadMappingRules(); err == nil {
		t.Fatal("DB 查询失败时 ReloadMappingRules 必须返回错误")
	}
	if got := getMappingSnapshot().Area["美国"]; got != "欧美" {
		t.Fatalf("DB 故障后旧快照被错误覆盖，Area[美国]=%q", got)
	}
}
