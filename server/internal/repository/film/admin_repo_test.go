package film

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/cache"
	"server/internal/repository/film/snapshot"

	"gorm.io/driver/sqlite"
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
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
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

func TestFilmZero_CleansAllTablesIncludingPosters(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	gdb := setupFilmZeroTestDB(t)

	// 注入数据
	gdb.Create(&model.MoviePoster{SourceId: "src1", MovieKey: "k1", Picture: "http://poster1"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 200, SourceId: "src1", LineKind: "play", Content: "playlist"})
	gdb.Create(&model.Category{Name: "动作片"})
	gdb.Create(&model.MovieSourceMapping{SourceId: "src1", SourceMid: 100, GlobalMid: 200})
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 1, CategoryId: 10})
	gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 1, RawName: "动作"})
	gdb.Create(&fileDummy{Id: 1})
	gdb.Create(&model.Banner{Id: "b1", Mid: 200, Name: "测试轮播"})
	gdb.Create(&model.FailureRecord{OriginId: "src1", Uri: "http://test", Cause: "err"})

	// 确认数据已存在
	var posterCount int64
	gdb.Model(&model.MoviePoster{}).Count(&posterCount)
	if posterCount != 1 {
		t.Fatalf("expected 1 poster before FilmZero, got %d", posterCount)
	}

	// 执行清库
	if err := FilmZero(); err != nil {
		t.Fatalf("FilmZero failed: %v", err)
	}

	// 验证所有表均已被清空，尤其是 TableMoviePoster 及物理清空的映射表与快照表
	gdb.Model(&model.MoviePoster{}).Count(&posterCount)
	if posterCount != 0 {
		t.Fatalf("expected 0 posters after FilmZero, got %d", posterCount)
	}

	var playlistCount, catCount, fileCount, mappingCount, catMapCount, srcCatCount, bannerCount, failureCount int64
	gdb.Model(&model.FilmSourcePlaylist{}).Count(&playlistCount)
	gdb.Model(&model.Category{}).Count(&catCount)
	gdb.Model(&fileDummy{}).Count(&fileCount)
	gdb.Unscoped().Model(&model.MovieSourceMapping{}).Count(&mappingCount)
	gdb.Unscoped().Model(&model.CategoryMapping{}).Count(&catMapCount)
	gdb.Unscoped().Model(&model.SourceCategory{}).Count(&srcCatCount)
	gdb.Model(&model.Banner{}).Count(&bannerCount)
	gdb.Model(&model.FailureRecord{}).Count(&failureCount)

	if playlistCount != 0 || catCount != 0 || fileCount != 0 || mappingCount != 0 || catMapCount != 0 || srcCatCount != 0 || bannerCount != 0 || failureCount != 0 {
		t.Fatalf("expected all tables physically cleared, got playlists=%d cats=%d files=%d mapping=%d catMap=%d srcCat=%d banners=%d failures=%d",
			playlistCount, catCount, fileCount, mappingCount, catMapCount, srcCatCount, bannerCount, failureCount)
	}
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

	if n := CleanEmptyFilms(); n != 2 {
		t.Fatalf("cleaned=%d, want 2", n)
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
