package catalog

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"
)

func TestSourceTypeTree_RulesStayOnOwnStation(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	prev := taxonomy.CurrentMappingSnapshotForTest()
	t.Cleanup(func() {
		db.Rdb = origRdb
		taxonomy.StoreMappingSnapshotForTest(prev)
	})

	gdb := setupCategoryActiveTreeTestDB(t)
	if err := gdb.AutoMigrate(&model.SourceCategory{}, &model.MappingRule{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rows := []model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 5, ParentSourceTypeId: 0, RawName: "福利视频", Sort: 1, Depth: 0},
		{SourceId: "src1", SourceTypeId: 18, ParentSourceTypeId: 0, RawName: "网红主播", Sort: 2, Depth: 0},
		{SourceId: "src2", SourceTypeId: 5, ParentSourceTypeId: 0, RawName: "福利视频", Sort: 1, Depth: 0},
		{SourceId: "src2", SourceTypeId: 18, ParentSourceTypeId: 0, RawName: "网红主播", Sort: 2, Depth: 0},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}
	if err := gdb.Create(&model.MappingRule{SourceId: "src1", Group: "CategoryRoot", Raw: "福利视频|网红主播", Target: "男人帮", MatchType: "regex"}).Error; err != nil {
		t.Fatalf("create rule: %v", err)
	}
	taxonomy.ReloadMappingRules()

	src1, err := GetActiveCategoryTree("src1")
	if err != nil {
		t.Fatal(err)
	}
	if len(src1.Children) != 1 || src1.Children[0].Name != "男人帮" {
		t.Fatalf("src1 roots = %v, want 男人帮", rootNames(src1))
	}

	src2, err := GetActiveCategoryTree("src2")
	if err != nil {
		t.Fatal(err)
	}
	got := rootNames(src2)
	if len(src2.Children) != 2 || got[0] != "福利视频#5" || got[1] != "网红主播#18" {
		t.Fatalf("src2 roots = %v, want original names", got)
	}
}
