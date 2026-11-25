package catalog

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
)

func seedMergedShowTree(t *testing.T, welfareShow, anchorShow bool) {
	t.Helper()
	gdb := setupCategoryActiveTreeTestDB(t)
	if err := gdb.AutoMigrate(&model.SourceCategory{}, &model.MappingRule{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	categories := []model.Category{
		{Id: 9, Pid: 0, Name: "电影", Show: true, Sort: 1, StableKey: "src1:1"},
		{Id: 547, Pid: 0, Name: "福利视频", Show: welfareShow, Sort: 2, StableKey: "src1:5"},
		{Id: 549, Pid: 0, Name: "网红主播", Show: anchorShow, Sort: 1, StableKey: "src1:18"},
		{Id: 550, Pid: 0, Name: "海外抖音", Show: true, Sort: 2, StableKey: "src1:37"},
	}
	if err := gdb.Create(&categories).Error; err != nil {
		t.Fatalf("create categories: %v", err)
	}
	if !welfareShow {
		if err := gdb.Model(&model.Category{}).Where("id = ?", 547).Update("show", false).Error; err != nil {
			t.Fatalf("hide 福利视频: %v", err)
		}
	}
	if !anchorShow {
		if err := gdb.Model(&model.Category{}).Where("id = ?", 549).Update("show", false).Error; err != nil {
			t.Fatalf("hide 网红主播: %v", err)
		}
	}
	mappings := []model.CategoryMapping{
		{SourceId: "src1", SourceTypeId: 1, CategoryId: 9},
		{SourceId: "src1", SourceTypeId: 5, CategoryId: 547},
		{SourceId: "src1", SourceTypeId: 18, CategoryId: 549},
		{SourceId: "src1", SourceTypeId: 37, CategoryId: 550},
	}
	if err := gdb.Create(&mappings).Error; err != nil {
		t.Fatalf("create mappings: %v", err)
	}
	rows := []model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0},
		{SourceId: "src1", SourceTypeId: 5, ParentSourceTypeId: 0, RawName: "福利视频", Sort: 5, Depth: 0},
		{SourceId: "src1", SourceTypeId: 18, ParentSourceTypeId: 0, RawName: "网红主播", Sort: 6, Depth: 0},
		{SourceId: "src1", SourceTypeId: 37, ParentSourceTypeId: 0, RawName: "海外抖音", Sort: 7, Depth: 0},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}
	if err := gdb.Create(&model.MappingRule{
		SourceId: "src1", Group: "CategoryRoot", Raw: "福利视频|海外抖音|网红主播", Target: "男人帮", MatchType: "regex",
	}).Error; err != nil {
		t.Fatalf("create rule: %v", err)
	}
	ReloadMappingRules()
}

func TestSourceTypeTree_HiddenMergedRootStaysOffNav(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	prev := repobase.CurrentMappingSnapshotForTest()
	t.Cleanup(func() {
		db.Rdb = origRdb
		repobase.StoreMappingSnapshotForTest(prev)
	})
	seedMergedShowTree(t, true, true)

	admin := GetSourceBoundCategoryTree("src1")
	if len(admin.Children) != 2 || admin.Children[0].Name != "电影" || admin.Children[1].Name != "男人帮" {
		t.Fatalf("admin roots = %v, want 电影 then 男人帮", rootNames(admin))
	}
	// 隐藏合成一级父组本身：整个男人帮及其成员对前台均不可见，但子行自身的 Show 保持独立
	admin.Children[1].Show = false
	if err := SaveSourceCategoryOrder("src1", admin.Children); err != nil {
		t.Fatalf("save hidden merged root: %v", err)
	}

	tree, err := GetActiveCategoryTree("src1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Children) != 1 || tree.Children[0].Name != "电影" {
		t.Fatalf("public roots = %v, want only 电影", rootNames(tree))
	}
	for _, id := range []int64{5, 18, 37} {
		if PublicSourceTypeVisible("src1", id) {
			t.Fatalf("hidden 男人帮 type %d still visible", id)
		}
		if got := LookupRootSourceType("src1", id); got != nil {
			t.Fatalf("hidden 男人帮 lookup %d = %+v, want nil", id, got)
		}
		if ids := PublicSourceTypeIDs("src1", "pid", id); len(ids) != 0 {
			t.Fatalf("hidden 男人帮 pid %d = %v, want empty", id, ids)
		}
		if ids := PublicSourceTypeIDs("src1", "cid", id); len(ids) != 0 {
			t.Fatalf("hidden 男人帮 cid %d = %v, want empty", id, ids)
		}
		if name := PublicSourceTypeName("src1", id); name != "" {
			t.Fatalf("hidden 男人帮 name %d = %q, want empty", id, name)
		}
	}
	if nodes := PublicSourceChildNodes("src1", 5); len(nodes) != 0 {
		t.Fatalf("hidden 男人帮 chips = %v", childNames(&model.CategoryTree{Children: nodes}))
	}

	admin = GetSourceBoundCategoryTree("src1")
	if len(admin.Children) != 2 || admin.Children[0].Name != "电影" || !admin.Children[0].Show || admin.Children[1].Name != "男人帮" || admin.Children[1].Show {
		t.Fatalf("admin roots = %v, want 电影 then hidden 男人帮", rootNames(admin))
	}
	if len(admin.Children[1].Children) != 3 {
		t.Fatalf("hidden 男人帮 should still expose all 3 independent children, got %v", childNames(admin.Children[1]))
	}
}

