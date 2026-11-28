package collect

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"server/internal/collect/ledger"
	"server/internal/collect/progress"
	"server/internal/model"
	"server/internal/utils"
)

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
		if b.sourceDroppedLocked(src.Id) {
			continue
		}
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
	if len(details) == 0 {
		return nil
	}
	detailJSON, err := json.Marshal(details)
	if err != nil {
		return err
	}
	targetMids := encodeTargetMids(b.replayFilmIDs)
	rec := &model.CollectRecord{
		BatchID:       b.batchID,
		ParentBatchID: b.parentBatchID,
		Trigger:       b.trigger,
		Tag:           b.tag,
		Hour:          b.hour,
		Status:        model.CollectRecordStatusRunning,
		SourceCount:   len(details),
		TargetMids:    targetMids,
		DetailJson:    string(detailJSON),
		StartedAt:     b.startedAt,
	}
	if err := ledger.CreateCollectRecord(rec); err != nil {
		return err
	}
	b.recordID = rec.ID
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

func (b *collectBatchContext) resolvedSourceHour(sourceID string) (int, bool) {
	if b == nil || strings.TrimSpace(sourceID) == "" {
		return 0, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	h, ok := b.sourceHours[sourceID]
	return h, ok && h != 0
}

// recordSourceStatusSnapshot 在站点提前结束（探页失败、派发/探页被停止等）时把明细锁定到批次快照，
// 避免随后收尾阶段超过进度保留期后，进度条目被 housekeeping 淘汰导致记录丢失或误判状态。
func recordSourceStatusSnapshot(batchCtx *collectBatchContext, fs model.FilmSource, status, errMsg string, readOwnedProgress bool) {
	if batchCtx == nil || strings.TrimSpace(fs.Id) == "" {
		return
	}
	name := strings.TrimSpace(fs.Name)
	if name == "" {
		name = fs.Id
	}
	detail := model.CollectRecordSourceDetail{
		SourceID:   fs.Id,
		SourceName: name,
		Status:     status,
		Hour:       batchCtx.sourceHour(fs.Id),
		Error:      strings.TrimSpace(errMsg),
	}
	if readOwnedProgress && batchCtx.ownsOccupy(fs.Id) {
		if snap, ok := progress.Snapshot(fs.Id); ok {
			detail.PageTotal = snap.Total
			detail.PageCurrent = snap.Success
			detail.SuccessCnt = snap.Success
			detail.FailedCnt = snap.Failed
			detail.FailedPages = copyFailedPages(snap.FailedPages)
			if snap.Status == progress.StatusStopping || snap.Status == progress.StatusStopped {
				detail.Status = progress.StatusStopped
				detail.Error = ""
			} else if status == "" && snap.Status != "" {
				detail.Status = snap.Status
			}
		}
	}
	if detail.Status == progress.StatusStopped {
		detail.Error = ""
	}
	batchCtx.recordSourceSnapshot(detail)
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

func (b *collectBatchContext) snapshotSourceFilmCounts() map[string]int {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]int, len(b.sourceFilmCounts))
	for sid, cnt := range b.sourceFilmCounts {
		if b.sourceDroppedLocked(sid) {
			continue
		}
		out[sid] = cnt
	}
	return out
}

func (b *collectBatchContext) targetMidsJSON() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return encodeTargetMids(b.replayFilmIDs)
}

func encodeTargetMids(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return ""
	}
	return string(raw)
}
