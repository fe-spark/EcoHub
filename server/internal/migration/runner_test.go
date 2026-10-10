package migration

import (
	"testing"

	"server/internal/model"

	"gorm.io/driver/sqlite"
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
	if count != 1 {
		t.Fatalf("expected 1 baseline migration, got %d", count)
	}

	var row model.SchemaMigration
	if err := testDB.First(&row).Error; err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	if row.Version != "20261009_baseline" {
		t.Fatalf("unexpected version %s", row.Version)
	}

	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	var countAfter int64
	if err := testDB.Model(&model.SchemaMigration{}).Count(&countAfter).Error; err != nil {
		t.Fatalf("count after second run: %v", err)
	}
	if countAfter != 1 {
		t.Fatalf("expected count to stay 1, got %d", countAfter)
	}
}

func TestRunAutoMigrations_NilDB(t *testing.T) {
	if err := RunAutoMigrations(nil); err != nil {
		t.Fatalf("nil db should be a no-op, got %v", err)
	}
}
