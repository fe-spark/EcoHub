package repository

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupCategoryActiveTreeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	// 故意不创建 FilmListSnapshot 和 FilmIndex 表，证明请求路径绝对不依赖大表做 DISTINCT 查询
	if err := gdb.AutoMigrate(
		&model.Category{},
		&model.CategoryMapping{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	db.Mdb = gdb
	return gdb
}

func TestLoadActiveCategoryIDsFromCurrentMappings_Fallback(t *testing.T) {
	gdb := setupCategoryActiveTreeTestDB(t)

	// 插入 mappings
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 1, CategoryId: 10})
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 2, CategoryId: 20})
	gdb.Create(&model.CategoryMapping{SourceId: "src2", SourceTypeId: 3, CategoryId: 0}) // 过滤 0

	// 执行 loadActiveCategoryIDsFromCurrentMappings
	// 期望直接从 category_mappings 查出 category_id > 0，不报错且不触发 snapshot 表 DISTINCT
	activeMap := loadActiveCategoryIDsFromCurrentMappings()
	if !activeMap[10] || !activeMap[20] {
		t.Fatalf("expected category IDs 10 and 20 to be active, got: %+v", activeMap)
	}
	if activeMap[0] {
		t.Fatalf("expected category ID 0 not to be active")
	}
	if len(activeMap) != 2 {
		t.Fatalf("expected exactly 2 active categories, got %d", len(activeMap))
	}
}

func TestIsValidActiveCategoryTree(t *testing.T) {
	gdb := setupCategoryActiveTreeTestDB(t)

	// 准备分类：根分类 1，子分类 10
	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", Show: true})
	gdb.Create(&model.Category{Id: 10, Pid: 1, Name: "动作片", Show: true})
	support.RefreshCategoryCache()

	// 1. 合法树：子节点 Pid 为 0 且属于根分类
	validTree := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息",
		Children: []*model.CategoryTree{
			{Id: 1, Pid: 0, Name: "电影"},
		},
	}
	if !isValidActiveCategoryTree(validTree) {
		t.Fatal("expected validTree to be valid")
	}

	// 2. 非法树：包含 nil 子节点
	nilChildTree := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息",
		Children: []*model.CategoryTree{nil},
	}
	if isValidActiveCategoryTree(nilChildTree) {
		t.Fatal("expected tree with nil child to be invalid")
	}

	// 3. 非法树：子节点 Pid != 0
	nonZeroPidChildTree := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息",
		Children: []*model.CategoryTree{
			{Id: 10, Pid: 1, Name: "动作片"},
		},
	}
	if isValidActiveCategoryTree(nonZeroPidChildTree) {
		t.Fatal("expected tree with child.Pid != 0 to be invalid")
	}

	// 4. 非法树：子节点的 Id 在分类体系中不是根分类 (例如 Id: 10)
	notRootCategoryTree := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息",
		Children: []*model.CategoryTree{
			{Id: 10, Pid: 0, Name: "假冒根分类"},
		},
	}
	if isValidActiveCategoryTree(notRootCategoryTree) {
		t.Fatal("expected tree with non-root category child to be invalid")
	}
}

func TestRepository_RedisNilAndSQLiteDialect(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	gdb := setupCategoryActiveTreeTestDB(t)
	if err := gdb.AutoMigrate(&model.SiteConfigRecord{}, &model.MappingRule{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// 1. GetActiveCategoryTree with db.Rdb == nil
	tree := GetActiveCategoryTree()
	if tree.Name != "分类信息" {
		t.Fatalf("expected 分类信息, got %s", tree.Name)
	}

	// 2. clearProvideListCache with db.Rdb == nil
	clearProvideListCache()

	// 3. TouchRuleVersion & GetRuleVersion with db.Rdb == nil
	support.TouchRuleVersion()
	rv := support.GetRuleVersion()
	if rv == "" {
		t.Fatalf("expected non-empty rule version when Redis is nil")
	}

	// 4. SaveSiteBasic with db.Rdb == nil
	cfg := model.BasicConfig{SiteName: "TestSite"}
	if err := SaveSiteBasic(cfg); err != nil {
		t.Fatalf("SaveSiteBasic failed with nil Redis: %v", err)
	}
	gotCfg := GetSiteBasic()
	if gotCfg.SiteName != "TestSite" {
		t.Fatalf("expected TestSite, got %s", gotCfg.SiteName)
	}

	// 5. EnsureMappingRuleIndexes and ResetMappingRules on SQLite
	if err := EnsureMappingRuleIndexes(); err != nil {
		t.Fatalf("EnsureMappingRuleIndexes failed on SQLite: %v", err)
	}
	gdb.Create(&model.MappingRule{Group: "Area", Raw: "港台", Target: "中国香港"})
	var ruleCount int64
	gdb.Model(&model.MappingRule{}).Count(&ruleCount)
	if ruleCount != 1 {
		t.Fatalf("expected 1 rule before reset, got %d", ruleCount)
	}
	if err := ResetMappingRules(); err != nil {
		t.Fatalf("ResetMappingRules failed on SQLite: %v", err)
	}
	gdb.Model(&model.MappingRule{}).Count(&ruleCount)
	if ruleCount != 0 {
		t.Fatalf("expected 0 rules after ResetMappingRules, got %d", ruleCount)
	}
}
