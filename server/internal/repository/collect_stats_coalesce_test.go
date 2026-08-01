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
