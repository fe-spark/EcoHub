package snapshot

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupSearchSourceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.FilmListSnapshot{},
		&model.FilmSnapshotSource{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	db.Mdb = gdb
	db.Rdb = nil
	return gdb
}

func TestSearchSnapshotsByKeywordSourceAndSort(t *testing.T) {
	gdb := setupSearchSourceTestDB(t)
	version := "test_search_source_v1"

	// 准备快照数据
	snapshots := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 1, Name: "斗破苍穹第一季", SourceId: "uku", Hits: 100},
		{SnapshotVersion: version, Mid: 2, Name: "斗破苍穹第二季", SourceId: "hd", Hits: 200},
		{SnapshotVersion: version, Mid: 3, Name: "斗罗大陆", SourceId: "subo", Hits: 300},
	}
	for _, s := range snapshots {
		if err := gdb.Create(&s).Error; err != nil {
			t.Fatalf("create snapshot: %v", err)
		}
	}

	// 准备源关联关系 (Mid 1 在 uku 和 hd 都有; Mid 2 在 hd; Mid 3 在 subo 和 uku 都有)
	sources := []model.FilmSnapshotSource{
		{SnapshotVersion: version, Mid: 1, SourceId: "uku"},
		{SnapshotVersion: version, Mid: 1, SourceId: "hd"},
		{SnapshotVersion: version, Mid: 2, SourceId: "hd"},
		{SnapshotVersion: version, Mid: 3, SourceId: "subo"},
		{SnapshotVersion: version, Mid: 3, SourceId: "uku"},
	}
	for _, s := range sources {
		if err := gdb.Create(&s).Error; err != nil {
			t.Fatalf("create snapshot source: %v", err)
		}
		if err := gdb.Create(&model.FilmSourcePlaylist{Mid: s.Mid, SourceId: s.SourceId, LineKind: "play"}).Error; err != nil {
			t.Fatalf("create playlist: %v", err)
		}
	}
	if err := WriteLiveFilmsFromSnapshots(snapshots); err != nil {
		t.Fatalf("seed live films: %v", err)
	}

	// 1. 搜索 "斗破", 源 "uku" -> 应该命中 Mid 1
	page1 := &dto.Page{Current: 1, PageSize: 10}
	res1 := SearchSnapshotsByKeywordSourceAndSortReadModel(version, "uku", "斗破", "", page1)
	if len(res1) != 1 || page1.Total != 1 || res1[0].Mid != 1 {
		t.Fatalf("uku search expected Mid=1, got len=%d total=%d", len(res1), page1.Total)
	}

	// 2. 搜索 "斗破", 源 "hd" -> 应该命中 Mid 1 和 Mid 2
	page2 := &dto.Page{Current: 1, PageSize: 10}
	res2 := SearchSnapshotsByKeywordSourceAndSortReadModel(version, "hd", "斗破", "", page2)
	if len(res2) != 2 || page2.Total != 2 {
		t.Fatalf("hd search expected 2 (Mid 1, 2), got len=%d total=%d", len(res2), page2.Total)
	}

	// 3. 搜索 "斗", 源 "subo" -> 应该命中 Mid 3
	page3 := &dto.Page{Current: 1, PageSize: 10}
	res3 := SearchSnapshotsByKeywordSourceAndSortReadModel(version, "subo", "斗", "", page3)
	if len(res3) != 1 || page3.Total != 1 || res3[0].Mid != 3 {
		t.Fatalf("subo search expected Mid=3, got len=%d total=%d", len(res3), page3.Total)
	}
}
