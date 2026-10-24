package repository

import (
	"fmt"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestNoteCollectSourceStatsCoalescesPending(t *testing.T) {
	// 重置全局 coalescer 状态，避免污染。
	collectStats.mu.Lock()
	collectStats.pending = make(map[string]time.Time)
	collectStats.lastFlushed = make(map[string]time.Time)
	collectStats.minInterval = time.Hour // 拉大间隔，避免测试里写库
	// 假装刚 flush 过，后续 Note 只进 pending。
	collectStats.lastFlushed["src-a"] = time.Now()
	collectStats.mu.Unlock()

	NoteCollectSourceStats("src-a")
	NoteCollectSourceStats("src-a")
	NoteCollectSourceStats("src-a")

	collectStats.mu.Lock()
	defer collectStats.mu.Unlock()
	if _, ok := collectStats.pending["src-a"]; !ok {
		t.Fatal("expected src-a pending after note within interval")
	}
	// 间隔内不应再触发 flush 清空 pending
	if at, ok := collectStats.pending["src-a"]; !ok || at.IsZero() {
		t.Fatalf("pending timestamp missing: %v", at)
	}
}

func TestSuppressCollectSourceStatsIgnoresNote(t *testing.T) {
	ResetCollectStatsCoalescer()
	collectStats.mu.Lock()
	collectStats.minInterval = time.Hour
	collectStats.lastFlushed["src-hold"] = time.Now()
	collectStats.mu.Unlock()

	SuppressCollectSourceStats("src-hold")
	NoteCollectSourceStats("src-hold")

	collectStats.mu.Lock()
	_, pending := collectStats.pending["src-hold"]
	collectStats.mu.Unlock()
	if pending {
		t.Fatal("suppressed source should not pending")
	}

	UnsuppressCollectSourceStats("src-hold")
	NoteCollectSourceStats("src-hold")
	collectStats.mu.Lock()
	_, pending = collectStats.pending["src-hold"]
	collectStats.mu.Unlock()
	if !pending {
		t.Fatal("after unsuppress, note should pending")
	}
}

func TestDropCollectSourceStatsPendingClearsHold(t *testing.T) {
	ResetCollectStatsCoalescer()
	collectStats.mu.Lock()
	collectStats.minInterval = time.Hour
	collectStats.lastFlushed["src-drop"] = time.Now()
	collectStats.mu.Unlock()

	NoteCollectSourceStats("src-drop")
	SuppressCollectSourceStats("src-drop")
	DropCollectSourceStatsPending("src-drop")

	collectStats.mu.Lock()
	_, pending := collectStats.pending["src-drop"]
	_, held := collectStats.suppressed["src-drop"]
	collectStats.mu.Unlock()
	if pending || held {
		t.Fatal("drop should clear pending and suppress")
	}
}

func TestNoteCollectSourceRunDoesNotMoveCronAnchor(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CollectSourceStats{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	anchor := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := gdb.Create(&model.CollectSourceStats{SourceId: "src-run", LastCollectTime: &anchor}).Error; err != nil {
		t.Fatal(err)
	}
	NoteCollectSourceRun("src-run", 123)

	gotAnchor, err := GetLastCollectTime("src-run")
	if err != nil {
		t.Fatal(err)
	}
	if gotAnchor == nil || !gotAnchor.Equal(anchor) {
		t.Fatalf("cron anchor = %v, want %v", gotAnchor, anchor)
	}
	runStat := GetCollectSourceRunStats([]string{"src-run"})["src-run"]
	runAt := runStat.LastRunTime
	if runStat.LastRunPageCount != 123 {
		t.Fatalf("last run page count = %d, want 123", runStat.LastRunPageCount)
	}
	if runAt == nil || runAt.IsZero() {
		t.Fatal("last run time missing")
	}

	onlyCron := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if err := gdb.Create(&model.CollectSourceStats{SourceId: "src-cron", LastCollectTime: &onlyCron}).Error; err != nil {
		t.Fatal(err)
	}
	if stat, ok := GetCollectSourceRunStats([]string{"src-cron"})["src-cron"]; ok && stat.LastRunTime != nil {
		t.Fatal("cron anchor was returned as last run time")
	}
}
