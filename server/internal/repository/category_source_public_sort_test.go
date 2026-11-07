package repository

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

func TestSourceTypeTree_UsesMappedSort(t *testing.T) {
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
		{Id: 9, Pid: 0, Name: "电影", Show: true, Sort: 3, StableKey: "src1:1"},
		{Id: 547, Pid: 0, Name: "福利视频", Show: true, Sort: 1, StableKey: "src1:5"},
		{Id: 549, Pid: 0, Name: "网红主播", Show: true, Sort: 3, StableKey: "src1:18"},
		{Id: 550, Pid: 0, Name: "海外抖音", Show: true, Sort: 2, StableKey: "src1:37"},
	}
	if err := gdb.Create(&categories).Error; err != nil {
		t.Fatalf("create categories: %v", err)
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

	tree, err := GetActiveCategoryTree("src1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Children) != 2 {
		t.Fatalf("roots = %v, want 男人帮 then 电影", rootNames(tree))
	}
	if tree.Children[0].Name != "男人帮" || tree.Children[0].Id != 5 || tree.Children[0].Sort != 1 {
		t.Fatalf("first root = %+v, want 男人帮#5 sort 1", tree.Children[0])
	}
	if tree.Children[1].Name != "电影" || tree.Children[1].Id != 1 || tree.Children[1].Sort != 3 {
		t.Fatalf("second root = %+v, want 电影#1 sort 3", tree.Children[1])
	}
	got := childNames(tree.Children[0])
	if len(tree.Children[0].Children) != 3 || got[0] != "福利视频#5" || got[1] != "海外抖音#37" || got[2] != "网红主播#18" {
		t.Fatalf("男人帮 children = %v, want 福利视频#5 海外抖音#37 网红主播#18", got)
	}

	admin := GetSourceBoundCategoryTree("src1")
	if len(admin.Children) != 2 || admin.Children[0].Name != "男人帮" || admin.Children[0].Id != 547 || admin.Children[0].Sort != 1 {
		t.Fatalf("admin first = %+v, want 男人帮#547 sort 1", admin.Children[0])
	}
	if admin.Children[1].Name != "电影" || admin.Children[1].Id != 9 || admin.Children[1].Sort != 3 {
		t.Fatalf("admin second = %+v, want 电影#9 sort 3", admin.Children[1])
	}
	adminKids := childNames(admin.Children[0])
	if len(admin.Children[0].Children) != 2 || adminKids[0] != "海外抖音#550" || adminKids[1] != "网红主播#549" {
		t.Fatalf("admin 男人帮 children = %v, want 海外抖音#550 网红主播#549", adminKids)
	}
}
