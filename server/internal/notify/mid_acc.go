package notify

import (
	"strings"
	"sync"
)

const maxMidsPerSource = 500

// MidAccumulator 按采集源累计本批 mid（有界，防全量 OOM）。
type MidAccumulator struct {
	mu        sync.Mutex
	bySource  map[string]map[int64]struct{}
	counts    map[string]int
	truncated map[string]bool
}

// Acc 全局 mid 累计器（随批次 Drain 清空）。
var Acc = NewMidAccumulator()

func NewMidAccumulator() *MidAccumulator {
	return &MidAccumulator{
		bySource:  make(map[string]map[int64]struct{}),
		counts:    make(map[string]int),
		truncated: make(map[string]bool),
	}
}

// Add 记录 source 下成功写入的 mid。
func (a *MidAccumulator) Add(sourceID string, mids ...int64) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(mids) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	set := a.bySource[sourceID]
	if set == nil {
		set = make(map[int64]struct{})
		a.bySource[sourceID] = set
	}
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := set[mid]; ok {
			continue
		}
		a.counts[sourceID]++
		if len(set) >= maxMidsPerSource {
			a.truncated[sourceID] = true
			continue
		}
		set[mid] = struct{}{}
	}
}

// DrainSource 取出并清除某源累计。
func (a *MidAccumulator) DrainSource(sourceID string) (mids []int64, total int, truncated bool) {
	sourceID = strings.TrimSpace(sourceID)
	a.mu.Lock()
	defer a.mu.Unlock()
	set := a.bySource[sourceID]
	total = a.counts[sourceID]
	truncated = a.truncated[sourceID]
	delete(a.bySource, sourceID)
	delete(a.counts, sourceID)
	delete(a.truncated, sourceID)
	if len(set) == 0 {
		return nil, total, truncated
	}
	mids = make([]int64, 0, len(set))
	for mid := range set {
		mids = append(mids, mid)
	}
	return mids, total, truncated
}

// ClearSources 清除指定源（批次结束后兜底）。
func (a *MidAccumulator) ClearSources(sourceIDs ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range sourceIDs {
		id = strings.TrimSpace(id)
		delete(a.bySource, id)
		delete(a.counts, id)
		delete(a.truncated, id)
	}
}
