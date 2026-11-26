package migration

import (
	"slices"
	"testing"

	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrateFilmIndexCidComposite(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}); err != nil {
		t.Fatalf("automigrate failed: %v", err)
	}

	// 模拟存量数据库仍保留旧列序 (cid, pid, update_stamp) 与 (cid, pid, hits)
	for _, stmt := range []string{
		"DROP INDEX IF EXISTS idx_cid_update",
		"DROP INDEX IF EXISTS idx_cid_hits",
		"CREATE INDEX idx_cid_update ON film_index(cid, pid, update_stamp)",
		"CREATE INDEX idx_cid_hits ON film_index(cid, pid, hits)",
	} {
		if err := gdb.Exec(stmt).Error; err != nil {
			t.Fatalf("prepare legacy index failed (%s): %v", stmt, err)
		}
	}

	if err := migrateFilmIndexCidComposite(gdb); err != nil {
		t.Fatalf("migrateFilmIndexCidComposite failed: %v", err)
	}
	// 再次执行应幂等
	if err := migrateFilmIndexCidComposite(gdb); err != nil {
		t.Fatalf("second migrateFilmIndexCidComposite failed: %v", err)
	}

	want := map[string][]string{
		"idx_cid_update": {"cid", "update_stamp", "mid"},
		"idx_cid_hits":   {"cid", "hits", "mid"},
	}
	for name, wantCols := range want {
		gotCols, exists, err := currentIndexColumns(gdb, true, model.TableFilmIndex, name)
		if err != nil {
			t.Fatalf("inspect %s failed: %v", name, err)
		}
		if !exists {
			t.Fatalf("expected index %s to exist", name)
		}
		if !slices.Equal(gotCols, wantCols) {
			t.Fatalf("index %s columns = %v, want %v", name, gotCols, wantCols)
		}
	}
}
