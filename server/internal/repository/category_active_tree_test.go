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

func TestFilterShownCategoryIDs_DynamicHidden(t *testing.T) {
	gdb := setupCategoryActiveTreeTestDB(t)

	// 模拟分类管理：电影(1, 显示), 电视剧(2, 显示), 体育(4, 隐藏)
	if err := gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", StableKey: "movie", Show: true}).Error; err != nil {
		t.Fatalf("create cat 1: %v", err)
	}
	if err := gdb.Create(&model.Category{Id: 2, Pid: 0, Name: "电视剧", StableKey: "tv", Show: true}).Error; err != nil {
		t.Fatalf("create cat 2: %v", err)
	}
	if err := gdb.Create(&model.Category{Id: 4, Pid: 0, Name: "体育", StableKey: "sports"}).Error; err != nil {
		t.Fatalf("create cat 4: %v", err)
	}
	// 模拟在分类管理中将分类 4 设置为不显示
	if err := gdb.Model(&model.Category{}).Where("id = ?", 4).Update("show", false).Error; err != nil {
		t.Fatalf("update cat 4 to show=false: %v", err)
	}

	shownIDs := GetShownRootCategoryIDs()
	if len(shownIDs) != 2 || shownIDs[0] != 1 || shownIDs[1] != 2 {
		t.Fatalf("expected shown IDs [1, 2], got %v", shownIDs)
	}

	// 哪怕之前选中的时候显示 [1, 2, 4]，后面分类 4 设置为不显示，依旧严格过滤出 [1, 2]
	filtered := FilterShownCategoryIDs([]int64{1, 2, 4})
	if len(filtered) != 2 || filtered[0] != 1 || filtered[1] != 2 {
		t.Fatalf("expected filtered IDs [1, 2], got %v", filtered)
	}

	// 如果只选了被隐藏的分类 4，过滤后应为空
	onlyHidden := FilterShownCategoryIDs([]int64{4})
	if len(onlyHidden) != 0 {
		t.Fatalf("expected empty slice for only hidden categories, got %v", onlyHidden)
	}

	// 验证 NormalizeBannerConfig 也会自动过滤掉被隐藏的分类 4
	cfg := NormalizeBannerConfig(model.BannerConfig{Categories: []int64{1, 2, 4}})
	if len(cfg.Categories) != 2 || cfg.Categories[0] != 1 || cfg.Categories[1] != 2 {
		t.Fatalf("expected NormalizeBannerConfig to filter out hidden category 4, got %v", cfg.Categories)
	}
}

func TestFilterShownCategoryIDs_AllHidden(t *testing.T) {
	gdb := setupCategoryActiveTreeTestDB(t)

	// 插入分类且全部置为隐藏 (Show=false)
	gdb.Create(&model.Category{Id: 10, Pid: 0, Name: "分类A", StableKey: "a"})
	gdb.Create(&model.Category{Id: 20, Pid: 0, Name: "分类B", StableKey: "b"})
	if err := gdb.Model(&model.Category{}).Where("id IN ?", []int64{10, 20}).Update("show", false).Error; err != nil {
		t.Fatalf("update to show=false: %v", err)
	}

	shownIDs := GetShownRootCategoryIDs()
	if len(shownIDs) != 0 {
		t.Fatalf("expected 0 shown IDs when all categories are hidden, got %v", shownIDs)
	}

	// 验证当全部分类都隐藏时，输入任何分类 ID 都必须严格被过滤为 0 个，绝不能短路放行
	filtered := FilterShownCategoryIDs([]int64{10, 20})
	if len(filtered) != 0 {
		t.Fatalf("expected empty slice when all categories hidden, got %v", filtered)
	}

	// 验证 NormalizeBannerConfig 在全隐藏状态下 Categories 被彻底清空
	cfg := NormalizeBannerConfig(model.BannerConfig{Categories: []int64{10, 20}})
	if len(cfg.Categories) != 0 {
		t.Fatalf("expected empty categories in NormalizeBannerConfig when all hidden, got %v", cfg.Categories)
	}
}

