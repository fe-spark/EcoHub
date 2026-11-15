package spider

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/progress"
	"server/internal/utils"
)

// BatchSourceLiveDetail 这一批里该站此刻要展示的明细。只认本批的进度代次。
// 还在采集、限流、停止或待检索时，用实时进度的页数。已经结束则用本批记下的部数和错误。
func BatchSourceLiveDetail(batchID, sourceID string) (model.CollectRecordSourceDetail, bool) {
	batchID = strings.TrimSpace(batchID)
	sourceID = strings.TrimSpace(sourceID)
	if batchID == "" || sourceID == "" {
		return model.CollectRecordSourceDetail{}, false
	}
	activeBatchesMu.Lock()
	var batch *collectBatchContext
	for b := range activeBatches {
		if b != nil && b.batchID == batchID {
			batch = b
			break
		}
	}
	activeBatchesMu.Unlock()
	if batch == nil {
		return model.CollectRecordSourceDetail{}, false
	}
	batch.mu.Lock()
	stored, hasStored := batch.sourceSnapshots[sourceID]
	batch.mu.Unlock()
	if epoch, ok := batch.progressEpoch(sourceID); ok && progress.Epoch(sourceID) == epoch {
		if snap, ok := progress.Snapshot(sourceID); ok && snap.Status != "" {
			if isLiveActivityStatus(snap.Status) {
				detail := stored
				if !hasStored {
					detail.SourceID = sourceID
				}
				detail.Status = snap.Status
				detail.PageTotal = snap.Total
				detail.PageCurrent = snap.Current
				detail.SuccessCnt = snap.Success
				detail.FailedCnt = snap.Failed
				detail.FailedPages = append([]int(nil), snap.FailedPages...)
				return detail, true
			}
		}
	}
	if hasStored && stored.Status != "" {
		return stored, true
	}
	return model.CollectRecordSourceDetail{}, false
}

func isLiveActivityStatus(status string) bool {
	switch status {
	case progress.StatusRunning, progress.StatusStarting, "waiting", progress.StatusPaused, progress.StatusStopping, progress.StatusSealing:
		return true
	default:
		return false
	}
}

func copyFailedPages(pages []int) []int {
	if len(pages) == 0 {
		return nil
	}
	return append([]int(nil), pages...)
}

func generateBatchID(startedAt time.Time) string {
	salt, err := utils.RandomString(4)
	if err != nil || salt == "" {
		salt = fmt.Sprintf("%08x", time.Now().UnixNano()%0xFFFFFFFF)
	}
	return fmt.Sprintf("c_%s_%s", startedAt.Format("20060102150405"), salt)
}

func (b *collectBatchContext) withHour(h int) *collectBatchContext {
	if b != nil {
		b.hour = h
	}
	return b
}

func (b *collectBatchContext) withReplayFilmIDs(ids []int64) *collectBatchContext {
	if b == nil || len(ids) == 0 {
		return b
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	b.replayFilmIDs = out
	return b
}

func (b *collectBatchContext) withParentBatchID(parentID string) *collectBatchContext {
	if b != nil {
		b.parentBatchID = parentID
	}
	return b
}

func (b *collectBatchContext) recordStart() error {
	if b == nil || len(b.sources) == 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.recordStarted {
		return nil
	}
	details := make([]model.CollectRecordSourceDetail, 0, len(b.sources))
	for _, src := range b.sources {
		name := strings.TrimSpace(src.Name)
		if name == "" {
			name = src.Id
		}
		details = append(details, model.CollectRecordSourceDetail{
			SourceID:   src.Id,
			SourceName: name,
			Status:     model.CollectRecordStatusRunning,
			Hour:       sourceHourLocked(b, src.Id),
		})
	}
	detailJSON, err := json.Marshal(details)
	if err != nil {
		return err
	}
	rec := &model.CollectRecord{
		BatchID:       b.batchID,
		ParentBatchID: b.parentBatchID,
		Trigger:       b.trigger,
		Tag:           b.tag,
		Hour:          b.hour,
		Status:        model.CollectRecordStatusRunning,
		SourceCount:   len(b.sources),
		DetailJson:    string(detailJSON),
		StartedAt:     b.startedAt,
	}
	if err := repository.CreateCollectRecord(rec); err != nil {
		return err
	}
	b.recordStarted = true
	return nil
}

func (b *collectBatchContext) noteSourceHour(sourceID string, hour int) {
	if b == nil || strings.TrimSpace(sourceID) == "" || hour == 0 {
		return
	}
	b.mu.Lock()
	if b.sourceHours == nil {
		b.sourceHours = map[string]int{}
	}
	b.sourceHours[sourceID] = hour
	// 批次 hour 保持配置跨度。各站实际窗口只写在明细 hour 上，列表才对得上点选的档位。
	b.mu.Unlock()
}

func (b *collectBatchContext) sourceHour(sourceID string) int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return sourceHourLocked(b, sourceID)
}

