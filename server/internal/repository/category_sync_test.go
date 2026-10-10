package repository

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSaveCategoryTree_SameNameStaysPerSource(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Category{}, &model.CategoryMapping{}, &model.SourceCategory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	db.Rdb = nil

	for _, item := range []struct {
		source string
		typeID int64
	}{
		{"src_a", 4},
		{"src_b", 17},
	} {
		tree := &model.CategoryTree{Children: []*model.CategoryTree{{Id: item.typeID, Name: "动漫", Show: true}}}
		if err := SaveCategoryTree(item.source, tree); err != nil {
			t.Fatalf("save %s: %v", item.source, err)
		}
	}

	var categories []model.Category
	if err := gdb.Where("name = ?", "动漫").Find(&categories).Error; err != nil {
		t.Fatalf("load categories: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("same name must stay two categories, got %d", len(categories))
	}
	var mappings []model.CategoryMapping
	if err := gdb.Order("source_id ASC").Find(&mappings).Error; err != nil {
		t.Fatalf("load mappings: %v", err)
	}
	if len(mappings) != 2 || mappings[0].CategoryId == mappings[1].CategoryId {
		t.Fatalf("sources must not share a category id, got %+v", mappings)
	}
}

func TestSaveCategoryTree_KeepsCategoriesUsedByOtherSources(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Category{}, &model.CategoryMapping{}, &model.SourceCategory{}, &model.MappingRule{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	db.Rdb = nil

	gdb.Create(&model.Category{Id: 90, Pid: 0, Name: "连续剧", StableKey: "display:root:连续剧", Show: true})
	gdb.Create(&model.Category{Id: 91, Pid: 0, Name: "综艺", StableKey: "display:root:综艺", Show: true})
	gdb.Create(&model.CategoryMapping{SourceId: "src_other", SourceTypeId: 13, CategoryId: 90})
	gdb.Create(&model.CategoryMapping{SourceId: "src_edit", SourceTypeId: 8, CategoryId: 90})
	gdb.Create(&model.CategoryMapping{SourceId: "src_edit", SourceTypeId: 9, CategoryId: 91})

	tree := &model.CategoryTree{
		Children: []*model.CategoryTree{{Id: 1, Name: "电影", Show: true}},
	}
	if err := SaveCategoryTree("src_edit", tree); err != nil {
		t.Fatalf("SaveCategoryTree: %v", err)
	}

	var shared model.Category
	if err := gdb.First(&shared, 90).Error; err != nil {
		t.Fatalf("category used by another source was deleted: %v", err)
	}
	var exclusive model.Category
	if err := gdb.First(&exclusive, 91).Error; err == nil {
		t.Fatalf("category only used by the edited source should be removed")
	}
	var otherMap int64
	gdb.Model(&model.CategoryMapping{}).Where("source_id = ? AND category_id = ?", "src_other", 90).Count(&otherMap)
	if otherMap != 1 {
		t.Fatalf("expected the other source mapping to remain, got %d", otherMap)
	}
	var editedRaw int64
	gdb.Model(&model.SourceCategory{}).Where("source_id = ? AND source_type_id = ?", "src_edit", 1).Count(&editedRaw)
	if editedRaw != 1 {
		t.Fatalf("expected the edited source to keep its own category copy, got %d", editedRaw)
	}
}
