package spider

import (
	"fmt"
	"strings"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestEnsureMasterCategoriesReady_RequiresSavedCategories(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CategoryMapping{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb

	source := &model.FilmSource{Id: "src_new", Name: "樱花", Uri: "http://example.invalid/api"}
	err = ensureMasterCategoriesReady(source)
	if err == nil || !strings.Contains(err.Error(), "还没有分类") {
		t.Fatalf("expected missing category error, got %v", err)
	}

	if err := gdb.Create(&model.CategoryMapping{SourceId: source.Id, SourceTypeId: 13, CategoryId: 90}).Error; err != nil {
		t.Fatalf("seed mapping: %v", err)
	}
	if err := ensureMasterCategoriesReady(source); err != nil {
		t.Fatalf("saved mapping should allow collect, got %v", err)
	}
}

func TestFilterSourcesReadyForCollect_SkipsMissingCategory(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CategoryMapping{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	if err := gdb.Create(&model.CategoryMapping{SourceId: "ready", SourceTypeId: 1, CategoryId: 9}).Error; err != nil {
		t.Fatalf("seed mapping: %v", err)
	}

	got := filterSourcesReadyForCollect([]model.FilmSource{
		{Id: "ready", Name: "有分类"},
		{Id: "missing", Name: "缺分类"},
	})
	if len(got) != 1 || got[0].Id != "ready" {
		t.Fatalf("expected only the source with categories, got %+v", got)
	}
}
