package collect

import (
	"fmt"
	"log"
	"server/internal/collect/progress"
	filmcache "server/internal/film/cache"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/model"
	"sort"
	"strings"
	"sync"
)

func (b *collectBatchContext) releaseSourceOccupy(id string) {
	id = collectSourceID(id)
	if id == "" {
		return
	}
	if b == nil {
		releaseCollectSourceIDs(id)
		return
	}
	b.mu.Lock()
	tok, hasTok := b.occupyTokens[id]
	delete(b.occupyTokens, id)
	b.mu.Unlock()
	if hasTok && tok > 0 {
		releaseCollectTokens(map[string]uint64{id: tok})
	} else {
		releaseCollectSourceIDs(id)
	}
}

// withClaim 用占用那一刻记下的令牌和进度代次，替换建批次时现读的值。
func (b *collectBatchContext) withClaim(claim CollectClaim) *collectBatchContext {
	if b == nil {
		return b
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, tok := range claim.tokens {
		b.occupyTokens[id] = tok
	}
	for id, epoch := range claim.epochs {
		b.progressEpochs[id] = epoch
	}
	if id := strings.TrimSpace(claim.cronTaskID); id != "" {
		b.cronTaskID = id
	}
	return b
}

// markSourceFailed 把本批次这一代的进度标成失败，返回进度是否仍属于本批次。
// 无批次或没记代次时按站点直接改。
func (b *collectBatchContext) markSourceFailed(id string) bool {
	if epoch, ok := b.progressEpoch(id); ok {
		return progress.MarkFailedIfEpoch(id, epoch)
	}
	progress.Update(id, func(cur *model.CollectProgress) {
		switch cur.Status {
		case progress.StatusStarting, progress.StatusRunning:
			cur.Status = progress.StatusFailed
		}
	})
	return true
}

// setSourcePageTotal 只写本批次这一代的总页，返回进度是否仍属于本批次。
func (b *collectBatchContext) setSourcePageTotal(id string, pageCount int) bool {
	if epoch, ok := b.progressEpoch(id); ok {
		return progress.SetPageTotalIfEpoch(id, epoch, pageCount)
	}
	progress.SetPageTotal(id, pageCount)
	return true
}

// progressEpoch 建批次时该站的进度代次。
func (b *collectBatchContext) progressEpoch(id string) (uint64, bool) {
	if b == nil {
		return 0, false
	}
	id = collectSourceID(id)
	b.mu.Lock()
	defer b.mu.Unlock()
	epoch, ok := b.progressEpochs[id]
	return epoch, ok
}

func snapshotProgressEpochs(sources []model.FilmSource) map[string]uint64 {
	epochs := make(map[string]uint64, len(sources))
	for _, source := range sources {
		id := collectSourceID(source.Id)
		if id == "" {
			continue
		}
		epochs[id] = progress.Epoch(id)
	}
	return epochs
}

// ownsOccupy 当前占用令牌仍是本批次抢到的那一个。停止后被新采集重新占用时为 false。
func (b *collectBatchContext) ownsOccupy(id string) bool {
	if b == nil {
		return false
	}
	id = collectSourceID(id)
	b.mu.Lock()
	tok, ok := b.occupyTokens[id]
	b.mu.Unlock()
	if !ok {
		return false
	}
	collectingSourcesMu.Lock()
	cur, exists := collectingSources[id]
	collectingSourcesMu.Unlock()
	return exists && cur == tok
}

func (b *collectBatchContext) markSourceFinished(source model.FilmSource) {
	if b == nil {
		return
	}
	source.Id = strings.TrimSpace(source.Id)
	if source.Id == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.finishedSources[source.Id] = source
}

func (b *collectBatchContext) recordSourceSnapshot(detail model.CollectRecordSourceDetail) {
	if b == nil || strings.TrimSpace(detail.SourceID) == "" {
		return
	}
	b.mu.Lock()
	b.sourceSnapshots[detail.SourceID] = detail
	b.mu.Unlock()
}

func (b *collectBatchContext) getSourceSnapshot(sourceID string) (model.CollectRecordSourceDetail, bool) {
	if b == nil {
		return model.CollectRecordSourceDetail{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.sourceSnapshots[sourceID]
	return d, ok
}

// takeSourceMIDs 取出这一站尚未封口的影片编号，其它站的编号留在批次里。
func (b *collectBatchContext) takeSourceMIDs(sourceID string) []int64 {
	if b == nil {
		return nil
	}
	sourceID = strings.TrimSpace(sourceID)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sourceDroppedLocked(sourceID) {
		return nil
	}
	set := b.sourceFilmMIDs[sourceID]
	if len(set) == 0 || len(b.affectedMIDs) == 0 {
		return nil
	}
	taken := make([]int64, 0, len(set))
	seen := make(map[int64]struct{}, len(set))
	kept := make([]int64, 0, len(b.affectedMIDs))
	for _, mid := range b.affectedMIDs {
		if _, ok := set[mid]; !ok {
			kept = append(kept, mid)
			continue
		}
		if _, dup := seen[mid]; dup {
			continue
		}
		seen[mid] = struct{}{}
		taken = append(taken, mid)
	}
	b.affectedMIDs = kept
	return taken
}

// sealFinishedSource 刷窗后把这一站送进全局封口队列。当时它是主站才预热首页。
func (b *collectBatchContext) sealFinishedSource(source model.FilmSource) error {
	if b == nil {
		return nil
	}
	source.Id = strings.TrimSpace(source.Id)
	if source.Id == "" {
		return nil
	}
	collectLifecycle.beginPublish()
	defer collectLifecycle.endPublish()
	publishMu.Lock()
	defer publishMu.Unlock()
	if _, err := drainStreamPublishLocked(true, false); err != nil {
		return err
	}
	mids := b.takeSourceMIDs(source.Id)
	if !isPreferredCollectSource(source.Id) {
		mids = nil
	}
	schedulePrimarySearchReseal(mids)
	filmcache.ClearTVBoxConfigCache()
	b.warmPrimaryHome(source.Id)
	return nil
}

func (b *collectBatchContext) warmPrimaryHome(sourceID string) {
	b.mu.Lock()
	if !isPreferredCollectSource(sourceID) || b.primaryWarmedUp {
		b.mu.Unlock()
		return
	}
	b.primaryWarmedUp = true
	b.mu.Unlock()
	filmsnapshot.FlushSnapshotCacheInvalidation()
}

func (b *collectBatchContext) flushAndFinalize() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	hasWork := len(b.finishedSources) > 0 || len(b.affectedMIDs) > 0 || streamBuf.pendingCount() > 0
	warmupHome := false
	if !b.primaryWarmedUp {
		for id := range b.finishedSources {
			if isPreferredCollectSource(id) {
				warmupHome = true
				b.primaryWarmedUp = true
				break
			}
		}
	}
	b.finishedSources = make(map[string]model.FilmSource)
	b.mu.Unlock()
	if !hasWork {
		return nil
	}

	collectLifecycle.beginPublish()
	defer collectLifecycle.endPublish()
	publishMu.Lock()
	defer publishMu.Unlock()

	return finalizeStreamPublish(b.takeAffectedMIDs, warmupHome)
}

func (b *collectBatchContext) close() {
	if b == nil {
		return
	}
	unregisterActiveBatch(b)
	b.mu.Lock()
	tokens := b.occupyTokens
	b.occupyTokens = nil
	b.mu.Unlock()
	releaseCollectTokens(tokens)
}

func (b *collectBatchContext) getTargetPages(sourceID string) []int {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sourceTargetPages == nil {
		return nil
	}
	return b.sourceTargetPages[sourceID]
}

func (b *collectBatchContext) withTargetPages(targetPages map[string][]int) *collectBatchContext {
	if b == nil || len(targetPages) == 0 {
		return b
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sourceTargetPages = make(map[string][]int, len(targetPages))
	for k, v := range targetPages {
		b.sourceTargetPages[k] = append([]int(nil), v...)
	}
	return b
}

func publishFilmSnapshot(affectedMIDs []int64) (string, error) {
	mids := normalizeAffectedMIDs(affectedMIDs)
	if len(mids) == 0 {
		if hasSnapshot, err := filmsnapshot.HasPublishedFilmListSnapshot(); err != nil {
			return "", err
		} else if !hasSnapshot {
			return "", nil
		}
	}
	version, _, err := filmsnapshot.TouchActiveSnapshotsByMids(mids...)
	if err != nil {
		return "", fmt.Errorf("upsert film list snapshot failed: %w", err)
	}
	return version, nil
}

func normalizeAffectedMIDs(mids []int64) []int64 {
	if len(mids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(mids))
	res := make([]int64, 0, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		res = append(res, mid)
	}
	sort.Slice(res, func(i, j int) bool { return res[i] < res[j] })
	return res
}

var (
	collectDoneHooksMu sync.RWMutex
	collectDoneHooks   []func()
)

// RegisterCollectDoneHook 注册采集批次全部完成后的全局回调
func RegisterCollectDoneHook(hook func()) {
	if hook == nil {
		return
	}
	collectDoneHooksMu.Lock()
	defer collectDoneHooksMu.Unlock()
	collectDoneHooks = append(collectDoneHooks, hook)
}

func triggerCollectDoneHooks() {
	collectDoneHooksMu.RLock()
	hooks := make([]func(), len(collectDoneHooks))
	copy(hooks, collectDoneHooks)
	collectDoneHooksMu.RUnlock()

	for _, fn := range hooks {
		if fn != nil {
			go func(f func()) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[Spider] collectDoneHook panic: %v", r)
					}
				}()
				f()
			}(fn)
		}
	}
}
