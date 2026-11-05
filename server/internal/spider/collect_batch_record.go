package spider

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/progress"
	"server/internal/utils"
)

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

func formatReplayFilmIDs(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			parts = append(parts, strconv.FormatInt(id, 10))
		}
	}
	return strings.Join(parts, ",")
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
	rec := &model.CollectRecord{
		BatchID:       b.batchID,
		ParentBatchID: b.parentBatchID,
		Trigger:       b.trigger,
		Tag:           b.tag,
		Hour:          b.hour,
		FilmIDs:       formatReplayFilmIDs(b.replayFilmIDs),
		Status:        model.CollectRecordStatusRunning,
		SourceCount:   len(b.sources),
		StartedAt:     b.startedAt,
	}
	if err := repository.CreateCollectRecord(rec); err != nil {
		return err
	}
	b.recordStarted = true
	return nil
}

func (b *collectBatchContext) LiveProgress() *model.CollectRecordProgressPayload {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	totalPage := 0
	succPage := 0
	failPage := 0
	succSources := 0
	failSources := 0

	details := make([]model.CollectRecordSourceDetail, 0, len(b.sources))
	for _, src := range b.sources {
		detail, hasDetail := b.sourceSnapshots[src.Id]
		if !hasDetail {
			snap, ok := progress.Snapshot(src.Id)
			status := snap.Status
			errMsg := strings.TrimSpace(b.sourceErrors[src.Id])
			if !ok {
				if errMsg != "" {
					status = progress.StatusFailed
				} else {
					status = progress.StatusStarting
				}
			} else {
				status, errMsg = settleSourceStatus(status, errMsg, snap.Success)
			}

			name := strings.TrimSpace(src.Name)
			if name == "" {
				name = strings.TrimSpace(snap.Name)
			}
			if name == "" {
				name = src.Id
			}

			detail = model.CollectRecordSourceDetail{
				SourceID:    src.Id,
				SourceName:  name,
				Status:      status,
				Hour:        sourceHourLocked(b, src.Id),
				PageTotal:   snap.Total,
				PageCurrent: snap.Success,
				SuccessCnt:  snap.Success,
				FailedCnt:   snap.Failed,
				FilmsTotal:  len(b.sourceFilmMIDs[src.Id]),
				Error:       errMsg,
			}
		} else {
			if films := len(b.sourceFilmMIDs[src.Id]); films > detail.FilmsTotal {
				detail.FilmsTotal = films
			}
			if detail.Hour == 0 {
				detail.Hour = sourceHourLocked(b, src.Id)
			}
		}

		if detail.Status == progress.StatusDone || detail.Status == model.CollectRecordStatusSuccess {
			succSources++
		} else if detail.Status == progress.StatusFailed || detail.Status == model.CollectRecordStatusPartial {
			failSources++
		}

		totalPage += detail.PageTotal
		succPage += detail.SuccessCnt
		failPage += detail.FailedCnt

		details = append(details, detail)
	}

	detailBytes, _ := json.Marshal(details)
	filmCount := len(b.filmMIDs)
	durationMs := time.Since(b.startedAt).Milliseconds()

	return &model.CollectRecordProgressPayload{
		BatchID:        b.batchID,
		Status:         model.CollectRecordStatusRunning,
		DurationMs:     durationMs,
		FilmCount:      filmCount,
		PageTotal:      totalPage,
		PageSuccess:    succPage,
		PageFailed:     failPage,
		SuccessSources: succSources,
		FailedSources:  failSources,
		DetailJson:     string(detailBytes),
	}
}

const (
	recordFlushGap = 2 * time.Second
	recordFlushMin = 200
)

func (b *collectBatchContext) noteSourceHour(sourceID string, hour int) {
	if b == nil || strings.TrimSpace(sourceID) == "" || hour == 0 {
		return
	}
	b.mu.Lock()
	if b.sourceHours == nil {
		b.sourceHours = map[string]int{}
	}
	changed := b.sourceHours[sourceID] != hour
	b.sourceHours[sourceID] = hour
	if hour > b.hour {
		b.hour = hour
		changed = true
	}
	if changed {
		b.progressDirty = true
	}
	b.mu.Unlock()
	if changed {
		b.maybeFlushRecord()
	}
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
	return len(b.sourceFilmMIDs[sourceID])
}

func (b *collectBatchContext) batchHour() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hour
}

func (b *collectBatchContext) trackFilmsLocked(sourceID string, mids []int64) {
	if b.sourceFilmMIDs == nil {
		b.sourceFilmMIDs = map[string]map[int64]struct{}{}
	}
	if b.filmMIDs == nil {
		b.filmMIDs = map[int64]struct{}{}
	}
	if b.pendingFilms == nil {
		b.pendingFilms = map[string][]int64{}
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
		b.pendingFilms[sourceID] = append(b.pendingFilms[sourceID], mid)
	}
}

// TouchRecord 页有进展时尝试把已写入影片和页数落到运行中的记录。
func (b *collectBatchContext) TouchRecord() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.progressDirty = true
	b.mu.Unlock()
	b.maybeFlushRecord()
}

func (b *collectBatchContext) maybeFlushRecord() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if !b.recordStarted || b.flushing {
		b.mu.Unlock()
		return
	}
	pending := 0
	for _, ids := range b.pendingFilms {
		pending += len(ids)
	}
	due := pending >= recordFlushMin || time.Since(b.lastFlush) >= recordFlushGap
	if !due || (pending == 0 && !b.progressDirty) {
		b.mu.Unlock()
		return
	}
	films := b.drainPendingLocked()
	b.progressDirty = false
	b.flushing = true
	b.lastFlush = time.Now()
	b.flushWG.Add(1)
	b.mu.Unlock()
	go func() {
		defer b.flushWG.Done()
		defer func() {
			b.mu.Lock()
			b.flushing = false
			b.mu.Unlock()
		}()
		b.writeCheckpoint(films)
	}()
}

