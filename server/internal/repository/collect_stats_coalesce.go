package repository

import (
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
)

// collectStatsCoalescer 合并采集热路径中的 last_collect_time 写库。
// 有真实写库时 Note；定时采集整次成功结束（含 0 变更）再 Note。
// suppressed 源的 Note 被忽略，避免中途写入把「上次成功」提前到熔断/停止时刻。
type collectStatsCoalescer struct {
	mu          sync.Mutex
	pending     map[string]time.Time // sourceID -> 最近活动时间
	lastFlushed map[string]time.Time
	suppressed  map[string]struct{}
	minInterval time.Duration
}

var collectStats = &collectStatsCoalescer{
	pending:     make(map[string]time.Time),
	lastFlushed: make(map[string]time.Time),
	suppressed:  make(map[string]struct{}),
	minInterval: collectStatsMinInterval(),
}

func collectStatsMinInterval() time.Duration {
	sec := config.CollectStatsFlushIntervalSec
	if sec <= 0 {
		sec = config.DefaultCollectStatsFlushIntervalSec
	}
	return time.Duration(sec) * time.Second
}

// ResetCollectStatsCoalescer 清空采集统计合并缓冲。
// 数据重置（清空 collect_source_stats 表）时必须调用，避免内存 pending 的旧统计随后被回写复活。
func ResetCollectStatsCoalescer() {
	collectStats.mu.Lock()
	collectStats.pending = make(map[string]time.Time)
	collectStats.lastFlushed = make(map[string]time.Time)
	collectStats.suppressed = make(map[string]struct{})
	collectStats.mu.Unlock()
}

// SuppressCollectSourceStats 忽略该源后续 Note，直到 Unsuppress 或 DropPending。
// 定时采集开始后调用，避免热路径写库把 last_collect_time 提前到任务中途。
func SuppressCollectSourceStats(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	collectStats.mu.Lock()
	if collectStats.suppressed == nil {
		collectStats.suppressed = make(map[string]struct{})
	}
	collectStats.suppressed[sourceID] = struct{}{}
	collectStats.mu.Unlock()
}

// UnsuppressCollectSourceStats 恢复该源 Note（不自动写入）。
func UnsuppressCollectSourceStats(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	collectStats.mu.Lock()
	delete(collectStats.suppressed, sourceID)
	collectStats.mu.Unlock()
}

// DropCollectSourceStatsPending 丢弃该源未落盘的 pending，并解除 suppress。
func DropCollectSourceStatsPending(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	collectStats.mu.Lock()
	delete(collectStats.pending, sourceID)
	delete(collectStats.suppressed, sourceID)
	collectStats.mu.Unlock()
}

// NoteCollectSourceStats 记录「有数据写入」的采集站活动；距上次落盘超过最小间隔时才写库。
func NoteCollectSourceStats(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	now := time.Now()
	collectStats.mu.Lock()
	if _, held := collectStats.suppressed[sourceID]; held {
		collectStats.mu.Unlock()
		return
	}
	collectStats.pending[sourceID] = now
	last := collectStats.lastFlushed[sourceID]
	shouldFlush := last.IsZero() || now.Sub(last) >= collectStats.minInterval
	collectStats.mu.Unlock()
	if shouldFlush {
		FlushCollectSourceStats(sourceID)
	}
}

// FlushCollectSourceStats 仅 flush 已有 pending 的站点。
// 传入 sourceIDs 时只处理这些 id 中仍 pending 的；传入空则 flush 全部 pending。
// 不会为「无 pending」的站点凭空写 last_collect_time。
func FlushCollectSourceStats(sourceIDs ...string) {
	collectStats.mu.Lock()
	toFlush := make(map[string]time.Time)
	if len(sourceIDs) == 0 {
		for id, at := range collectStats.pending {
			toFlush[id] = at
		}
	} else {
		for _, id := range sourceIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if at, ok := collectStats.pending[id]; ok {
				toFlush[id] = at
			}
		}
	}
	for id := range toFlush {
		delete(collectStats.pending, id)
	}
	collectStats.mu.Unlock()

	if len(toFlush) == 0 {
		return
	}

	nowFlush := time.Now()
	for id, at := range toFlush {
		if err := TouchCollectSourceStatsTx(db.Mdb, id, at); err != nil {
			log.Printf("FlushCollectSourceStats source=%s err=%v", id, err)
			collectStats.mu.Lock()
			if _, exists := collectStats.pending[id]; !exists {
				collectStats.pending[id] = at
			}
			collectStats.mu.Unlock()
			continue
		}
		collectStats.mu.Lock()
		collectStats.lastFlushed[id] = nowFlush
		collectStats.mu.Unlock()
	}
}
