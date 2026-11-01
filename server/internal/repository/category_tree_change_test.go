package repository

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupCategoryChangeTestDB(t *testing.T) *gorm.DB {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	err = gdb.AutoMigrate(
		&model.SourceCategory{},
		&model.Category{},
		&model.CategoryMapping{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.MovieMatchKey{},
		&model.MovieSourceMapping{},
		&model.Banner{},
		&model.FilmTMDBScrape{},
		&model.FailureRecord{},
	)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	db.Rdb = nil
	return gdb
}

func TestCategoryTreeDiff_OnlyAddOrSort_EmptyChanges(t *testing.T) {
	old := []model.SourceCategory{
		{SourceId: "src_1", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1},
		{SourceId: "src_1", SourceTypeId: 2, ParentSourceTypeId: 1, RawName: "动作片", Sort: 2},
	}
	plans := []sourceCategoryPlacement{
		{SourceTypeId: 2, ParentSourceTypeId: 1, Name: "动作片", Sort: 1}, // 仅改排序
		{SourceTypeId: 1, ParentSourceTypeId: 0, Name: "电影", Sort: 2},  // 仅改排序
		{SourceTypeId: 3, ParentSourceTypeId: 1, Name: "喜剧片", Sort: 3}, // 新增 type_id
	}

	changes := DiffSourceCategoryTree(old, plans)
	if len(changes) != 0 {
		t.Fatalf("expected empty changes, got %+v", changes)
	}
}

func TestCategoryTreeDiff_RenamedMovedRemoved(t *testing.T) {
	old := []model.SourceCategory{
		{SourceId: "src_1", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影"},
		{SourceId: "src_1", SourceTypeId: 2, ParentSourceTypeId: 0, RawName: "电视剧"},
		{SourceId: "src_1", SourceTypeId: 3, ParentSourceTypeId: 1, RawName: "动作片"},
		{SourceId: "src_1", SourceTypeId: 4, ParentSourceTypeId: 0, RawName: "伦理片"},
	}
	plans := []sourceCategoryPlacement{
		{SourceTypeId: 1, ParentSourceTypeId: 0, Name: "电影"},
		{SourceTypeId: 2, ParentSourceTypeId: 0, Name: "剧集"}, // renamed (电视剧 -> 剧集)
		// 3: 动作片 -> 福利片 (renamed), 且父级从电影(1) -> 顶级(0) (moved)
		{SourceTypeId: 3, ParentSourceTypeId: 0, Name: "福利片"},
		// 4: 伦理片 removed
	}

	changes := DiffSourceCategoryTree(old, plans)
	if len(changes) != 4 {
		t.Fatalf("expected 4 changes, got %d: %+v", len(changes), changes)
	}

	// 按 typeId 升序，同一 id 先改名再换父级
	// 2: renamed
	if changes[0].TypeId != 2 || changes[0].Kind != model.CategoryChangeRenamed || changes[0].From != "电视剧" || changes[0].To != "剧集" {
		t.Fatalf("changes[0] mismatch: %+v", changes[0])
	}
	// 3: renamed
	if changes[1].TypeId != 3 || changes[1].Kind != model.CategoryChangeRenamed || changes[1].From != "动作片" || changes[1].To != "福利片" {
		t.Fatalf("changes[1] mismatch: %+v", changes[1])
	}
	// 3: moved (父级 0 应显示顶级)
	if changes[2].TypeId != 3 || changes[2].Kind != model.CategoryChangeMoved || changes[2].From != "电影" || changes[2].To != "顶级" {
		t.Fatalf("changes[2] mismatch: %+v", changes[2])
	}
	// 4: removed
	if changes[3].TypeId != 4 || changes[3].Kind != model.CategoryChangeRemoved || changes[3].Name != "伦理片" {
		t.Fatalf("changes[3] mismatch: %+v", changes[3])
	}
}

func TestCategoryTreeSync_EmptyNewTreeWhenOldExists_Errors(t *testing.T) {
	setupCategoryChangeTestDB(t)

	// 先存旧树
	oldTree := &model.CategoryTree{
		Children: []*model.CategoryTree{
			{Id: 1, Name: "电影"},
		},
	}
	if err := SaveCategoryTree("src_test", oldTree); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	// 预览空新树
	emptyTree := &model.CategoryTree{Children: nil}
	_, err := PreviewSourceCategoryChanges("src_test", emptyTree)
	if err == nil || err.Error() != "采集站返回的分类为空，未更新" {
		t.Fatalf("preview should reject empty tree, err=%v", err)
	}

	// 应用空新树
	_, err = ApplyCategoryTree("src_test", emptyTree, false)
	if err == nil || err.Error() != "采集站返回的分类为空，未更新" {
		t.Fatalf("apply should reject empty tree, err=%v", err)
	}

	// 确认旧数据还在
	var count int64
	db.Mdb.Model(&model.SourceCategory{}).Where("source_id = ?", "src_test").Count(&count)
	if count != 1 {
		t.Fatalf("old category rows should remain, count=%d", count)
	}
}

func TestCategoryTreeSync_ApplyWithoutClear(t *testing.T) {
	gdb := setupCategoryChangeTestDB(t)

	// 初始化旧树
	oldTree := &model.CategoryTree{
		Children: []*model.CategoryTree{
			{Id: 1, Name: "电影"},
			{Id: 2, Name: "动作片", Pid: 1},
		},
	}
	if err := SaveCategoryTree("src_1", oldTree); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	// 插入片子和线路
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101},
		FilmIndexContent:  model.FilmIndexContent{Name: "片1"},
		FilmIndexCategory: model.FilmIndexCategory{
			CategoryKey:     "source:src_1:2",
			RootCategoryKey: "source:src_1:1",
		},
	})
	gdb.Create(&model.FilmSourcePlaylist{
		Mid:      101,
		SourceId: "src_1",
		LineKind: "play",
	})

	// 新树（改名）且 clearCollected=false
	newTree := &model.CategoryTree{
		Children: []*model.CategoryTree{
			{Id: 1, Name: "电影"},
			{Id: 2, Name: "动作大片", Pid: 1},
		},
	}
	res, err := ApplyCategoryTree("src_1", newTree, false)
	if err != nil {
		t.Fatalf("apply without clear: %v", err)
	}
	if res.DataCleared {
		t.Fatalf("data should not be cleared")
	}

	// 验证线路还在，片子的 category_key 不变
	var playCount int64
	gdb.Model(&model.FilmSourcePlaylist{}).Where("mid = 101 AND source_id = 'src_1'").Count(&playCount)
	if playCount != 1 {
		t.Fatalf("playlist should remain")
	}
	var fi model.FilmIndex
	gdb.Where("mid = 101").First(&fi)
	if fi.CategoryKey != "source:src_1:2" {
		t.Fatalf("category_key should remain unchanged, got %s", fi.CategoryKey)
	}
}

