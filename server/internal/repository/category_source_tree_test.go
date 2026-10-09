package repository

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
)

func TestGetSourceBoundCategoryTree_OnlyThatSource(t *testing.T) {
	gdb := setupCategoryActiveTreeTestDB(t)
	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "电影", StableKey: "a-movie", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 10, Pid: 9, Name: "动作片", StableKey: "a-action", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 11, Pid: 9, Name: "喜剧片", StableKey: "a-comedy", Show: true, Sort: 2})
	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电视剧", StableKey: "b-tv", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 6, CategoryId: 10})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 7, CategoryId: 11})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 2, CategoryId: 1})

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
			Children: []*model.CategoryTree{
				{Id: 11, Name: "喜剧片"},
				{Id: 10, Name: "动作片"},
			},
		},
	}
	if err := SaveSourceCategoryOrder("src_a", nodes); err != nil {
		t.Fatalf("save: %v", err)
	}
	var action, comedy, other model.Category
	gdb.First(&action, 10)
	gdb.First(&comedy, 11)
	gdb.First(&other, 1)
	if comedy.Sort != 1 || action.Sort != 2 {
		t.Fatalf("child sort comedy=%d action=%d", comedy.Sort, action.Sort)
	}
	if other.Sort != 3 {
		t.Fatalf("other source sort changed: %d", other.Sort)
	}
	if err := SaveSourceCategoryOrder("src_a", []*model.CategoryTree{{Id: 1, Name: "电视剧"}}); err == nil {
		t.Fatal("category from another source should be rejected")
	}
}
