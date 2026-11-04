package migration

import (
	"testing"

	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRunAutoMigrations_EmptyBaselineIdempotent(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test sqlite: %v", err)
	}
	if err := testDB.AutoMigrate(&model.SchemaMigration{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	var count int64
	if err := testDB.Model(&model.SchemaMigration{}).Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != int64(len(migrations)) {
		t.Fatalf("expected %d migrations, got %d", len(migrations), count)
	}

	var row model.SchemaMigration
	if err := testDB.First(&row).Error; err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	if row.Version != "20261009_baseline" || row.Name != "当前版本基线，无结构变更" {
		t.Fatalf("unexpected baseline %+v", row)
	}

	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	var countAfter int64
	if err := testDB.Model(&model.SchemaMigration{}).Count(&countAfter).Error; err != nil {
		t.Fatalf("count after second run: %v", err)
	}
	if countAfter != int64(len(migrations)) {
		t.Fatalf("expected count to stay %d, got %d", len(migrations), countAfter)
	}
}

func TestRunAutoMigrations_NilDB(t *testing.T) {
	if err := RunAutoMigrations(nil); err != nil {
		t.Fatalf("nil db should be a no-op, got %v", err)
	}
}
