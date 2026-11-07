package repository

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
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
	prev := support.CurrentMappingSnapshotForTest()
	t.Cleanup(func() {
		db.Rdb = origRdb
		support.StoreMappingSnapshotForTest(prev)
	})
	seedMergedShowTree(t, false, true)

	tree, err := GetActiveCategoryTree("src1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Children) != 1 || tree.Children[0].Name != "电影" {
		t.Fatalf("public roots = %v, want only 电影", rootNames(tree))
	}
	for _, id := range []int64{5, 18, 37} {
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

	admin := GetSourceBoundCategoryTree("src1")
	if len(admin.Children) != 2 || admin.Children[0].Name != "电影" || !admin.Children[0].Show || admin.Children[1].Name != "男人帮" || admin.Children[1].Show {
		t.Fatalf("admin roots = %v, want 电影 then hidden 男人帮", rootNames(admin))
	}
}

func TestSourceTypeTree_HiddenMergedChildLeavesChips(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	prev := support.CurrentMappingSnapshotForTest()
	t.Cleanup(func() {
		db.Rdb = origRdb
		support.StoreMappingSnapshotForTest(prev)
	})
	seedMergedShowTree(t, true, false)

	tree, err := GetActiveCategoryTree("src1")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*model.CategoryTree{}
	for _, child := range tree.Children {
		byName[child.Name] = child
	}
	root := byName["男人帮"]
	if root == nil || root.Id != 5 || !root.Show {
		t.Fatalf("roots = %v, want shown 男人帮#5", rootNames(tree))
	}
	got := childNames(root)
	if len(root.Children) != 2 || got[0] != "福利视频#5" || got[1] != "海外抖音#37" {
		t.Fatalf("男人帮 children = %v, want 福利视频#5 海外抖音#37", got)
	}
	nodes := PublicSourceChildNodes("src1", 5)
	if len(nodes) != 2 || nodes[0].Name != "福利视频" || nodes[1].Name != "海外抖音" {
		t.Fatalf("filter children = %v", childNames(&model.CategoryTree{Children: nodes}))
	}
	ids := PublicSourceTypeIDs("src1", "pid", 5)
	if len(ids) != 2 || ids[0] != 5 || ids[1] != 37 {
		t.Fatalf("男人帮 pid members = %v, want [5 37]", ids)
	}
	if got := LookupRootSourceType("src1", 18); got != nil {
		t.Fatalf("hidden 网红主播 lookup = %+v, want nil", got)
	}
	if got := PublicSourceTypeIDs("src1", "cid", 18); len(got) != 0 {
		t.Fatalf("hidden 网红主播 cid = %v, want empty", got)
	}
	if name := PublicSourceTypeName("src1", 18); name != "" {
		t.Fatalf("hidden 网红主播 name = %q, want empty", name)
	}
	if nodes := PublicSourceChildNodes("src1", 18); len(nodes) != 0 {
		t.Fatalf("hidden 网红主播 chips via pid 18 = %v, want empty", childNames(&model.CategoryTree{Children: nodes}))
	}

	admin := GetSourceBoundCategoryTree("src1")
	var merged *model.CategoryTree
	for _, child := range admin.Children {
		if child.Name == "男人帮" {
			merged = child
			break
		}
	}
	if merged == nil || !merged.Show {
		t.Fatalf("admin 男人帮 = %+v, want shown", merged)
	}
	gotAdmin := childNames(merged)
	if len(merged.Children) != 2 || gotAdmin[0] != "网红主播#549" || merged.Children[0].Show || gotAdmin[1] != "海外抖音#550" || !merged.Children[1].Show {
		t.Fatalf("admin 男人帮 children = %v show=%v %v, want hidden 网红主播 then 海外抖音", gotAdmin, merged.Children[0].Show, merged.Children[1].Show)
	}
}
