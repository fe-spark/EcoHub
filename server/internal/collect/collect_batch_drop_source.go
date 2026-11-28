package collect

import (
	"sort"
	"strings"

	"server/internal/model"
)

// PauseActiveBatchRecords 停住包含该站的进行中批次的流式落账与结算，执行删站清库，成功后再标记摘除。
// 先拿 recordMu 再拿批次锁，与流式刷账和结算同一顺序。
func PauseActiveBatchRecords(sourceID string, fn func() error) error {
	if fn == nil {
		return nil
	}
	sourceID = strings.TrimSpace(sourceID)
	batches := activeBatchesWithSource(sourceID)
	for _, b := range batches {
		b.recordMu.Lock()
	}
	defer func() {
		for i := len(batches) - 1; i >= 0; i-- {
			batches[i].recordMu.Unlock()
		}
	}()
	if err := fn(); err != nil {
		return err
	}
	for _, b := range batches {
		b.mu.Lock()
		b.dropSourceLocked(sourceID)
		b.mu.Unlock()
	}
	return nil
}

func activeBatchesWithSource(sourceID string) []*collectBatchContext {
	activeBatchesMu.Lock()
	all := make([]*collectBatchContext, 0, len(activeBatches))
	for b := range activeBatches {
		all = append(all, b)
	}
	activeBatchesMu.Unlock()
	sort.Slice(all, func(i, j int) bool { return all[i].seq < all[j].seq })
	if sourceID == "" {
		return nil
	}
	hit := make([]*collectBatchContext, 0, 1)
	for _, b := range all {
		b.mu.Lock()
		ok := b.hasSourceLocked(sourceID)
		b.mu.Unlock()
		if ok {
			hit = append(hit, b)
		}
	}
	return hit
}

func (b *collectBatchContext) hasSourceLocked(sourceID string) bool {
	if b == nil || sourceID == "" {
		return false
	}
	for _, src := range b.sources {
		if strings.TrimSpace(src.Id) == sourceID {
			return true
		}
	}
	return false
}

func (b *collectBatchContext) sourceDroppedLocked(sourceID string) bool {
	if b == nil || sourceID == "" {
		return false
	}
	_, ok := b.droppedSources[strings.TrimSpace(sourceID)]
	return ok
}

// dropSourceLocked 记墓碑并同步剔除缓冲中未刷库的该站数据。调用方须持有 b.mu。
func (b *collectBatchContext) dropSourceLocked(sourceID string) {
	if b == nil || sourceID == "" || b.sourceDroppedLocked(sourceID) {
		return
	}
	if b.droppedSources == nil {
		b.droppedSources = map[string]struct{}{}
	}
	b.droppedSources[sourceID] = struct{}{}
	delete(b.pendingSealMIDs, sourceID)
	delete(b.sourceFilmCounts, sourceID)
	delete(b.sourceFilmSets, sourceID)
}

func (b *collectBatchContext) withoutDroppedResults(results []model.SourceNotifyResult) ([]model.SourceNotifyResult, bool) {
	if b == nil || len(results) == 0 {
		return results, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.droppedSources) == 0 {
		return results, false
	}
	out := make([]model.SourceNotifyResult, 0, len(results))
	removed := false
	for _, result := range results {
		if b.sourceDroppedLocked(result.SourceID) {
			removed = true
			continue
		}
		out = append(out, result)
	}
	return out, removed
}
