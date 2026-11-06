package repository

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

func TestSourceTypeTree_RulesStayOnOwnStation(t *testing.T) {
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
	ReloadMappingRules()

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

func TestPreviewMappingRule_HitsRealSourceCategories(t *testing.T) {
	gdb := setupCategoryActiveTreeTestDB(t)
	if err := gdb.AutoMigrate(&model.SourceCategory{}, &model.FilmSource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src1", Name: "爱旦", Uri: "http://src1.test", State: true}).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	rows := []model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 5, ParentSourceTypeId: 0, RawName: "福利视频", Sort: 1, Depth: 0},
		{SourceId: "src1", SourceTypeId: 18, ParentSourceTypeId: 0, RawName: "网红主播", Sort: 2, Depth: 0},
		{SourceId: "src1", SourceTypeId: 37, ParentSourceTypeId: 0, RawName: "海外抖音", Sort: 3, Depth: 0},
		{SourceId: "src1", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 4, Depth: 0},
		{SourceId: "src1", SourceTypeId: 12, ParentSourceTypeId: 1, RawName: "动作片", Sort: 1, Depth: 1},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}

	preview, err := PreviewMappingRule("src1", "CategoryRoot", "福利视频|海外抖音|网红主播", "regex", "男人帮")
	if err != nil {
		t.Fatal(err)
	}
	if preview.SourceName != "爱旦" || preview.Target != "男人帮" || len(preview.Hits) != 3 {
		t.Fatalf("preview = %+v, want 3 root hits", preview)
	}
	if preview.Hits[0].Name != "福利视频" || preview.Hits[1].Name != "网红主播" || preview.Hits[2].Name != "海外抖音" {
		t.Fatalf("hits = %+v", preview.Hits)
	}

	sub, err := PreviewMappingRule("src1", "CategorySub", "片$", "regex", "影片")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Hits) != 1 || sub.Hits[0].Name != "动作片" {
		t.Fatalf("sub preview = %+v, want 动作片", sub)
	}
}