func sourceHourLocked(b *collectBatchContext, sourceID string) int {
	if b == nil {
		return 0
	}
	if h, ok := b.sourceHours[sourceID]; ok && h != 0 {
		return h
	}
	return b.hour
}

func (b *collectBatchContext) sourceFilmCount(sourceID string) int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sourceDroppedLocked(sourceID) {
		return 0
	}
	return len(b.sourceFilmMIDs[sourceID])
}

func (b *collectBatchContext) trackFilmsLocked(sourceID string, mids []int64) {
	if b.sourceDroppedLocked(sourceID) {
		return
	}
	if b.sourceFilmMIDs == nil {
		b.sourceFilmMIDs = map[string]map[int64]struct{}{}
	}
	if b.filmMIDs == nil {
		b.filmMIDs = map[int64]struct{}{}
	}
	set := b.sourceFilmMIDs[sourceID]
	if set == nil {
		set = map[int64]struct{}{}
		b.sourceFilmMIDs[sourceID] = set
	}
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := set[mid]; ok {
			continue
		}
		set[mid] = struct{}{}
		b.filmMIDs[mid] = struct{}{}
	}
}

// runningFilmRowsLocked 已入库的影片行数。同一影片在多站各算一行。调用方须持有 b.mu。
func (b *collectBatchContext) runningFilmRowsLocked() int {
	rows := 0
	for sourceID, set := range b.sourceFilmMIDs {
		if b.sourceDroppedLocked(sourceID) {
			continue
		}
		rows += len(set)
	}
	return rows
}

// ledgerFilmsLocked 这一批要写入清单的编号。调用方须持有 b.mu。
// 普通批次是实际入库的编号。单片批次只存要重放的编号：有入库的站只挂自己的，一个站都没入库时只挂请求过的站。
func (b *collectBatchContext) ledgerFilmsLocked() map[string][]int64 {
	if len(b.replayFilmIDs) > 0 {
		return b.singleReplayLedgerLocked()
	}
	return b.storedFilmsLocked()
}

func (b *collectBatchContext) storedFilmsLocked() map[string][]int64 {
	films := make(map[string][]int64, len(b.sourceFilmMIDs))
	for sourceID, set := range b.sourceFilmMIDs {
		if b.sourceDroppedLocked(sourceID) || len(set) == 0 {
			continue
		}
		mids := make([]int64, 0, len(set))
		for mid := range set {
			mids = append(mids, mid)
		}
		films[sourceID] = mids
	}
	if len(films) == 0 {
		return nil
	}
	return films
}

func (b *collectBatchContext) singleReplayLedgerLocked() map[string][]int64 {
	films := make(map[string][]int64)
	for sourceID, set := range b.sourceFilmMIDs {
		if b.sourceDroppedLocked(sourceID) || len(set) == 0 {
			continue
		}
		kept := make([]int64, 0, len(b.replayFilmIDs))
		for _, mid := range b.replayFilmIDs {
			if _, ok := set[mid]; ok {
				kept = append(kept, mid)
			}
		}
		if len(kept) > 0 {
			films[sourceID] = kept
		}
	}
	if len(films) > 0 {
		return films
	}
	for _, src := range b.sources {
		if b.sourceDroppedLocked(src.Id) || strings.TrimSpace(src.Id) == "" {
			continue
		}
		if _, attempted := b.sourceSnapshots[src.Id]; !attempted {
			continue
		}
		films[src.Id] = append([]int64(nil), b.replayFilmIDs...)
	}
	if len(films) == 0 {
		return nil
	}
	return films
}

func (b *collectBatchContext) collectRecordFilms() map[string][]int64 {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.recordStarted {
		return nil
	}
	return b.ledgerFilmsLocked()
}

func (b *collectBatchContext) saveRecordFilms(films map[string][]int64) map[string][]int64 {
	if b == nil || strings.TrimSpace(b.batchID) == "" {
		return nil
	}
	b.recordMu.Lock()
	defer b.recordMu.Unlock()
	return b.saveRecordFilmsLocked(films)
}

func (b *collectBatchContext) saveRecordFilmsLocked(films map[string][]int64) map[string][]int64 {
	films = b.withoutDroppedFilms(films)
	var missed map[string][]int64
	if len(films) > 0 {
		if err := repository.BatchSaveCollectRecordFilms(b.batchID, films); err != nil {
			log.Printf("[Spider] 保存采集批次影片清单失败 batchID=%s: %v", b.batchID, err)
			missed = repository.UnstoredCollectFilms(err)
		}
	}
	return missed
}
