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

func setupResetServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
	return gdb
}

func TestSpiderService_ClearFilms_RequiresResetHooks(t *testing.T) {
	gdb := setupResetServiceTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "keep_src", Name: "保留站", Uri: "http://keep", State: true}).Error; err != nil {
		t.Fatal(err)
	}

	prevRecoverAccess := OnResetRecoverAccess
	prevRecoverCron := OnResetRecoverCrontab
	prevReinit := OnResetReinitSite
	t.Cleanup(func() {
		OnResetRecoverAccess = prevRecoverAccess
		OnResetRecoverCrontab = prevRecoverCron
		OnResetReinitSite = prevReinit
	})

	OnResetRecoverAccess = nil
	OnResetRecoverCrontab = nil
	OnResetReinitSite = nil

	if err := ResetSvc.ClearFilms(); err == nil {
		t.Fatal("expected error when reset hooks are nil")
	}
	var count int64
	if err := gdb.Model(&model.FilmSource{}).Where("id = ?", "keep_src").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("ClearFilms must not wipe database when hooks are nil, count=%d", count)
	}
}
