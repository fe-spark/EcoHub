package spider

import (
	"context"
	"encoding/json"
	"fmt"
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
				PageTotal:   snap.Total,
				PageCurrent: snap.Current,
				SuccessCnt:  snap.Success,
				FailedCnt:   snap.Failed,
				FilmsTotal:  len(b.sourceNotifyMIDs[src.Id]),
				Error:       errMsg,
			}
		} else {
			if films := len(b.sourceNotifyMIDs[src.Id]); films > detail.FilmsTotal {
				detail.FilmsTotal = films
			}
		}

		if detail.Status == progress.StatusDone || detail.Status == model.CollectRecordStatusSuccess {
			succSources++
		} else if detail.Status == progress.StatusFailed {
			failSources++
		}

		totalPage += detail.PageTotal
		succPage += detail.SuccessCnt
		failPage += detail.FailedCnt

		details = append(details, detail)
	}

	detailBytes, _ := json.Marshal(details)
	filmCount := len(b.notifyMIDs)
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
