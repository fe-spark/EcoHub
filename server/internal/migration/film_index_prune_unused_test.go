package migration

import (
	"testing"

	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrateFilmIndexPruneUnused(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}); err != nil {
		t.Fatalf("automigrate failed: %v", err)
	}

	legacy := []string{
		"CREATE INDEX idx_film_index_db_id ON film_index(db_id)",
		"CREATE INDEX idx_film_index_area ON film_index(area)",
		"CREATE INDEX idx_film_index_language ON film_index(language)",
		"CREATE INDEX idx_film_index_year ON film_index(year)",
		"CREATE INDEX idx_film_index_collect_stamp ON film_index(collect_stamp)",
		"CREATE INDEX idx_filter_score ON film_index(cid, pid, area, language, year, score)",
		"CREATE INDEX idx_filter_update ON film_index(cid, pid, area, language, year, update_stamp)",
		"CREATE INDEX idx_filter_hits ON film_index(cid, pid, area, language, year, hits)",
	}
	for _, stmt := range legacy {
		if err := gdb.Exec(stmt).Error; err != nil {
			t.Fatalf("create legacy index failed: %v", err)
		}
	}

	if err := migrateFilmIndexPruneUnused(gdb); err != nil {
		t.Fatalf("migrateFilmIndexPruneUnused failed: %v", err)
	}
	if err := migrateFilmIndexPruneUnused(gdb); err != nil {
		t.Fatalf("second migrateFilmIndexPruneUnused failed: %v", err)
	}

	for _, dropped := range unusedFilmIndexes {
		if gdb.Migrator().HasIndex(&model.FilmIndex{}, dropped) {
			t.Fatalf("expected index %s to be dropped", dropped)
		}
	}
	for _, kept := range []string{"idx_root_key_upd_stamp", "idx_cat_key_upd_stamp", "idx_cid_update", "idx_film_index_pid_update_mid", "idx_film_index_name"} {
		if !gdb.Migrator().HasIndex(&model.FilmIndex{}, kept) {
			t.Fatalf("expected index %s to be kept", kept)
		}
	}
}
