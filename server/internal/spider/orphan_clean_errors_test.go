package spider

import (
	"fmt"
	"strings"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCleanGhostFilms_ReportsStepErrors(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// 不建 film_index，让两步查询都失败。
	if err := gdb.AutoMigrate(&model.FilmSourcePlaylist{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prevM, prevR := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	t.Cleanup(func() { db.Mdb, db.Rdb = prevM, prevR })

	m, p, err := cleanGhostFilms()
	if err == nil {
		t.Fatalf("expected error")
	}
	if m != 0 || p != 0 {
		t.Fatalf("cleaned m=%d p=%d, want 0", m, p)
	}
	msg := err.Error()
	for _, want := range []string{"清理空记录失败", "清理无线路幽灵影片失败"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestCleanOrphanData_ReportsStepError(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	prevM, prevR := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	t.Cleanup(func() { db.Mdb, db.Rdb = prevM, prevR })

	if _, _, err := cleanOrphanData(); err == nil || !strings.Contains(err.Error(), "清理孤儿播放列表失败") {
		t.Fatalf("unexpected err: %v", err)
	}
}
