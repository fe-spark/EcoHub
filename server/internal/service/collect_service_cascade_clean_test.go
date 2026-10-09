package service

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupCascadeCleanTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.MovieMatchKey{},
		&model.MovieSourceMapping{},
		&model.MoviePoster{},
		&model.Banner{},
		&model.FailureRecord{},
		&model.CronSourceRel{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
		&model.Category{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	db.Mdb = gdb
	db.Rdb = nil
	return gdb
}

func TestCollectService_DelFilmSource_CleansOrphanFilms(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)

	// 创建两个采集源: src1 和 src2
	gdb.Create(&model.FilmSource{Id: "src1", Name: "源1", Uri: "http://src1.com", State: true})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "源2", Uri: "http://src2.com", State: true})

	// 影片 1: 仅属于 src1 (独占影片)
	// 影片 2: 属于 src1 和 src2 (多源影片)
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1},
		FilmIndexContent:  model.FilmIndexContent{Name: "独占影片"},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2},
		FilmIndexContent:  model.FilmIndexContent{Name: "多源影片"},
	})

	// 播放线路
	gdb.Create(&model.FilmSourcePlaylist{Mid: 1, SourceId: "src1", LineKind: "play", Content: "[]"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 2, SourceId: "src1", LineKind: "play", Content: "[]"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 2, SourceId: "src2", LineKind: "play", Content: "[]"})
	gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 1, RawName: "电影"})
	gdb.Create(&model.SourceCategory{SourceId: "src2", SourceTypeId: 1, RawName: "电影"})
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.CategoryMapping{SourceId: "src2", SourceTypeId: 1, CategoryId: 9})

	// 删除 src1
	srv := new(CollectService)
	if err := srv.DelFilmSource("src1"); err != nil {
		t.Fatalf("DelFilmSource failed: %v", err)
	}

	// 验证结果:
	// 1. src1 独占的影片 1 应该被彻底清理
	var count1 int64
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 1).Count(&count1)
	if count1 != 0 {
		t.Fatalf("expected orphan film 1 to be deleted, got count=%d", count1)
	}

	// 2. 多源影片 2 应该依然存在 (因为还有 src2 的线路)
	var count2 int64
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 2).Count(&count2)
	if count2 != 1 {
		t.Fatalf("expected multi-source film 2 to be kept, got count=%d", count2)
	}

	// 3. 影片 2 在 src1 的线路应被删除，src2 的线路应保留
	var lineCount int64
	gdb.Model(&model.FilmSourcePlaylist{}).Where("mid = ? AND source_id = ?", 2, "src2").Count(&lineCount)
	if lineCount != 1 {
		t.Fatalf("expected src2 playlist to remain, got %d", lineCount)
	}
	var src1Raw int64
	var src1Map int64
	gdb.Model(&model.SourceCategory{}).Where("source_id = ?", "src1").Count(&src1Raw)
	gdb.Model(&model.CategoryMapping{}).Where("source_id = ?", "src1").Count(&src1Map)
	if src1Raw != 0 || src1Map != 0 {
		t.Fatalf("expected src1 categories to be deleted, raw=%d map=%d", src1Raw, src1Map)
	}
	var src2Raw int64
	var src2Map int64
	gdb.Model(&model.SourceCategory{}).Where("source_id = ?", "src2").Count(&src2Raw)
	gdb.Model(&model.CategoryMapping{}).Where("source_id = ?", "src2").Count(&src2Map)
	if src2Raw != 1 || src2Map != 1 {
		t.Fatalf("expected src2 category copy to remain, raw=%d map=%d", src2Raw, src2Map)
	}
}

func TestCollectService_UpdateFilmSource_CleanOldData(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)

	// 创建采集源 src1
	gdb.Create(&model.FilmSource{Id: "src1", Name: "旧源", Uri: "http://old-src.com", State: true})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 10},
		FilmIndexContent:  model.FilmIndexContent{Name: "旧源独占影片"},
	})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 10, SourceId: "src1", LineKind: "play", Content: "[]"})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "其他站", Uri: "http://other-src.com", State: true})
	gdb.Create(&model.Category{Id: 8, Pid: 0, Name: "旧分类", StableKey: "display:root:旧分类", Show: true})
	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "共用分类", StableKey: "display:root:共用分类", Show: true})
	gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 1, RawName: "旧分类"})
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 1, CategoryId: 8})
	gdb.Create(&model.CategoryMapping{SourceId: "src2", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.FailureRecord{OriginId: "src1", OriginName: "旧源", PageNumber: 3, Status: model.FailureRecordStatusPending})

	srv := new(CollectService)
	updateReq := model.FilmSource{
		Id:    "src1",
		Name:  "新源",
		Uri:   "http://new-src.com",
		State: true,
	}
	if err := srv.UpdateFilmSource(updateReq); err != nil {
		t.Fatalf("UpdateFilmSource failed: %v", err)
	}

	// 验证: 旧独占影片应该被清理，旧线路被清空
	var filmCount int64
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 10).Count(&filmCount)
	if filmCount != 0 {
		t.Fatalf("expected old film 10 to be cleaned, got count=%d", filmCount)
	}

	// 验证: 站点 URI 已成功更新
	var updatedSrc model.FilmSource
	gdb.Where("id = ?", "src1").First(&updatedSrc)
	if updatedSrc.Uri != "http://new-src.com" {
		t.Fatalf("expected uri http://new-src.com, got %s", updatedSrc.Uri)
	}

	var src1Raw int64
	var src1Map int64
	var failures int64
	gdb.Model(&model.SourceCategory{}).Where("source_id = ?", "src1").Count(&src1Raw)
	gdb.Model(&model.CategoryMapping{}).Where("source_id = ?", "src1").Count(&src1Map)
	gdb.Model(&model.FailureRecord{}).Where("origin_id = ?", "src1").Count(&failures)
	if src1Raw != 0 || src1Map != 0 || failures != 0 {
		t.Fatalf("expected source data cleared, raw=%d map=%d failures=%d", src1Raw, src1Map, failures)
	}
	var exclusive model.Category
	if err := gdb.First(&exclusive, 8).Error; err == nil {
		t.Fatalf("category only used by the changed source should be removed")
	}
	var shared model.Category
	if err := gdb.First(&shared, 9).Error; err != nil {
		t.Fatalf("category used by another source was deleted: %v", err)
	}
}
