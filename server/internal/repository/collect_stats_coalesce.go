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
// 仅在有真实写库活动时 Note；结束时只 flush pending，不为空跑强行更新时间。
type collectStatsCoalescer struct {
	mu          sync.Mutex
	pending     map[string]time.Time // sourceID -> 最近活动时间
	lastFlushed map[string]time.Time
	minInterval time.Duration
}

var collectStats = &collectStatsCoalescer{
	pending:     make(map[string]time.Time),
	lastFlushed: make(map[string]time.Time),
	minInterval: collectStatsMinInterval(),
}

func collectStatsMinInterval() time.Duration {
	sec := config.CollectStatsFlushIntervalSec
	if sec <= 0 {
		sec = config.DefaultCollectStatsFlushIntervalSec
	}
	return time.Duration(sec) * time.Second
}

// NoteCollectSourceStats 记录「有数据写入」的采集站活动；距上次落盘超过最小间隔时才写库。
func NoteCollectSourceStats(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	now := time.Now()
	collectStats.mu.Lock()
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
