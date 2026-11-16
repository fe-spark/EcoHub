package snapshot

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupSearchSourceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.FilmSource{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	if err := gdb.Create(&model.FilmSource{
		Id: "uku", Name: "主站", Uri: "https://example.com/uku", State: true,
	}).Error; err != nil {
		t.Fatalf("create primary: %v", err)
	}
	db.Mdb = gdb
	db.Rdb = nil
	return gdb
}

func TestSearchSnapshotsByKeywordSourceAndSort(t *testing.T) {
	gdb := setupSearchSourceTestDB(t)
	version := "test_search_source_v1"

	snapshots := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 1, Name: "斗破苍穹第一季", SourceId: "uku", Hits: 100},
		{SnapshotVersion: version, Mid: 2, Name: "斗破苍穹第二季", SourceId: "hd", Hits: 200},
		{SnapshotVersion: version, Mid: 3, Name: "斗罗大陆", SourceId: "subo", Hits: 300},
	}
	// Mid 1 在 uku 和 hd 都有；Mid 2 在 hd；Mid 3 在 subo 和 uku。
	playlists := []model.FilmSourcePlaylist{
		{Mid: 1, SourceId: "uku", LineKind: "play"},
		{Mid: 1, SourceId: "hd", LineKind: "play"},
		{Mid: 2, SourceId: "hd", LineKind: "play"},
		{Mid: 3, SourceId: "subo", LineKind: "play"},
		{Mid: 3, SourceId: "uku", LineKind: "play"},
	}
	for _, line := range playlists {
		if err := gdb.Create(&line).Error; err != nil {
			t.Fatalf("create playlist: %v", err)
		}
	}
	if err := WriteLiveFilmsFromSnapshots(snapshots); err != nil {
		t.Fatalf("seed live films: %v", err)
	}

	// 1. 搜索 "斗破", 源 "uku" -> 应该命中 Mid 1
	page1 := &dto.Page{Current: 1, PageSize: 10}
	res1, err := SearchSnapshotsByKeywordSourceAndSortReadModel(version, "uku", "斗破", "", page1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res1) != 1 || page1.Total != 1 || res1[0].Mid != 1 {
		t.Fatalf("uku search expected Mid=1, got len=%d total=%d", len(res1), page1.Total)
	}

	// 非主站不在内存索引里。
	page2 := &dto.Page{Current: 1, PageSize: 10}
	res2, err := SearchSnapshotsByKeywordSourceAndSortReadModel(version, "hd", "斗破", "", page2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2) != 1 || page2.Total != 1 || res2[0].Mid != 1 {
		t.Fatalf("hd search expected primary film mid 1, got len=%d total=%d", len(res2), page2.Total)
	}

	page3 := &dto.Page{Current: 1, PageSize: 10}
	res3, err := SearchSnapshotsByKeywordSourceAndSortReadModel(version, "subo", "斗", "", page3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res3) != 0 || page3.Total != 0 {
		t.Fatalf("subo search expected empty, got len=%d total=%d", len(res3), page3.Total)
	}
}
