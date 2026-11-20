package migration

import (
	"testing"

	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrateFilmIndexDedup(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}); err != nil {
		t.Fatalf("automigrate failed: %v", err)
	}

	// 模拟老版本残留的重复与残缺索引
	legacy := []string{
		"CREATE INDEX idx_root_key_update ON film_index(root_category_key)",
		"CREATE INDEX idx_category_key_latest ON film_index(category_key)",
		"CREATE INDEX idx_film_index_update_mid ON film_index(update_stamp)",
		"CREATE INDEX idx_pid_update ON film_index(cid, pid, update_stamp)",
		"CREATE INDEX idx_film_index_first_source_id ON film_index(first_source_id)",
	}
	for _, stmt := range legacy {
		if err := gdb.Exec(stmt).Error; err != nil {
			t.Fatalf("create legacy index failed: %v", err)
		}
	}

	if err := migrateFilmIndexDedup(gdb); err != nil {
		t.Fatalf("migrateFilmIndexDedup failed: %v", err)
	}
	// 再次执行应幂等
	if err := migrateFilmIndexDedup(gdb); err != nil {
		t.Fatalf("second migrateFilmIndexDedup failed: %v", err)
	}

	for _, dropped := range []string{"idx_root_key_update", "idx_category_key_latest", "idx_film_index_update_mid", "idx_pid_update", "idx_film_index_first_source_id"} {
		if gdb.Migrator().HasIndex(&model.FilmIndex{}, dropped) {
			t.Fatalf("expected index %s to be dropped", dropped)
		}
	}
	for _, kept := range []string{"idx_root_key_upd_stamp", "idx_cat_key_upd_stamp", "idx_cid_update"} {
		if !gdb.Migrator().HasIndex(&model.FilmIndex{}, kept) {
			t.Fatalf("expected index %s to be kept", kept)
		}
	}
}
