package repository

import (
	"testing"
	"time"
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