func TestSourceTypeTree_HiddenMergedChildLeavesChips(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	prev := repobase.CurrentMappingSnapshotForTest()
	t.Cleanup(func() {
		db.Rdb = origRdb
		repobase.StoreMappingSnapshotForTest(prev)
	})
	seedMergedShowTree(t, false, false)

	tree, err := GetActiveCategoryTree("src1")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*model.CategoryTree{}
	for _, child := range tree.Children {
		byName[child.Name] = child
	}
	root := byName["男人帮"]
	if root == nil || root.Id < syntheticRootTypeOffset || !root.Show {
		t.Fatalf("roots = %v, want shown synthetic 男人帮", rootNames(tree))
	}
	got := childNames(root)
	if len(root.Children) != 1 || got[0] != "海外抖音#37" {
		t.Fatalf("男人帮 children = %v, want 海外抖音#37", got)
	}
	nodes := PublicSourceChildNodes("src1", root.Id)
	if len(nodes) != 1 || nodes[0].Name != "海外抖音" {
		t.Fatalf("filter children = %v", childNames(&model.CategoryTree{Children: nodes}))
	}
	ids := PublicSourceTypeIDs("src1", "pid", root.Id)
	if len(ids) != 1 || ids[0] != 37 {
		t.Fatalf("男人帮 pid members = %v, want [37]", ids)
	}
	for _, hiddenTypeID := range []int64{5, 18} {
		if PublicSourceTypeVisible("src1", hiddenTypeID) {
			t.Fatalf("hidden child %d still visible", hiddenTypeID)
		}
		if got := LookupRootSourceType("src1", hiddenTypeID); got != nil {
			t.Fatalf("hidden child %d lookup = %+v, want nil", hiddenTypeID, got)
		}
		if got := PublicSourceTypeIDs("src1", "cid", hiddenTypeID); len(got) != 0 {
			t.Fatalf("hidden child %d cid = %v, want empty", hiddenTypeID, got)
		}
		if name := PublicSourceTypeName("src1", hiddenTypeID); name != "" {
			t.Fatalf("hidden child %d name = %q, want empty", hiddenTypeID, name)
		}
		if nodes := PublicSourceChildNodes("src1", hiddenTypeID); len(nodes) != 0 {
			t.Fatalf("hidden child chips via pid %d = %v, want empty", hiddenTypeID, childNames(&model.CategoryTree{Children: nodes}))
		}
	}
	if !PublicSourceTypeVisible("src1", 37) {
		t.Fatal("shown 男人帮 member 37 should stay visible")
	}

	admin := GetSourceBoundCategoryTree("src1")
	var merged *model.CategoryTree
	for _, child := range admin.Children {
		if child.Name == "男人帮" {
			merged = child
			break
		}
	}
	if merged == nil || !merged.Show || merged.Id == 547 || merged.Id == 549 || merged.Id == 550 {
		t.Fatalf("admin 男人帮 = %+v, want dedicated shown row", merged)
	}
	gotAdmin := childNames(merged)
	if len(merged.Children) != 3 || gotAdmin[0] != "网红主播#549" || merged.Children[0].Show || gotAdmin[1] != "福利视频#547" || merged.Children[1].Show || gotAdmin[2] != "海外抖音#550" || !merged.Children[2].Show {
		t.Fatalf("admin 男人帮 children = %v", gotAdmin)
	}
}