func TestGetActiveCategoryTree_SourceScoped(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	gdb := setupCategoryActiveTreeTestDB(t)
	if err := gdb.AutoMigrate(&model.SourceCategory{}); err != nil {
		t.Fatalf("migrate source category: %v", err)
	}
	rows := []model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0},
		{SourceId: "src1", SourceTypeId: 10, ParentSourceTypeId: 1, RawName: "动作片", Sort: 1, Depth: 1},
		{SourceId: "src2", SourceTypeId: 2, ParentSourceTypeId: 0, RawName: "电视剧", Sort: 1, Depth: 0},
		{SourceId: "src2", SourceTypeId: 20, ParentSourceTypeId: 2, RawName: "国产剧", Sort: 1, Depth: 1},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}

	tree1 := GetActiveCategoryTree("src1")
	if len(tree1.Children) != 1 || tree1.Children[0].Id != 1 || tree1.Children[0].Name != "电影" {
		t.Fatalf("expected src1 root type 1 电影, got: %+v", tree1.Children)
	}
	if len(tree1.Children[0].Children) != 1 || tree1.Children[0].Children[0].Id != 10 {
		t.Fatalf("expected src1 child type 10, got: %+v", tree1.Children[0].Children)
	}

	tree2 := GetActiveCategoryTree("src2")
	if len(tree2.Children) != 1 || tree2.Children[0].Id != 2 || tree2.Children[0].Name != "电视剧" {
		t.Fatalf("expected src2 root type 2 电视剧, got: %+v", tree2.Children)
	}
	if len(tree2.Children[0].Children) != 1 || tree2.Children[0].Children[0].Id != 20 {
		t.Fatalf("expected src2 child type 20, got: %+v", tree2.Children[0].Children)
	}
}

func TestSourceTypeTree_UsesMappedShowFlag(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	gdb := setupCategoryActiveTreeTestDB(t)
	if err := gdb.AutoMigrate(&model.SourceCategory{}); err != nil {
		t.Fatalf("migrate source category: %v", err)
	}
	categories := []model.Category{
		{Id: 1, Pid: 0, Name: "电影", Show: true, StableKey: "source:src1:1"},
		{Id: 17, Pid: 0, Name: "公告", Show: false, StableKey: "source:src1:17"},
		{Id: 18, Pid: 0, Name: "头条", Show: false, StableKey: "source:src1:18"},
	}
	if err := gdb.Create(&categories).Error; err != nil {
		t.Fatalf("create categories: %v", err)
	}
	if err := gdb.Model(&model.Category{}).Where("id IN ?", []int64{17, 18}).Update("show", false).Error; err != nil {
		t.Fatalf("hide categories: %v", err)
	}
	mappings := []model.CategoryMapping{
		{SourceId: "src1", SourceTypeId: 1, CategoryId: 1},
		{SourceId: "src1", SourceTypeId: 17, CategoryId: 17},
		{SourceId: "src1", SourceTypeId: 18, CategoryId: 18},
	}
	if err := gdb.Create(&mappings).Error; err != nil {
		t.Fatalf("create mappings: %v", err)
	}
	rows := []model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0},
		{SourceId: "src1", SourceTypeId: 17, ParentSourceTypeId: 0, RawName: "公告", Sort: 2, Depth: 0},
		{SourceId: "src1", SourceTypeId: 18, ParentSourceTypeId: 0, RawName: "头条", Sort: 3, Depth: 0},
		{SourceId: "src1", SourceTypeId: 21, ParentSourceTypeId: 0, RawName: "纪录片", Sort: 4, Depth: 0},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}

	tree := GetActiveCategoryTree("src1")
	shown := map[int64]bool{}
	for _, child := range tree.Children {
		shown[child.Id] = child.Show
	}
	if shown[1] != true || shown[21] != true || len(tree.Children) != 2 {
		t.Fatalf("visible roots = %+v, want 电影 and 纪录片", shown)
	}
	for _, id := range []int64{17, 18} {
		got := LookupRootSourceType("src1", id)
		if got == nil || got.Show {
			t.Fatalf("hidden type %d lookup = %+v", id, got)
		}
	}
}

