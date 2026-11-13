package migration

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"server/internal/model"
)

func TestMigrateCollectIntervalAddsColumn(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateCollectInterval(gdb); err != nil {
		t.Fatal(err)
	}
	if !gdb.Migrator().HasColumn(&model.FilmSource{}, "CollectIntervalMs") {
		t.Fatal("missing collect_interval_ms")
	}
	if err := migrateCollectInterval(gdb); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateCollectIntervalDefaultBackfillsZero(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateCollectInterval(gdb); err != nil {
		t.Fatal(err)
	}
	rows := []model.FilmSource{
		{Id: "old", Name: "旧", Uri: "http://old", CollectIntervalMs: 1},
		{Id: "keep", Name: "保留", Uri: "http://keep", CollectIntervalMs: 80},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&model.FilmSource{}).Where("id = ?", "old").Update("collect_interval_ms", 0).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateCollectInterval(gdb); err != nil {
		t.Fatal(err)
	}
	if err := migrateCollectInterval(gdb); err != nil {
		t.Fatal(err)
	}

	var old, keep model.FilmSource
	if err := gdb.First(&old, "id = ?", "old").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.First(&keep, "id = ?", "keep").Error; err != nil {
		t.Fatal(err)
	}
	if old.CollectIntervalMs != model.CollectIntervalMsDefault {
		t.Fatalf("zero interval = %d, want %d", old.CollectIntervalMs, model.CollectIntervalMsDefault)
	}
	if keep.CollectIntervalMs != 80 {
		t.Fatalf("explicit interval = %d, want 80", keep.CollectIntervalMs)
	}
}