func TestCategoryTreeSync_ApplyWithClear(t *testing.T) {
	gdb := setupCategoryChangeTestDB(t)

	// 初始化旧树
	oldTree := &model.CategoryTree{
		Children: []*model.CategoryTree{
			{Id: 1, Name: "电影"},
			{Id: 2, Name: "动作片", Pid: 1},
			{Id: 3, Name: "科幻片", Pid: 1},
		},
	}
	if err := SaveCategoryTree("src_1", oldTree); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101, FirstSourceId: "src_1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "源1片"},
		FilmIndexCategory: model.FilmIndexCategory{
			CategoryKey:     "source:src_1:2",
			RootCategoryKey: "source:src_1:1",
		},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 104, FirstSourceId: "src_1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "源1无线路"},
	})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 101, SourceId: "src_1", LineKind: "play"})
	gdb.Create(&model.MovieMatchKey{Mid: 101, MatchKey: "key_101"})

	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 102, FirstSourceId: "src_2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "源2片"},
		FilmIndexCategory: model.FilmIndexCategory{
			CategoryKey:     "source:src_2:2",
			RootCategoryKey: "source:src_2:1",
		},
	})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 102, SourceId: "src_1", LineKind: "play"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 102, SourceId: "src_2", LineKind: "play"})

	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 103, FirstSourceId: "src_2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "源2片3"},
		FilmIndexCategory: model.FilmIndexCategory{
			CategoryKey:     "source:src_2:3",
			RootCategoryKey: "source:src_2:1",
		},
	})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 103, SourceId: "src_1", LineKind: "play"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 103, SourceId: "src_2", LineKind: "play"})

	// 新树：分类 2 改名（动作片 -> 动作大片），分类 3 未变
	newTree := &model.CategoryTree{
		Children: []*model.CategoryTree{
			{Id: 1, Name: "电影"},
			{Id: 2, Name: "动作大片", Pid: 1},
			{Id: 3, Name: "科幻片", Pid: 1},
		},
	}

	res, err := ApplyCategoryTree("src_1", newTree, true)
	if err != nil {
		t.Fatalf("apply with clear: %v", err)
	}
	if !res.DataCleared {
		t.Fatalf("expected DataCleared to be true")
	}

	// a. 该站线路没了
	var src1Playlists int64
	gdb.Model(&model.FilmSourcePlaylist{}).Where("source_id = 'src_1'").Count(&src1Playlists)
	if src1Playlists != 0 {
		t.Fatalf("src_1 playlists must be cleared, got %d", src1Playlists)
	}

	// b. 另一站的线路还在
	var src2Playlists int64
	gdb.Model(&model.FilmSourcePlaylist{}).Where("source_id = 'src_2'").Count(&src2Playlists)
	if src2Playlists != 2 {
		t.Fatalf("src_2 playlists must remain, got %d", src2Playlists)
	}

	var fi101Count int64
	gdb.Model(&model.FilmIndex{}).Where("mid = 101").Count(&fi101Count)
	if fi101Count != 0 {
		t.Fatalf("owned film 101 must be deleted")
	}
	var fi104Count int64
	gdb.Model(&model.FilmIndex{}).Where("mid = 104").Count(&fi104Count)
	if fi104Count != 0 {
		t.Fatalf("playless film 104 must be deleted")
	}
	var keys int64
	gdb.Model(&model.MovieMatchKey{}).Where("mid = 101").Count(&keys)
	if keys != 0 {
		t.Fatalf("match key left on deleted film")
	}

	var fi102 model.FilmIndex
	if err := gdb.Where("mid = 102").First(&fi102).Error; err != nil {
		t.Fatalf("src_2 film 102 must remain: %v", err)
	}
	if fi102.CategoryKey != "source:src_2:2" {
		t.Fatalf("film 102 category changed, got %s", fi102.CategoryKey)
	}
	var fi103 model.FilmIndex
	if err := gdb.Where("mid = 103").First(&fi103).Error; err != nil {
		t.Fatalf("src_2 film 103 must remain: %v", err)
	}
	if fi103.CategoryKey != "source:src_2:3" {
		t.Fatalf("film 103 category changed, got %s", fi103.CategoryKey)
	}
}

func TestCategoryTreeSync_ClearRequestedButNoChanges_DoesNotClearPlaylists(t *testing.T) {
	gdb := setupCategoryChangeTestDB(t)

	tree := &model.CategoryTree{
		Children: []*model.CategoryTree{
			{Id: 1, Name: "电影"},
		},
	}
	if err := SaveCategoryTree("src_1", tree); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 201},
		FilmIndexContent:  model.FilmIndexContent{Name: "片201"},
	})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 201, SourceId: "src_1", LineKind: "play"})

	// 分类完全没变，clearCollected=true
	res, err := ApplyCategoryTree("src_1", tree, true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.DataCleared {
		t.Fatalf("data should not be cleared when changes are empty")
	}

	var count int64
	gdb.Model(&model.FilmSourcePlaylist{}).Where("source_id = 'src_1'").Count(&count)
	if count != 1 {
		t.Fatalf("playlists must remain, count=%d", count)
	}
}
