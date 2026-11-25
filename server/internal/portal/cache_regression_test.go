package portal

import (
	"testing"

	filmsnapshot "server/internal/film/snapshot"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/taxonomy"
)

func TestGetFilmClassify_WithSourceIDNotBlockedByLocalHiddenCategory(t *testing.T) {
	gdb, _ := setupTestDBAndRedis(t)
	if err := gdb.AutoMigrate(&model.Category{}, &model.SourceCategory{}, &model.CategoryMapping{}); err != nil {
		t.Fatalf("migrate category tables: %v", err)
	}

	// 本地分类 id=1 隐藏，但请求指定了 sourceId="source_master" 对应的源站 type_id=1（映射到 id=2 可见，不应被本地 id=1 误拦）
	taxonomy.SetCategoryShowForTest(map[int64]bool{1: false, 2: true})
	t.Cleanup(func() { taxonomy.SetCategoryShowForTest(nil) })

	const ver = "v_classify_src_test"
	seedLiveSnaps(t, model.FilmListSnapshot{
		SnapshotVersion: ver,
		Mid:             9101,
		Pid:             2,
		Cid:             10,
		SourceId:        "source_master",
		RootCategoryKey: "source:source_master:1",
		CategoryKey:     "source:source_master:1",
		Name:            "源站可见电影",
		UpdateStamp:     1000,
	})
	_ = gdb.Create(&model.SourceCategory{SourceId: "source_master", SourceTypeId: 1, RawName: "电影"}).Error
	_ = gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "本地隐藏", Show: false}).Error
	_ = gdb.Create(&model.Category{Id: 2, Pid: 0, Name: "电影", Show: true}).Error
	_ = gdb.Create(&model.CategoryMapping{SourceId: "source_master", SourceTypeId: 1, CategoryId: 2}).Error

	_ = filmsnapshot.SetActiveSnapshotVersion(ver)
	_ = filmsnapshot.LoadActiveFilmReadModel(ver)
	filmsnapshot.WaitActiveFilmSearchIndexBuilt()

	res, err := IndexSvc.GetFilmClassify(1, nil, "source_master")
	if err != nil {
		t.Fatalf("GetFilmClassify failed: %v", err)
	}
	recent, ok := res["recent"].([]model.MovieBasicInfo)
	if !ok || len(recent) == 0 {
		t.Fatalf("expected source classification results not blocked by local category id=1 visibility, got %+v", res["recent"])
	}
}

func TestClearIndexPageCache_BumpsGeneration(t *testing.T) {
	before := repobase.IndexPageGeneration()
	repobase.ClearIndexPageCache()
	after := repobase.IndexPageGeneration()
	if after <= before {
		t.Fatalf("expected IndexPageGeneration to increase on ClearIndexPageCache, before=%d after=%d", before, after)
	}
}