func (b *collectBatchContext) drainPendingLocked() map[string][]int64 {
	if len(b.pendingFilms) == 0 {
		return nil
	}
	out := b.pendingFilms
	b.pendingFilms = make(map[string][]int64)
	return out
}

func (b *collectBatchContext) flushRecordSync() map[string][]int64 {
	if b == nil {
		return nil
	}
	b.flushWG.Wait()
	b.mu.Lock()
	if !b.recordStarted {
		b.mu.Unlock()
		return nil
	}
	films := b.drainPendingLocked()
	b.progressDirty = false
	b.lastFlush = time.Now()
	b.mu.Unlock()
	return b.writeCheckpoint(films)
}

func (b *collectBatchContext) requeuePending(films map[string][]int64) {
	if b == nil || len(films) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pendingFilms == nil {
		b.pendingFilms = map[string][]int64{}
	}
	for sourceID, mids := range films {
		if sourceID == "" || len(mids) == 0 {
			continue
		}
		b.pendingFilms[sourceID] = append(b.pendingFilms[sourceID], mids...)
	}
}

// abandonUnstored 结算时丢掉最终仍没写上的编号，让影片数和清单行一致。
func (b *collectBatchContext) abandonUnstored(films map[string][]int64) (int, map[string]int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sourceID, mids := range films {
		drop := make(map[int64]int, len(mids))
		for _, mid := range mids {
			if mid > 0 {
				drop[mid]++
			}
		}
		pending := b.pendingFilms[sourceID]
		if len(pending) > 0 {
			kept := pending[:0]
			for _, mid := range pending {
				if drop[mid] > 0 {
					drop[mid]--
					continue
				}
				kept = append(kept, mid)
			}
			if len(kept) == 0 {
				delete(b.pendingFilms, sourceID)
			} else {
				b.pendingFilms[sourceID] = kept
			}
		}
		set := b.sourceFilmMIDs[sourceID]
		for _, mid := range mids {
			if mid > 0 {
				delete(set, mid)
			}
		}
	}
	next := make(map[int64]struct{})
	bySource := make(map[string]int, len(b.sourceFilmMIDs))
	for sourceID, set := range b.sourceFilmMIDs {
		bySource[sourceID] = len(set)
		for mid := range set {
			next[mid] = struct{}{}
		}
	}
	b.filmMIDs = next
	return len(next), bySource
}

func (b *collectBatchContext) writeCheckpoint(films map[string][]int64) map[string][]int64 {
	if b == nil || strings.TrimSpace(b.batchID) == "" {
		return nil
	}
	var missed map[string][]int64
	if len(films) > 0 {
		if err := repository.BatchSaveCollectRecordFilms(b.batchID, films); err != nil {
			log.Printf("[Spider] 保存采集批次影片清单失败 batchID=%s: %v", b.batchID, err)
			missed = repository.UnstoredCollectFilms(err)
			b.requeuePending(missed)
		}
	}
	payload := b.LiveProgress()
	if payload == nil {
		return missed
	}
	if err := repository.UpdateRunningCollectRecord(b.batchID, model.CollectRecordCompletion{
		Hour:           b.batchHour(),
		SuccessSources: payload.SuccessSources,
		FailedSources:  payload.FailedSources,
		FilmCount:      payload.FilmCount,
		PageTotal:      payload.PageTotal,
		PageSuccess:    payload.PageSuccess,
		PageFailed:     payload.PageFailed,
		DetailJson:     payload.DetailJson,
	}); err != nil {
		log.Printf("[Spider] 刷新运行中采集记录失败 batchID=%s: %v", b.batchID, err)
	}
	return missed
}

// GetActiveBatchesLiveProgress 批量查询指定批次当前运行中的实时进展快照
func GetActiveBatchesLiveProgress(batchIDs map[string]struct{}) []*model.CollectRecordProgressPayload {
	if len(batchIDs) == 0 {
		return nil
	}
	activeBatchesMu.Lock()
	defer activeBatchesMu.Unlock()

	out := make([]*model.CollectRecordProgressPayload, 0, len(batchIDs))
	for b := range activeBatches {
		if _, ok := batchIDs[b.batchID]; ok {
			if p := b.LiveProgress(); p != nil {
				out = append(out, p)
			}
		}
	}
	return out
}

var (
	recordCompletionMu          sync.RWMutex
	recordCompletionSubscribers = make(map[chan *model.CollectRecordProgressPayload]struct{})
)

// SubscribeRecordCompletion 订阅批次结算完成通知
func SubscribeRecordCompletion(ctx context.Context) <-chan *model.CollectRecordProgressPayload {
	ch := make(chan *model.CollectRecordProgressPayload, 16)
	recordCompletionMu.Lock()
	recordCompletionSubscribers[ch] = struct{}{}
	recordCompletionMu.Unlock()

	go func() {
		<-ctx.Done()
		recordCompletionMu.Lock()
		delete(recordCompletionSubscribers, ch)
		recordCompletionMu.Unlock()
		close(ch)
	}()
	return ch
}

// BroadcastRecordCompletion 广播批次终态更新事件
func BroadcastRecordCompletion(p *model.CollectRecordProgressPayload) {
	if p == nil {
		return
	}
	recordCompletionMu.RLock()
	defer recordCompletionMu.RUnlock()
	for ch := range recordCompletionSubscribers {
		select {
		case ch <- p:
		default:
		}
	}
}
