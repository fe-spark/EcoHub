package migration

import (
	"slices"
	"testing"

	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrateFilmIndexOptimize(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}); err != nil {
		t.Fatalf("automigrate failed: %v", err)
	}

	// 模拟存量数据库残留旧列序索引与废弃单列/超宽复合索引。
	for _, stmt := range []string{
		"DROP INDEX IF EXISTS idx_cid_update",
		"DROP INDEX IF EXISTS idx_cid_hits",
		"CREATE INDEX idx_cid_update ON film_index(cid, pid, update_stamp)",
		"CREATE INDEX idx_cid_hits ON film_index(cid, pid, hits)",
		"CREATE INDEX idx_film_index_db_id ON film_index(db_id)",
		"CREATE INDEX idx_film_index_area ON film_index(area)",
		"CREATE INDEX idx_film_index_language ON film_index(language)",
		"CREATE INDEX idx_film_index_year ON film_index(year)",
		"CREATE INDEX idx_film_index_collect_stamp ON film_index(collect_stamp)",
		"CREATE INDEX idx_filter_score ON film_index(cid, pid, area, language, year, score)",
		"CREATE INDEX idx_filter_update ON film_index(cid, pid, area, language, year, update_stamp)",
		"CREATE INDEX idx_filter_hits ON film_index(cid, pid, area, language, year, hits)",
	} {
		if err := gdb.Exec(stmt).Error; err != nil {
			t.Fatalf("prepare legacy index failed (%s): %v", stmt, err)
		}
	}

	if err := migrateFilmIndexOptimize(gdb); err != nil {
		t.Fatalf("migrateFilmIndexOptimize failed: %v", err)
	}
	if err := migrateFilmIndexOptimize(gdb); err != nil {
		t.Fatalf("second migrateFilmIndexOptimize failed: %v", err)
	}

	for _, dropped := range unusedFilmIndexes {
		if gdb.Migrator().HasIndex(&model.FilmIndex{}, dropped) {
			t.Fatalf("expected index %s to be dropped", dropped)
		}
	}
	for name, wantCols := range map[string][]string{
		"idx_cid_update": {"cid", "update_stamp", "mid"},
		"idx_cid_hits":   {"cid", "hits", "mid"},
	} {
		gotCols, exists, err := currentIndexColumns(gdb, true, model.TableFilmIndex, name)
		if err != nil || !exists || !slices.Equal(gotCols, wantCols) {
			t.Fatalf("index %s exists=%v cols=%v want=%v err=%v", name, exists, gotCols, wantCols, err)
		}
	}
	for _, kept := range []string{"idx_root_key_upd_stamp", "idx_cat_key_upd_stamp", "idx_film_index_pid_update_mid", "idx_film_index_name"} {
		if !gdb.Migrator().HasIndex(&model.FilmIndex{}, kept) {
			t.Fatalf("expected index %s to be kept", kept)
		}
	}
}