func TestSourceTypeTree_AppliesPreferredStationRules(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	prev := support.CurrentMappingSnapshotForTest()
	t.Cleanup(func() {
		db.Rdb = origRdb
		support.StoreMappingSnapshotForTest(prev)
	})

	gdb := setupCategoryActiveTreeTestDB(t)
	if err := gdb.AutoMigrate(&model.SourceCategory{}, &model.MappingRule{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	categories := []model.Category{
		{Id: 1, Pid: 0, Name: "电影", Show: true, StableKey: "source:src1:1"},
		{Id: 17, Pid: 0, Name: "公告", Show: true, StableKey: "source:src1:17"},
		{Id: 21, Pid: 0, Name: "纪录片", Show: true, StableKey: "source:src1:21"},
		{Id: 30, Pid: 0, Name: "动画片", Show: true, StableKey: "source:src1:30"},
	}
	if err := gdb.Create(&categories).Error; err != nil {
		t.Fatalf("create categories: %v", err)
	}
	if err := gdb.Model(&model.Category{}).Where("id = ?", 17).Update("show", false).Error; err != nil {
		t.Fatalf("hide 公告: %v", err)
	}
	mappings := []model.CategoryMapping{
		{SourceId: "src1", SourceTypeId: 1, CategoryId: 1},
		{SourceId: "src1", SourceTypeId: 17, CategoryId: 17},
		{SourceId: "src1", SourceTypeId: 21, CategoryId: 21},
		{SourceId: "src1", SourceTypeId: 30, CategoryId: 30},
	}
	if err := gdb.Create(&mappings).Error; err != nil {
		t.Fatalf("create mappings: %v", err)
	}
	rows := []model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0},
		{SourceId: "src1", SourceTypeId: 11, ParentSourceTypeId: 1, RawName: "动作片", Sort: 1, Depth: 1},
		{SourceId: "src1", SourceTypeId: 12, ParentSourceTypeId: 1, RawName: "喜剧片", Sort: 2, Depth: 1},
		{SourceId: "src1", SourceTypeId: 13, ParentSourceTypeId: 1, RawName: "剧情", Sort: 3, Depth: 1},
		{SourceId: "src1", SourceTypeId: 21, ParentSourceTypeId: 0, RawName: "纪录片", Sort: 2, Depth: 0},
		{SourceId: "src1", SourceTypeId: 30, ParentSourceTypeId: 0, RawName: "动画片", Sort: 3, Depth: 0},
		{SourceId: "src1", SourceTypeId: 17, ParentSourceTypeId: 0, RawName: "公告", Sort: 4, Depth: 0},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}
	rules := []model.MappingRule{
		{Group: "CategoryRoot", Raw: "片$", Target: "测试", MatchType: "regex"},
		{Group: "CategorySub", Raw: "片$", Target: "影片", MatchType: "regex"},
	}
	if err := gdb.Create(&rules).Error; err != nil {
		t.Fatalf("create rules: %v", err)
	}
	ReloadMappingRules()

	tree := GetActiveCategoryTree("src1")
	names := make([]string, 0, len(tree.Children))
	byName := map[string]*model.CategoryTree{}
	for _, child := range tree.Children {
		names = append(names, child.Name)
		byName[child.Name] = child
	}
	if len(names) != 2 || byName["电影"] == nil || byName["测试"] == nil || byName["测试"].Id != 21 {
		t.Fatalf("public roots = %v, want 电影 and 测试#21", names)
	}
	movieChildren := make([]string, 0)
	for _, child := range byName["电影"].Children {
		movieChildren = append(movieChildren, child.Name)
	}
	if len(byName["电影"].Children) != 2 || byName["电影"].Children[0].Name != "影片" || byName["电影"].Children[0].Id != 11 || byName["电影"].Children[1].Name != "剧情" {
		t.Fatalf("电影 children = %v, want 影片#11 and 剧情", movieChildren)
	}

	merged := LookupRootSourceType("src1", 30)
	if merged == nil || merged.Id != 21 || merged.Name != "测试" {
		t.Fatalf("member 30 lookup = %+v, want 测试#21", merged)
	}
	hidden := LookupRootSourceType("src1", 17)
	if hidden == nil || hidden.Name != "公告" || hidden.Show {
		t.Fatalf("hidden 公告 lookup = %+v", hidden)
	}
	rootIDs := PublicSourceTypeIDs("src1", "pid", 21)
	if len(rootIDs) != 2 || rootIDs[0] != 21 || rootIDs[1] != 30 {
		t.Fatalf("测试 members = %v, want 21 and 30", rootIDs)
	}
	if ids := PublicSourceTypeIDs("src1", "pid", 17); len(ids) != 1 || ids[0] != 17 {
		t.Fatalf("hidden members = %v, want only 17", ids)
	}
	childIDs := PublicSourceTypeIDs("src1", "cid", 12)
	if len(childIDs) != 2 || childIDs[0] != 11 || childIDs[1] != 12 {
		t.Fatalf("影片 members = %v, want 11 and 12", childIDs)
	}
}
