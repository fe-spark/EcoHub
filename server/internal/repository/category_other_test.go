package repository

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAppendOtherCategoryPlan_AddsWhenMissing(t *testing.T) {
	plans := []sourceCategoryPlacement{
		{SourceTypeId: 1, Name: "电影", Sort: 1},
		{SourceTypeId: 2, ParentSourceTypeId: 1, Name: "动作片", Sort: 2, Depth: 1},
	}
	got := appendOtherCategoryPlan(plans, nil)
	if len(got) != 3 {
		t.Fatalf("want 3 plans, got %d", len(got))
	}
	last := got[len(got)-1]
	if last.SourceTypeId != model.OtherSourceTypeID || last.Name != model.TagOthersName || last.ParentSourceTypeId != 0 {
		t.Fatalf("other plan mismatch: %+v", last)
	}
	if last.Sort != model.OtherCategorySort {
		t.Fatalf("other sort=%d", last.Sort)
	}
}

func TestAppendOtherCategoryPlan_KeepsExisting其它(t *testing.T) {
	plans := []sourceCategoryPlacement{
		{SourceTypeId: 1, Name: "电影", Sort: 1},
		{SourceTypeId: 20, Name: "其它", Sort: 2},
	}
	got := appendOtherCategoryPlan(plans, nil)
	if len(got) != 2 {
		t.Fatalf("should keep CMS 其它, got %d plans", len(got))
	}
}

func TestAppendOtherCategoryPlan_RekeepsOldOther(t *testing.T) {
	plans := []sourceCategoryPlacement{
		{SourceTypeId: 1, Name: "电影", Sort: 1},
	}
	old := []model.SourceCategory{
		{SourceTypeId: 20, RawName: "其它", ParentSourceTypeId: 0, Sort: 8},
	}
	got := appendOtherCategoryPlan(plans, old)
	if len(got) != 2 {
		t.Fatalf("want old 其它 kept, got %d", len(got))
	}
	last := got[1]
	if last.SourceTypeId != 20 || last.Name != "其它" || last.Sort != 8 {
		t.Fatalf("kept other mismatch: %+v", last)
	}
}

func TestAppendOtherCategoryPlan_OldOtherIdTakenGetsFreshID(t *testing.T) {
	plans := []sourceCategoryPlacement{
		{SourceTypeId: 20, Name: "伦理片", Sort: 1},
	}
	old := []model.SourceCategory{
		{SourceTypeId: 20, RawName: "其它", ParentSourceTypeId: 0, Sort: 8},
	}
	got := appendOtherCategoryPlan(plans, old)
	if len(got) != 2 {
		t.Fatalf("want 伦理片 + 其他, got %d: %+v", len(got), got)
	}
	if got[0].SourceTypeId != 20 || got[0].Name != "伦理片" {
		t.Fatalf("cms row: %+v", got[0])
	}
	if got[1].SourceTypeId != model.OtherSourceTypeID || got[1].Name != model.TagOthersName || got[1].ParentSourceTypeId != 0 {
		t.Fatalf("catch-all: %+v", got[1])
	}
}

func TestAppendOtherCategoryPlan_SyntheticIdTakenGetsNext(t *testing.T) {
	plans := []sourceCategoryPlacement{
		{SourceTypeId: model.OtherSourceTypeID, Name: "专题", Sort: 1},
	}
	old := []model.SourceCategory{
		{SourceTypeId: model.OtherSourceTypeID, RawName: "其他", ParentSourceTypeId: 0, Sort: 8},
	}
	got := appendOtherCategoryPlan(plans, old)
	if len(got) != 2 {
		t.Fatalf("want 专题 + 其他, got %d: %+v", len(got), got)
	}
	if got[0].SourceTypeId != model.OtherSourceTypeID || got[0].Name != "专题" {
		t.Fatalf("cms row: %+v", got[0])
	}
	if got[1].SourceTypeId != model.OtherSourceTypeID+1 || got[1].Name != model.TagOthersName {
		t.Fatalf("catch-all: %+v", got[1])
	}
}

func TestAppendOtherCategoryPlan_EmptyStaysEmpty(t *testing.T) {
	if got := appendOtherCategoryPlan(nil, nil); len(got) != 0 {
		t.Fatalf("empty plans must stay empty, got %d", len(got))
	}
}

