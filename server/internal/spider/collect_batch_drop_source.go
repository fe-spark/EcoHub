package spider

import (
	"sort"
	"strings"

	"server/internal/model"
)

// PauseActiveBatchRecords 停住包含该站的进行中批次的检查点和结算，执行删站清库，成功后再标记摘除。
// 先拿 recordMu 再拿批次锁，和 writeCheckpoint、结算同一顺序。
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

// dropSourceLocked 只记墓碑。读取和写入都跳过这个站，不再改各张明细表。
func (b *collectBatchContext) dropSourceLocked(sourceID string) {
	if b == nil || sourceID == "" || b.sourceDroppedLocked(sourceID) {
		return
	}
	if b.droppedSources == nil {
		b.droppedSources = map[string]struct{}{}
	}
	b.droppedSources[sourceID] = struct{}{}
}

func (b *collectBatchContext) listedFilmCountLocked() int {
	if b == nil {
		return 0
	}
	n := 0
	for id, set := range b.sourceFilmMIDs {
		if b.sourceDroppedLocked(id) {
			continue
		}
		n += len(set)
	}
	return n
}

func (b *collectBatchContext) midListedLocked(mid int64) bool {
	for id, set := range b.sourceFilmMIDs {
		if b.sourceDroppedLocked(id) {
			continue
		}
		if _, ok := set[mid]; ok {
			return true
		}
	}
	return false
}

func (b *collectBatchContext) notifyMidLiveLocked(mid int64) bool {
	ownedByDropped := false
	for id, set := range b.sourceNotifyMIDs {
		if _, ok := set[mid]; !ok {
			continue
		}
		if b.sourceDroppedLocked(id) {
			ownedByDropped = true
			continue
		}
		return true
	}
	return !ownedByDropped
}

func (b *collectBatchContext) withoutDroppedFilms(films map[string][]int64) map[string][]int64 {
	if b == nil || len(films) == 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.droppedSources) == 0 {
		return films
	}
	out := make(map[string][]int64, len(films))
	for id, mids := range films {
		if b.sourceDroppedLocked(id) || len(mids) == 0 {
			continue
		}
		out[id] = mids
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
