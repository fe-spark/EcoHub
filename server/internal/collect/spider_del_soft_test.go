package collect

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 验证历史软删除的影片档案（deleted_at IS NOT NULL）也能被分批物理清理干净
func TestDelCollectResource_CleansSoftDeletedFilmsPhysically(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	origMdb := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = origMdb })
	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatal(err)
	}

	src := model.FilmSource{
		Id:    "src_soft_del_test",
		Name:  "SoftDelTest",
		Uri:   "http://soft.test",
		State: true,
	}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}

	film := &model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:           9999,
			FirstSourceId: src.Id,
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "已软删除影片",
		},
	}
	if err := gdb.Create(film).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Delete(film).Error; err != nil {
		t.Fatal(err)
	}

	var softCount int64
	gdb.Unscoped().Model(&model.FilmIndex{}).Where("mid = ?", 9999).Count(&softCount)
	if softCount != 1 {
		t.Fatalf("expected 1 soft deleted film, got %d", softCount)
	}

	if _, err := DelCollectResource(src.Id); err != nil {
		t.Fatalf("DelCollectResource failed: %v", err)
	}

	gdb.Unscoped().Model(&model.FilmIndex{}).Where("mid = ?", 9999).Count(&softCount)
	if softCount != 0 {
		t.Fatalf("soft deleted film must be physically deleted, remaining=%d", softCount)
	}
}
