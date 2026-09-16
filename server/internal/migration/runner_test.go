package migration

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRunAutoMigrations(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test sqlite: %v", err)
	}

	origMdb := db.Mdb
	db.Mdb = testDB
	defer func() {
		db.Mdb = origMdb
	}()

	// 1. 初始化模型
	if err := testDB.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// 2. 首次执行迁移
	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("RunAutoMigrations first run failed: %v", err)
	}

	// 3. 验证 schema_migrations 记录数
	var count int64
	if err := testDB.Model(&model.SchemaMigration{}).Count(&count).Error; err != nil {
		t.Fatalf("Count schema_migrations failed: %v", err)
	}
	if count != int64(len(migrations)) {
		t.Fatalf("expected %d migrations recorded, got %d", len(migrations), count)
	}

	// 4. 二次执行迁移（测试幂等性与跳过逻辑）
	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("RunAutoMigrations second run failed: %v", err)
	}

	// 再次验证记录数没有增加
	var countAfter int64
	if err := testDB.Model(&model.SchemaMigration{}).Count(&countAfter).Error; err != nil {
		t.Fatalf("Count schema_migrations failed: %v", err)
	}
	if countAfter != count {
		t.Fatalf("expected count to remain %d, got %d", count, countAfter)
	}
}
