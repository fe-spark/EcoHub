package support

import (
	"testing"

	"server/internal/model"
)

func TestIsOtherCategoryName(t *testing.T) {
	if !model.IsOtherCategoryName("其他") || !model.IsOtherCategoryName("其它") || !model.IsOtherCategoryName(" 其他 ") {
		t.Fatal("其他/其它 should match")
	}
	if model.IsOtherCategoryName("电影") || model.IsOtherCategoryName("") {
		t.Fatal("unrelated names should not match")
	}
}

func TestLookupOtherSourceCategory(t *testing.T) {
	prev := CurrentMappingSnapshotForTest()
	t.Cleanup(func() { StoreMappingSnapshotForTest(prev) })

	snap := emptyMappingSnapshot()
	snap.SourceCategories["src_1_20"] = SourceCategoryInfo{RawName: "其它", ParentSourceTypeId: 0}
	snap.SourceCategories["src_1_1"] = SourceCategoryInfo{RawName: "电影", ParentSourceTypeId: 0}
	snap.Source["src_1_20"] = 88
	StoreMappingSnapshotForTest(snap)

	typeID, categoryID, ok := LookupOtherSourceCategory("src_1")
	if !ok || typeID != 20 || categoryID != 88 {
		t.Fatalf("got type=%d cat=%d ok=%v", typeID, categoryID, ok)
	}
	if _, _, ok := LookupOtherSourceCategory("src_missing"); ok {
		t.Fatal("missing source should not hit")
	}
}
