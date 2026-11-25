package film

import (
	"testing"
	"time"

	"server/internal/film/cache"
	"server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fileDummy struct {
	Id uint `gorm:"primarykey"`
}

func (fileDummy) TableName() string {
	return "files"
}

func setupFilmZeroTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	if err := gdb.AutoMigrate(&fileDummy{}); err != nil {
		t.Fatalf("migrate fileDummy schema: %v", err)
	}
	db.Mdb = gdb
	return gdb
}

func TestCleanEmptyFilms_HardDeletesEmptyAndUnknownCategory(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() { db.Rdb = origRdb }()

	gdb := setupFilmZeroTestDB(t)
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
		FilmIndexContent:  model.FilmIndexContent{Name: ""},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 0},
		FilmIndexContent:  model.FilmIndexContent{Name: "无分类"},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
		FilmIndexContent:  model.FilmIndexContent{Name: "保留"},
	})
	gdb.Create(&model.MovieMatchKey{Mid: 1, MatchKey: "empty-name"})
	gdb.Create(&model.MovieSourceMapping{SourceId: "src", SourceMid: 9, GlobalMid: 2})

	if n, err := CleanEmptyFilms(); err != nil || n != 2 {
		t.Fatalf("cleaned=%d, err=%v, want 2", n, err)
	}
	var left int64
	gdb.Unscoped().Model(&model.FilmIndex{}).Count(&left)
	if left != 1 {
		t.Fatalf("film rows=%d, want 1 hard-deleted", left)
	}
	var name string
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 3).Pluck("name", &name)
	if name != "保留" {
		t.Fatalf("kept name=%q", name)
	}
	var keys, mappings int64
	gdb.Model(&model.MovieMatchKey{}).Where("mid = ?", 1).Count(&keys)
	gdb.Model(&model.MovieSourceMapping{}).Where("global_mid = ?", 2).Count(&mappings)
	if keys != 0 || mappings != 0 {
		t.Fatalf("orphan keys=%d mappings=%d", keys, mappings)
	}
}

func TestAdminRepo_RedisNilSafety(t *testing.T) {
	_ = setupFilmZeroTestDB(t)
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	// 验证在 Redis 为空时均不 panic
	cache.BumpSearchTagsVersion()

	v := cache.GetSearchTagsVersion()
	if v == "" {
		t.Fatalf("expected non-empty version fallback when Redis is nil")
	}

	RefreshMasterDataCaches()
}

func TestSnapshotAndShared_RedisNilSafety(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	// 1. 无 DB 也无 Redis 环境
	_ = snapshot.GetActiveSnapshotVersion()
	_ = snapshot.SetActiveSnapshotVersion("v_nil_redis")
	snapshot.RefreshAccessDataCaches()
	snapshot.ClearSnapshotState()
	refreshCategoryCaches()

	// 2. 有 DB 但无 Redis 环境
	_ = setupFilmZeroTestDB(t)
	_ = snapshot.GetActiveSnapshotVersion()
	_ = snapshot.SetActiveSnapshotVersion("v_nil_redis")
	snapshot.RefreshAccessDataCaches()
	snapshot.ClearSnapshotState()
	refreshCategoryCaches()
}

func TestInvalidateMasterSwitchCaches_ClearsActiveSnapshotVersion(t *testing.T) {
	_ = setupFilmZeroTestDB(t)
	origRdb := db.Rdb
	db.Rdb = nil
	t.Cleanup(func() { db.Rdb = origRdb })

	if err := snapshot.SetActiveSnapshotVersion("ghost_after_switch"); err != nil {
		t.Fatalf("set version: %v", err)
	}
	if snapshot.GetActiveSnapshotVersion() != "ghost_after_switch" {
		t.Fatal("expected version to be set in memory")
	}

	InvalidateMasterSwitchCaches()
	if got := snapshot.GetActiveSnapshotVersion(); got != "" {
		t.Fatalf("expected memory snapshot version cleared on master switch, got %q", got)
	}
}

func TestCleanPlaylessFilms_MultiBatchCursorScansPastHealthyRows(t *testing.T) {
	origRdb := db.Rdb
	origBatch := filmCleanBatchSize
	db.Rdb = nil
	filmCleanBatchSize = 2
	t.Cleanup(func() {
		db.Rdb = origRdb
		filmCleanBatchSize = origBatch
	})

	gdb := setupFilmZeroTestDB(t)
	oldTime := time.Now().Add(-10 * 24 * time.Hour)

	// 前 5 条为带播放线路的健康老片，第 6 条为无播放线路的幽灵老片（跨 3 批扫描仍能精确清理且不误伤前 5 条）
	for mid := int64(1); mid <= 5; mid++ {
		gdb.Create(&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
			FilmIndexContent:  model.FilmIndexContent{Name: "健康片"},
			CreatedAt:         oldTime,
			UpdatedAt:         oldTime,
		})
		gdb.Create(&model.FilmSourcePlaylist{Mid: mid, SourceId: "s1", LineKind: "play", Content: "01$https://v/1.m3u8"})
	}
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 6},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
		FilmIndexContent:  model.FilmIndexContent{Name: "幽灵片"},
		CreatedAt:         oldTime,
		UpdatedAt:         oldTime,
	})

	cleaned, err := CleanPlaylessFilms(7 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("CleanPlaylessFilms failed: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("expected 1 ghost film cleaned across batches, got %d", cleaned)
	}
	var left int64
	gdb.Unscoped().Model(&model.FilmIndex{}).Count(&left)
	if left != 5 {
		t.Fatalf("expected 5 healthy films preserved, got %d", left)
	}
}
