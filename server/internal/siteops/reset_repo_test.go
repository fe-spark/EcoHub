package siteops

import (
	"testing"

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

func TestFilmZero_CleansInventoryTables(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	gdb := setupFilmZeroTestDB(t)

	// 注入数据
	gdb.Create(&model.User{UserName: "admin_keep", Password: "hashed", Role: model.UserRoleAdmin, Status: 0})
	gdb.Create(&model.SchemaMigration{Version: "20261120_init"})
	gdb.Create(&model.FilmSource{Id: "src1", Name: "自定义站", Uri: "https://example.com/api", State: true})
	gdb.Create(&model.PrimaryState{Id: 1, PrimarySourceId: "src1"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 200, SourceId: "src1", LineKind: "play", Content: "playlist"})
	gdb.Create(&model.Category{Name: "动作片"})
	gdb.Create(&model.MovieSourceMapping{SourceId: "src1", SourceMid: 100, GlobalMid: 200})
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 1, CategoryId: 10})
	gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 1, RawName: "动作"})
	gdb.Create(&model.SiteConfigRecord{SiteName: "OldSite"})
	gdb.Create(&model.CrontabRecord{TaskId: "task1", TaskModel: 0, Spec: "0 0 * * * *"})
	gdb.Create(&fileDummy{Id: 1})
	gdb.Create(&model.BannerSlot{Id: "b1", SourceId: "src1", Mid: 200})

	if err := FilmZero(); err != nil {
		t.Fatalf("FilmZero failed: %v", err)
	}

	var playlistCount, catCount, fileCount, mappingCount, catMapCount, srcCatCount, bannerCount int64
	var sourceCount, primaryCount, siteCfgCount, cronCount, userCount, migCount int64
	gdb.Model(&model.FilmSourcePlaylist{}).Count(&playlistCount)
	gdb.Model(&model.Category{}).Count(&catCount)
	gdb.Model(&fileDummy{}).Count(&fileCount)
	gdb.Unscoped().Model(&model.MovieSourceMapping{}).Count(&mappingCount)
	gdb.Unscoped().Model(&model.CategoryMapping{}).Count(&catMapCount)
	gdb.Unscoped().Model(&model.SourceCategory{}).Count(&srcCatCount)
	gdb.Model(&model.BannerSlot{}).Count(&bannerCount)
	gdb.Unscoped().Model(&model.FilmSource{}).Count(&sourceCount)
	gdb.Model(&model.PrimaryState{}).Count(&primaryCount)
	gdb.Model(&model.SiteConfigRecord{}).Count(&siteCfgCount)
	gdb.Model(&model.CrontabRecord{}).Count(&cronCount)
	gdb.Model(&model.User{}).Count(&userCount)
	gdb.Model(&model.SchemaMigration{}).Count(&migCount)

	if playlistCount != 0 || fileCount != 0 || mappingCount != 0 || bannerCount != 0 ||
		catCount != 0 || catMapCount != 0 || srcCatCount != 0 ||
		sourceCount != 0 || primaryCount != 0 || siteCfgCount != 0 || cronCount != 0 {
		t.Fatalf("expected all non-user tables cleared, got playlists=%d files=%d mapping=%d banners=%d cats=%d catMap=%d srcCat=%d sources=%d primary=%d siteCfg=%d crons=%d",
			playlistCount, fileCount, mappingCount, bannerCount, catCount, catMapCount, srcCatCount, sourceCount, primaryCount, siteCfgCount, cronCount)
	}
	if userCount != 1 || migCount != 1 {
		t.Fatalf("expected user and schema_migrations preserved, got users=%d migrations=%d", userCount, migCount)
	}
}
