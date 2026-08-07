package film

import (
	"fmt"
	"testing"

	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openContentKeyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 每个测试独立 in-memory 库
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}, &model.FilmListSnapshot{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	return gdb
}

func seedIndex(t *testing.T, gdb *gorm.DB, mid int64, contentKey, name string) model.FilmIndex {
	t.Helper()
	row := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:        mid,
			ContentKey: contentKey,
			SourceId:   "master",
		},
		FilmIndexContent: model.FilmIndexContent{Name: name},
	}
	if err := gdb.Create(&row).Error; err != nil {
		t.Fatalf("seed index mid=%d key=%s: %v", mid, contentKey, err)
	}
	return row
}

func TestMigrateLegacyContentKeysBasic(t *testing.T) {
	gdb := openContentKeyTestDB(t)
	seedIndex(t, gdb, 87682, "name_abc", "烬九州第四季")
	seedIndex(t, gdb, 100, "name_xyz", "别的片")

	n, err := MigrateLegacyContentKeys(gdb)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 migrated, got %d", n)
	}

	var keys []string
	if err := gdb.Model(&model.FilmIndex{}).Order("mid").Pluck("content_key", &keys).Error; err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != "vod_100" || keys[1] != "vod_87682" {
		t.Fatalf("keys=%v want [vod_100 vod_87682]", keys)
	}

	// 幂等
	n2, err := MigrateLegacyContentKeys(gdb)
	if err != nil {
		t.Fatalf("migrate2: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("idempotent want 0, got %d", n2)
	}
	legacy, err := HasLegacyContentKeyInventory(gdb)
	if err != nil || legacy {
		t.Fatalf("legacy after migrate: legacy=%v err=%v", legacy, err)
	}
}

func TestMigrateLegacyContentKeysFreesSoftDeletedOccupant(t *testing.T) {
	gdb := openContentKeyTestDB(t)

	// 软删行占着 vod_1
	tomb := seedIndex(t, gdb, 999, "vod_1", "tomb")
	if err := gdb.Delete(&tomb).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// 活跃 name_* 要迁到 vod_1
	seedIndex(t, gdb, 1, "name_one", "live")

	n, err := MigrateLegacyContentKeys(gdb)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 migrated, got %d", n)
	}

	var live model.FilmIndex
	if err := gdb.Where("mid = ?", 1).First(&live).Error; err != nil {
		t.Fatal(err)
	}
	if live.ContentKey != "vod_1" {
		t.Fatalf("live key=%q", live.ContentKey)
	}

	var soft model.FilmIndex
	if err := gdb.Unscoped().Where("id = ?", tomb.ID).First(&soft).Error; err != nil {
		t.Fatal(err)
	}
	if soft.ContentKey != fmt.Sprintf("del_%d", tomb.ID) {
		t.Fatalf("tomb key=%q want del_%d", soft.ContentKey, tomb.ID)
	}
}

func TestMigrateLegacyContentKeysSkipsOccupiedActiveKey(t *testing.T) {
	gdb := openContentKeyTestDB(t)
	// mid=10 name 待迁 → vod_10
	seedIndex(t, gdb, 10, "name_ten", "need")
	// mid=11 脏数据占用 content_key=vod_10
	seedIndex(t, gdb, 11, "vod_10", "occupier")

	n, err := MigrateLegacyContentKeys(gdb)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 0 {
		t.Fatalf("occupied target should skip, got n=%d", n)
	}
	var row model.FilmIndex
	if err := gdb.Where("mid = ?", 10).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ContentKey != "name_ten" {
		t.Fatalf("should remain name_ten, got %q", row.ContentKey)
	}
	legacy, _ := HasLegacyContentKeyInventory(gdb)
	if !legacy {
		t.Fatal("expect residual legacy")
	}
}

func TestMigrateLegacyContentKeysUpdatesSnapshots(t *testing.T) {
	gdb := openContentKeyTestDB(t)
	seedIndex(t, gdb, 3, "name_snap", "片")

	snap := model.FilmListSnapshot{
		SnapshotVersion: "v1",
		Mid:             3,
		ContentKey:      "name_snap",
		Name:            "片",
	}
	if err := gdb.Create(&snap).Error; err != nil {
		t.Fatal(err)
	}

	n, err := MigrateLegacyContentKeys(gdb)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
	var got model.FilmListSnapshot
	if err := gdb.Where("mid = ?", 3).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.ContentKey != "vod_3" {
		t.Fatalf("snapshot key=%q", got.ContentKey)
	}
}

func TestMigrateLegacyContentKeysIdempotentEmpty(t *testing.T) {
	gdb := openContentKeyTestDB(t)
	seedIndex(t, gdb, 1, "vod_1", "already new")
	n, err := MigrateLegacyContentKeys(gdb)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestEnsureContentKeySchemaReadyAfterMigrate(t *testing.T) {
	gdb := openContentKeyTestDB(t)
	seedIndex(t, gdb, 42, "name_x", "x")
	InvalidateContentKeySchemaCache()
	if err := EnsureContentKeySchemaReady(gdb); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// 第二次应快速通过
	if err := EnsureContentKeySchemaReady(gdb); err != nil {
		t.Fatalf("ensure2: %v", err)
	}
}

func TestEnsureContentKeySchemaReadyBlocksResidual(t *testing.T) {
	gdb := openContentKeyTestDB(t)
	seedIndex(t, gdb, 10, "name_ten", "need")
	seedIndex(t, gdb, 11, "vod_10", "occupier")
	InvalidateContentKeySchemaCache()
	if err := EnsureContentKeySchemaReady(gdb); err == nil {
		t.Fatal("expect error on residual")
	}
}
