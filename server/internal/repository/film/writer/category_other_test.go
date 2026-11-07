package writer

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/support"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestConvertFilmIndex_UnmappedGoesToOther(t *testing.T) {
	openWriterCategoryDB(t)
	tree := &model.CategoryTree{
		Children: []*model.CategoryTree{{Id: 1, Name: "电影", Show: true}},
	}
	if err := repository.SaveCategoryTree("src_oscar", tree); err != nil {
		t.Fatalf("SaveCategoryTree: %v", err)
	}
	index, err := ConvertFilmIndex("src_oscar", model.MovieDetail{
		Id:              8,
		Cid:             99,
		RawCid:          99,
		Name:            "未映射片",
		MovieDescriptor: model.MovieDescriptor{CName: "冷门类", UpdateTime: "2026-11-07 12:00:00"},
	}, "v1", "r1")
	if err != nil {
		t.Fatalf("ConvertFilmIndex: %v", err)
	}
	wantKey := support.BuildSourceCategoryKey("src_oscar", model.OtherSourceTypeID)
	if index.Pid <= 0 || index.Cid <= 0 {
		t.Fatalf("pid/cid not bound: pid=%d cid=%d", index.Pid, index.Cid)
	}
	if index.CName != model.TagOthersName {
		t.Fatalf("c_name=%q", index.CName)
	}
	if index.RootCategoryKey != wantKey || index.CategoryKey != wantKey {
		t.Fatalf("keys=%s / %s", index.RootCategoryKey, index.CategoryKey)
	}
	if index.OriginalCategory != "冷门类" {
		t.Fatalf("original_category=%q", index.OriginalCategory)
	}
}

func TestConvertFilmIndex_UnmappedUsesCMS其它(t *testing.T) {
	openWriterCategoryDB(t)
	tree := &model.CategoryTree{
		Children: []*model.CategoryTree{
			{Id: 1, Name: "电影", Show: true},
			{Id: 20, Name: "其它", Show: true},
		},
	}
	if err := repository.SaveCategoryTree("src_alias", tree); err != nil {
		t.Fatalf("SaveCategoryTree: %v", err)
	}
	index, err := ConvertFilmIndex("src_alias", model.MovieDetail{
		Id:              9,
		Cid:             77,
		RawCid:          77,
		Name:            "未映射片",
		MovieDescriptor: model.MovieDescriptor{CName: "冷门类", UpdateTime: "2026-11-07 12:00:00"},
	}, "v1", "r1")
	if err != nil {
		t.Fatalf("ConvertFilmIndex: %v", err)
	}
	wantKey := support.BuildSourceCategoryKey("src_alias", 20)
	if index.CName != "其它" {
		t.Fatalf("c_name=%q", index.CName)
	}
	if index.RootCategoryKey != wantKey || index.CategoryKey != wantKey {
		t.Fatalf("keys=%s / %s", index.RootCategoryKey, index.CategoryKey)
	}
}

func TestConvertFilmIndex_HiddenOtherStillSkipped(t *testing.T) {
	gdb := openWriterCategoryDB(t)
	tree := &model.CategoryTree{
		Children: []*model.CategoryTree{{Id: 1, Name: "电影", Show: true}},
	}
	if err := repository.SaveCategoryTree("src_hide", tree); err != nil {
		t.Fatalf("SaveCategoryTree: %v", err)
	}
	if err := gdb.Model(&model.Category{}).
		Where("stable_key = ?", support.BuildSourceCategoryKey("src_hide", model.OtherSourceTypeID)).
		Update("show", false).Error; err != nil {
		t.Fatalf("hide 其他: %v", err)
	}
	support.ReloadMappingRules()
	_, err := ConvertFilmIndex("src_hide", model.MovieDetail{
		Id:              10,
		Cid:             99,
		RawCid:          99,
		Name:            "未映射片",
		MovieDescriptor: model.MovieDescriptor{CName: "冷门类", UpdateTime: "2026-11-07 12:00:00"},
	}, "v1", "r1")
	if err == nil || !errors.Is(err, ErrCategoryDisabled) {
		t.Fatalf("hidden 其他 should skip as disabled, err=%v", err)
	}
}

func TestSaveCollectedPeerDetails_UnmappedGoesToOther(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := &model.FilmSource{Id: "src-unmapped", Name: "奥斯卡", State: true}
	detail := testPeerDetail(88, "未映射片", "第1集")
	detail.Pid = 0
	detail.Cid = 99
	detail.RawPid = 0
	detail.RawCid = 99
	detail.CName = "冷门类"
	got, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{detail})
	if err != nil {
		t.Fatalf("SaveCollectedPeerDetails: %v", err)
	}
	if len(got.Affected) != 1 || got.Unmapped != 0 {
		t.Fatalf("affected=%v unmapped=%d", got.Affected, got.Unmapped)
	}
	var film model.FilmIndex
	if err := gdb.Where("name = ?", "未映射片").First(&film).Error; err != nil {
		t.Fatalf("film missing: %v", err)
	}
	wantKey := support.BuildSourceCategoryKey(source.Id, model.OtherSourceTypeID)
	if film.CName != model.TagOthersName || film.CategoryKey != wantKey {
		t.Fatalf("c_name=%s key=%s", film.CName, film.CategoryKey)
	}
}

func TestConvertFilmIndex_MappedFilmUnchanged(t *testing.T) {
	openWriterCategoryDB(t)
	tree := &model.CategoryTree{
		Children: []*model.CategoryTree{{Id: 1, Name: "电影", Show: true}},
	}
	if err := repository.SaveCategoryTree("src_ok", tree); err != nil {
		t.Fatalf("SaveCategoryTree: %v", err)
	}
	index, err := ConvertFilmIndex("src_ok", model.MovieDetail{
		Id:              1,
		Cid:             1,
		Pid:             1,
		RawCid:          1,
		RawPid:          1,
		Name:            "动作片",
		MovieDescriptor: model.MovieDescriptor{CName: "电影", UpdateTime: "2026-11-07 12:00:00"},
	}, "v1", "r1")
	if err != nil {
		t.Fatalf("ConvertFilmIndex: %v", err)
	}
	wantKey := support.BuildSourceCategoryKey("src_ok", 1)
	if index.CName != "电影" || index.CategoryKey != wantKey {
		t.Fatalf("mapped film rewritten: c_name=%s key=%s", index.CName, index.CategoryKey)
	}
}

func openWriterCategoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Category{}, &model.CategoryMapping{}, &model.SourceCategory{}, &model.MappingRule{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	db.Rdb = nil
	t.Cleanup(func() {
		db.Mdb = prev
		support.StoreMappingSnapshotForTest(nil)
	})
	return gdb
}