func TestSourceTypeTree_PartiallyHiddenCascadingMergedChildStaysVisible(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	prev := repobase.CurrentMappingSnapshotForTest()
	t.Cleanup(func() {
		db.Rdb = origRdb
		repobase.StoreMappingSnapshotForTest(prev)
	})

	gdb := setupCategoryActiveTreeTestDB(t)
	if err := gdb.AutoMigrate(&model.SourceCategory{}, &model.MappingRule{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	categories := []model.Category{
		{Id: 19, Pid: 0, Name: "伦理片", Show: true, Sort: 1, StableKey: "src1:19"},
		{Id: 44, Pid: 0, Name: "预告片", Show: true, Sort: 2, StableKey: "src1:44"},
	}
	if err := gdb.Create(&categories).Error; err != nil {
		t.Fatalf("create categories: %v", err)
	}
	if err := gdb.Model(&model.Category{}).Where("id = ?", 19).Update("show", false).Error; err != nil {
		t.Fatalf("hide 伦理片: %v", err)
	}
	mappings := []model.CategoryMapping{
		{SourceId: "src1", SourceTypeId: 19, CategoryId: 19},
		{SourceId: "src1", SourceTypeId: 44, CategoryId: 44},
	}
	if err := gdb.Create(&mappings).Error; err != nil {
		t.Fatalf("create mappings: %v", err)
	}
	rows := []model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 19, ParentSourceTypeId: 0, RawName: "伦理片", Sort: 1, Depth: 0},
		{SourceId: "src1", SourceTypeId: 44, ParentSourceTypeId: 0, RawName: "预告片", Sort: 2, Depth: 0},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}
	rules := []model.MappingRule{
		{SourceId: "src1", Group: "CategoryRoot", Raw: "片$", Target: "11212121", MatchType: "regex"},
		{SourceId: "src1", Group: "CategorySub", Raw: "片$", Target: "测试", MatchType: "regex"},
	}
	if err := gdb.Create(&rules).Error; err != nil {
		t.Fatalf("create rules: %v", err)
	}
	ReloadMappingRules()

	tree, err := GetActiveCategoryTree("src1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Children) != 1 || tree.Children[0].Name != "11212121" {
		t.Fatalf("public roots = %v, want 11212121", rootNames(tree))
	}
	root := tree.Children[0]
	if len(root.Children) != 1 || root.Children[0].Name != "测试" || root.Children[0].Id != 44 || !root.Children[0].Show {
		t.Fatalf("11212121 children = %v, want shown 测试#44", childNames(root))
	}
	if got := PublicSourceTypeIDs("src1", "pid", root.Id); len(got) != 1 || got[0] != 44 {
		t.Fatalf("pid members = %v, want [44]", got)
	}
	if got := PublicSourceTypeIDs("src1", "cid", root.Children[0].Id); len(got) != 1 || got[0] != 44 {
		t.Fatalf("cid members = %v, want [44]", got)
	}

	admin := GetSourceBoundCategoryTree("src1")
	if len(admin.Children) != 1 || len(admin.Children[0].Children) != 1 {
		t.Fatalf("admin tree = %+v", admin.Children)
	}
	adminChild := admin.Children[0].Children[0]
	if adminChild.Name != "测试" || !adminChild.Show || adminChild.Id != 44 {
		t.Fatalf("admin child = %+v, want shown 测试#44", adminChild)
	}

	if err := SaveSourceCategoryOrder("src1", admin.Children); err != nil {
		t.Fatalf("save order without toggling show: %v", err)
	}
	if got := PublicSourceTypeIDs("src1", "cid", adminChild.Id); len(got) != 1 || got[0] != 44 {
		t.Fatalf("cid members after save = %v, want [44] (19 stays hidden)", got)
	}
}
