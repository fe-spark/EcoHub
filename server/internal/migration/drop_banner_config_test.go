package migration

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrateDropBannerConfig(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test sqlite: %v", err)
	}
	if err := testDB.Exec("CREATE TABLE banner_config (id integer primary key, payload text)").Error; err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if err := testDB.Exec("INSERT INTO banner_config (payload) VALUES ('{}')").Error; err != nil {
		t.Fatalf("seed legacy table: %v", err)
	}

	if err := migrateDropBannerConfig(testDB); err != nil {
		t.Fatalf("drop banner_config failed: %v", err)
	}
	if testDB.Migrator().HasTable("banner_config") {
		t.Fatalf("banner_config should be dropped")
	}
	// 表已经不存在时再跑一次也不报错。
	if err := migrateDropBannerConfig(testDB); err != nil {
		t.Fatalf("second run should be a no-op, got %v", err)
	}
}
