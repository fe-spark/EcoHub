package notify

import (
	"strings"
	"sync"
)

// MidAccumulator 按源统计变更次数；mid 明细写入 MySQL 批次表（无内存全量列表）。
type MidAccumulator struct {
	mu     sync.Mutex
	counts map[string]int
}

// Acc 全局累计器（仅计数；明细见 change_batch）。
var Acc = NewMidAccumulator()

func NewMidAccumulator() *MidAccumulator {
	return &MidAccumulator{counts: make(map[string]int)}
}

// Add 按源计数，并将 mid 写入指定变更批次（明细落 MySQL，全局去重）。
func (a *MidAccumulator) Add(batch *ChangeBatch, sourceID, sourceName string, mids ...int64) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(mids) == 0 {
		return
	}
	// 先落库（全局去重），再计数（按调用方传入去重）
	if batch != nil {
		batch.AppendMids(sourceName, mids...)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	seen := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		a.counts[sourceID]++
	}
}

// DrainSource 取出并清除某源计数；Films 列表为空（明细在 MySQL 批次中）。
func (a *MidAccumulator) DrainSource(sourceID string) (mids []int64, total int, truncated bool) {
	sourceID = strings.TrimSpace(sourceID)
	a.mu.Lock()
	defer a.mu.Unlock()
	total = a.counts[sourceID]
	delete(a.counts, sourceID)
	return nil, total, false
}

// ClearSources 清除指定源计数。
func (a *MidAccumulator) ClearSources(sourceIDs ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range sourceIDs {
		delete(a.counts, strings.TrimSpace(id))
	}
}
