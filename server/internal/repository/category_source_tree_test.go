package repository

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func seedSourceBoundTree(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "电影", StableKey: "a-movie", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 10, Pid: 9, Name: "动作片", StableKey: "a-action", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 11, Pid: 9, Name: "喜剧片", StableKey: "a-comedy", Show: true, Sort: 2})
	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电视剧", StableKey: "b-tv", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 6, CategoryId: 10})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 7, CategoryId: 11})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 2, CategoryId: 1})
	gdb.Create(&[]model.SourceCategory{
		{SourceId: "src_a", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0},
		{SourceId: "src_a", SourceTypeId: 6, ParentSourceTypeId: 1, RawName: "动作片", Sort: 1, Depth: 1},
		{SourceId: "src_a", SourceTypeId: 7, ParentSourceTypeId: 1, RawName: "喜剧片", Sort: 2, Depth: 1},
		{SourceId: "src_b", SourceTypeId: 2, ParentSourceTypeId: 0, RawName: "电视剧", Sort: 1, Depth: 0},
	})
}

func TestGetSourceBoundCategoryTree_OnlyThatSource(t *testing.T) {
	prev := support.CurrentMappingSnapshotForTest()
	t.Cleanup(func() { support.StoreMappingSnapshotForTest(prev) })

	gdb := setupCategoryActiveTreeTestDB(t)
	if err := gdb.AutoMigrate(&model.SourceCategory{}, &model.MappingRule{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seedSourceBoundTree(t, gdb)
	ReloadMappingRules()

	tree := GetSourceBoundCategoryTree("src_a")
	if len(tree.Children) != 1 || tree.Children[0].Id != 9 || tree.Children[0].Name != "电影" {
		t.Fatalf("src_a root = %+v", tree.Children)
	}
	if len(tree.Children[0].Children) != 2 {
		t.Fatalf("src_a children = %+v", tree.Children[0].Children)
	}
	other := GetSourceBoundCategoryTree("src_b")
	if len(other.Children) != 1 || other.Children[0].Id != 1 {
		t.Fatalf("src_b root = %+v", other.Children)
	}
}

func TestGetSourceBoundCategoryTree_AppliesCategoryRules(t *testing.T) {
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
		{Id: 9, Pid: 0, Name: "电影", StableKey: "a-movie", Show: true, Sort: 1},
		{Id: 10, Pid: 9, Name: "动作片", StableKey: "a-action", Show: true, Sort: 1},
		{Id: 11, Pid: 9, Name: "喜剧片", StableKey: "a-comedy", Show: true, Sort: 2},
		{Id: 547, Pid: 0, Name: "福利视频", StableKey: "a-welfare", Show: true, Sort: 5},
		{Id: 549, Pid: 0, Name: "网红主播", StableKey: "a-anchor", Show: true, Sort: 6},
		{Id: 550, Pid: 0, Name: "海外抖音", StableKey: "a-douyin", Show: true, Sort: 7},
		{Id: 548, Pid: 0, Name: "三级伦理", StableKey: "a-ethic", Show: true, Sort: 8},
		{Id: 560, Pid: 548, Name: "韩国伦理", StableKey: "a-kr", Show: true, Sort: 1},
	}
	if err := gdb.Create(&categories).Error; err != nil {
		t.Fatalf("create categories: %v", err)
	}
	mappings := []model.CategoryMapping{
		{SourceId: "src_a", SourceTypeId: 1, CategoryId: 9},
		{SourceId: "src_a", SourceTypeId: 6, CategoryId: 10},
		{SourceId: "src_a", SourceTypeId: 7, CategoryId: 11},
		{SourceId: "src_a", SourceTypeId: 5, CategoryId: 547},
		{SourceId: "src_a", SourceTypeId: 18, CategoryId: 549},
		{SourceId: "src_a", SourceTypeId: 37, CategoryId: 550},
		{SourceId: "src_a", SourceTypeId: 17, CategoryId: 548},
		{SourceId: "src_a", SourceTypeId: 19, CategoryId: 560},
	}
	if err := gdb.Create(&mappings).Error; err != nil {
		t.Fatalf("create mappings: %v", err)
	}
	rows := []model.SourceCategory{
		{SourceId: "src_a", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0},
		{SourceId: "src_a", SourceTypeId: 6, ParentSourceTypeId: 1, RawName: "动作片", Sort: 1, Depth: 1},
		{SourceId: "src_a", SourceTypeId: 7, ParentSourceTypeId: 1, RawName: "喜剧片", Sort: 2, Depth: 1},
		{SourceId: "src_a", SourceTypeId: 5, ParentSourceTypeId: 0, RawName: "福利视频", Sort: 5, Depth: 0},
		{SourceId: "src_a", SourceTypeId: 18, ParentSourceTypeId: 0, RawName: "网红主播", Sort: 6, Depth: 0},
		{SourceId: "src_a", SourceTypeId: 37, ParentSourceTypeId: 0, RawName: "海外抖音", Sort: 7, Depth: 0},
		{SourceId: "src_a", SourceTypeId: 17, ParentSourceTypeId: 0, RawName: "三级伦理", Sort: 8, Depth: 0},
		{SourceId: "src_a", SourceTypeId: 19, ParentSourceTypeId: 17, RawName: "韩国伦理", Sort: 1, Depth: 1},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}
	rules := []model.MappingRule{
		{SourceId: "src_a", Group: "CategoryRoot", Raw: "伦理$", Target: "伦理剧", MatchType: "regex"},
		{SourceId: "src_a", Group: "CategoryRoot", Raw: "福利视频|海外抖音|网红主播", Target: "男人帮", MatchType: "regex"},
		{SourceId: "src_a", Group: "CategorySub", Raw: "片$", Target: "动作", MatchType: "regex"},
	}
	if err := gdb.Create(&rules).Error; err != nil {
		t.Fatalf("create rules: %v", err)
	}
	ReloadMappingRules()

	tree := GetSourceBoundCategoryTree("src_a")
	byName := map[string]*model.CategoryTree{}
	for _, child := range tree.Children {
		byName[child.Name] = child
	}
	if byName["男人帮"] == nil || byName["男人帮"].Id != 547 || byName["福利视频"] != nil || byName["网红主播"] != nil {
		t.Fatalf("roots = %v, want 男人帮#547", rootNames(tree))
	}
	if byName["伦理剧"] == nil || byName["伦理剧"].Id != 548 || byName["三级伦理"] != nil {
		t.Fatalf("roots = %v, want 伦理剧#548", rootNames(tree))
	}
	if byName["电影"] == nil || byName["电影"].Id != 9 {
		t.Fatalf("roots = %v, want 电影#9", rootNames(tree))
	}
	got := childNames(byName["男人帮"])
	if len(byName["男人帮"].Children) != 2 || got[0] != "网红主播#549" || got[1] != "海外抖音#550" {
		t.Fatalf("男人帮 children = %v, want 网红主播#549 海外抖音#550", got)
	}
	ethic := childNames(byName["伦理剧"])
	if len(byName["伦理剧"].Children) != 1 || ethic[0] != "韩国伦理#560" {
		t.Fatalf("伦理剧 children = %v, want 韩国伦理#560", ethic)
	}
	movieKids := byName["电影"].Children
	if len(movieKids) != 1 || movieKids[0].Name != "动作" || movieKids[0].Id != 10 {
		t.Fatalf("电影 children = %v, want 动作#10", childNames(byName["电影"]))
	}
	if len(movieKids[0].MemberIds) != 2 || movieKids[0].MemberIds[0] != 10 || movieKids[0].MemberIds[1] != 11 {
		t.Fatalf("动作 memberIds = %v, want [10 11]", movieKids[0].MemberIds)
	}

	byName["男人帮"].Show = false
	movieKids[0].Show = false
	if err := SaveSourceCategoryOrder("src_a", tree.Children); err != nil {
		t.Fatalf("save grouped tree: %v", err)
	}
	var welfare, anchor, action, comedy model.Category
	gdb.First(&welfare, 547)
	gdb.First(&anchor, 549)
	gdb.First(&action, 10)
	gdb.First(&comedy, 11)
	if welfare.Show {
		t.Fatalf("男人帮 representative should be hidden")
	}
	if !anchor.Show {
		t.Fatalf("网红主播 stays independently visible")
	}
	if action.Show || comedy.Show {
		t.Fatalf("merged 动作 members show action=%v comedy=%v", action.Show, comedy.Show)
	}
	if welfare.Pid != 0 || anchor.Pid != 0 {
		t.Fatalf("save must not rewrite pid, welfare=%d anchor=%d", welfare.Pid, anchor.Pid)
	}
}

func TestSaveSourceCategoryOrder_KeepsOtherSource(t *testing.T) {
	gdb := setupCategoryActiveTreeTestDB(t)
	origRdb := db.Rdb
	db.Rdb = nil
	t.Cleanup(func() { db.Rdb = origRdb })

	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "电影", StableKey: "a-movie", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 10, Pid: 9, Name: "动作片", StableKey: "a-action", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 11, Pid: 9, Name: "喜剧片", StableKey: "a-comedy", Show: true, Sort: 2})
	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电视剧", StableKey: "b-tv", Show: true, Sort: 3})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 6, CategoryId: 10})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 7, CategoryId: 11})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 2, CategoryId: 1})

	nodes := []*model.CategoryTree{
		{
			Id:   9,
			Name: "电影",
			Show: true,
			Children: []*model.CategoryTree{
				{Id: 11, Name: "喜剧片", Show: false},
				{Id: 10, Name: "动作片", Show: true},
			},
		},
	}
	if err := SaveSourceCategoryOrder("src_a", nodes); err != nil {
		t.Fatalf("save: %v", err)
	}
	var movie, action, comedy, other model.Category
	gdb.First(&movie, 9)
	gdb.First(&action, 10)
	gdb.First(&comedy, 11)
	gdb.First(&other, 1)
	if comedy.Sort != 1 || action.Sort != 2 {
		t.Fatalf("child sort comedy=%d action=%d", comedy.Sort, action.Sort)
	}
	if other.Sort != 3 {
		t.Fatalf("other source sort changed: %d", other.Sort)
	}
	if !movie.Show || !action.Show || comedy.Show || !other.Show {
		t.Fatalf("show movie=%v action=%v comedy=%v other=%v", movie.Show, action.Show, comedy.Show, other.Show)
	}
	if err := SaveSourceCategoryOrder("src_a", []*model.CategoryTree{{Id: 1, Name: "电视剧"}}); err == nil {
		t.Fatal("category from another source should be rejected")
	}
}
