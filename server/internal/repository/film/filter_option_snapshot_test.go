package film

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupFilterSnapshotTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.Category{},
		&model.SearchTagItem{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	db.Mdb = gdb
	return gdb
}

func TestGetFilterOptionSnapshotDirectMetadata(t *testing.T) {
	gdb := setupFilterSnapshotTestDB(t)

	// 1. 初始化分类
	rootCat := model.Category{Id: 1, Pid: 0, Name: "电影", Show: true, Sort: 1, StableKey: "movie"}
	subCat := model.Category{Id: 10, Pid: 1, Name: "动作片", Show: true, Sort: 1, StableKey: "action"}
	if err := gdb.Create(&rootCat).Error; err != nil {
		t.Fatalf("create rootCat: %v", err)
	}
	if err := gdb.Create(&subCat).Error; err != nil {
		t.Fatalf("create subCat: %v", err)
	}

	// 2. 插入搜索标签
	tag := model.SearchTagItem{
		Pid:     1,
		TagType: "Area",
		Name:    "中国大陆",
		Value:   "中国大陆",
		Score:   100,
	}
	if err := gdb.Create(&tag).Error; err != nil {
		t.Fatalf("create tag: %v", err)
	}

	res := GetFilterOptionSnapshot("test_v1", 1)
	if res == nil {
		t.Fatal("expected non-nil response")
	}

	tags, ok := res["tags"].(map[string]any)
	if !ok {
		t.Fatalf("expected tags map, got %T", res["tags"])
	}

	if catList, ok := tags["Category"].([]map[string]string); !ok || len(catList) < 2 {
		t.Fatalf("expected Category tags with '全部' and subcategory, got %v", tags["Category"])
	}

	if areaList, ok := tags["Area"].([]map[string]string); !ok || len(areaList) < 1 {
		t.Fatalf("expected Area tags, got %v", tags["Area"])
	}
}

func TestEmptyFilterOptionResponse(t *testing.T) {
	res := emptyFilterOptionResponse()
	if res == nil {
		t.Fatal("expected non-nil response")
	}
	titles, ok := res["titles"].(map[string]string)
	if !ok || titles["Sort"] != "排序" {
		t.Fatalf("expected Sort title, got %v", res["titles"])
	}
}
