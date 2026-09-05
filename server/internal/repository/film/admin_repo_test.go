package film

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

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
	gdb.Create(&model.SlaveMoviePlaylist{SourceId: "src1", MovieKey: "k1", Content: "playlist"})
	gdb.Create(&model.Category{Name: "动作片"})
	gdb.Create(&model.MovieSourceMapping{SourceId: "src1", SourceMid: 100, GlobalMid: 200})
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 1, CategoryId: 10})
	gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 1, RawName: "动作"})
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: "v_old", Mid: 200, Pid: 1})
	gdb.Create(&fileDummy{Id: 1})

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

	var playlistCount, catCount, fileCount, mappingCount, catMapCount, srcCatCount, snapCount int64
	gdb.Model(&model.SlaveMoviePlaylist{}).Count(&playlistCount)
	gdb.Model(&model.Category{}).Count(&catCount)
	gdb.Model(&fileDummy{}).Count(&fileCount)
	gdb.Unscoped().Model(&model.MovieSourceMapping{}).Count(&mappingCount)
	gdb.Unscoped().Model(&model.CategoryMapping{}).Count(&catMapCount)
	gdb.Unscoped().Model(&model.SourceCategory{}).Count(&srcCatCount)
	gdb.Model(&model.FilmListSnapshot{}).Count(&snapCount)

	if playlistCount != 0 || catCount != 0 || fileCount != 0 || mappingCount != 0 || catMapCount != 0 || srcCatCount != 0 || snapCount != 0 {
		t.Fatalf("expected all tables physically cleared, got playlists=%d cats=%d files=%d mapping=%d catMap=%d srcCat=%d snap=%d",
			playlistCount, catCount, fileCount, mappingCount, catMapCount, srcCatCount, snapCount)
	}
}

func TestAdminRepo_RedisNilSafety(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	// 验证在 Redis 为空时均不 panic
	bumpSearchTagsCacheVersion()

	v := getSearchTagsCacheVersion()
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
	_ = GetActiveSnapshotVersion()
	_ = SetActiveSnapshotVersion("v_nil_redis")
	RefreshAccessDataCaches()
	ClearSnapshotState()
	refreshCategoryCaches()

	// 2. 有 DB 但无 Redis 环境
	_ = setupFilmZeroTestDB(t)
	_ = GetActiveSnapshotVersion()
	_ = SetActiveSnapshotVersion("v_nil_redis")
	RefreshAccessDataCaches()
	ClearSnapshotState()
	refreshCategoryCaches()
}