func TestSaveCategoryTree_InjectsOtherCategory(t *testing.T) {
	gdb := openCategoryOtherTestDB(t)
	tree := &model.CategoryTree{
		Children: []*model.CategoryTree{{Id: 1, Name: "电影", Show: true}},
	}
	if err := SaveCategoryTree("src_other", tree); err != nil {
		t.Fatalf("SaveCategoryTree: %v", err)
	}
	var other model.SourceCategory
	if err := gdb.Where("source_id = ? AND source_type_id = ?", "src_other", model.OtherSourceTypeID).First(&other).Error; err != nil {
		t.Fatalf("injected 其他 missing: %v", err)
	}
	if other.RawName != model.TagOthersName || other.ParentSourceTypeId != 0 {
		t.Fatalf("injected 其他 mismatch: %+v", other)
	}
	var mapping model.CategoryMapping
	if err := gdb.Where("source_id = ? AND source_type_id = ?", "src_other", model.OtherSourceTypeID).First(&mapping).Error; err != nil {
		t.Fatalf("其他 mapping missing: %v", err)
	}
	tree2, err := GetActiveCategoryTree("src_other")
	if err != nil {
		t.Fatalf("GetActiveCategoryTree: %v", err)
	}
	if len(tree2.Children) != 2 {
		t.Fatalf("public tree want 2 roots, got %d", len(tree2.Children))
	}
	last := tree2.Children[len(tree2.Children)-1]
	if last == nil || last.Name != model.TagOthersName || last.Id != model.OtherSourceTypeID {
		t.Fatalf("public 其他 mismatch: %+v", last)
	}
}

func TestSaveCategoryTree_Renamed其它GetsFreshOther(t *testing.T) {
	gdb := openCategoryOtherTestDB(t)
	oldTree := &model.CategoryTree{
		Children: []*model.CategoryTree{{Id: 20, Name: "其它", Show: true}},
	}
	if err := SaveCategoryTree("src_rename", oldTree); err != nil {
		t.Fatalf("initial: %v", err)
	}
	newTree := &model.CategoryTree{
		Children: []*model.CategoryTree{{Id: 20, Name: "伦理片", Show: true}},
	}
	if err := SaveCategoryTree("src_rename", newTree); err != nil {
		t.Fatalf("save after rename: %v", err)
	}
	var rows []model.SourceCategory
	if err := gdb.Where("source_id = ?", "src_rename").Order("source_type_id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %+v", rows)
	}
	if rows[0].SourceTypeId != 20 || rows[0].RawName != "伦理片" {
		t.Fatalf("cms: %+v", rows[0])
	}
	if rows[1].SourceTypeId != model.OtherSourceTypeID || rows[1].RawName != model.TagOthersName {
		t.Fatalf("other: %+v", rows[1])
	}
}

func TestSaveCategoryTree_DoesNotDuplicate其它(t *testing.T) {
	gdb := openCategoryOtherTestDB(t)
	tree := &model.CategoryTree{
		Children: []*model.CategoryTree{
			{Id: 1, Name: "电影", Show: true},
			{Id: 20, Name: "其它", Show: true},
		},
	}
	if err := SaveCategoryTree("src_alias", tree); err != nil {
		t.Fatalf("SaveCategoryTree: %v", err)
	}
	var n int64
	gdb.Model(&model.SourceCategory{}).Where("source_id = ?", "src_alias").Count(&n)
	if n != 2 {
		t.Fatalf("want 2 source categories, got %d", n)
	}
	var synthetic int64
	gdb.Model(&model.SourceCategory{}).Where("source_id = ? AND source_type_id = ?", "src_alias", model.OtherSourceTypeID).Count(&synthetic)
	if synthetic != 0 {
		t.Fatal("should not inject 900000 when CMS already has 其它")
	}
}

func openCategoryOtherTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Category{}, &model.CategoryMapping{}, &model.SourceCategory{}, &model.MappingRule{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	db.Rdb = nil
	t.Cleanup(func() {
		db.Mdb = prev
		support.StoreMappingSnapshotForTest(nil)
	})
	return gdb
}
