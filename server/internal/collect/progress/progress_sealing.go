package progress

import (
	"context"
	"server/internal/model"
	"strings"
	"time"
)

func MarkSourceSealingIfRun(ctx context.Context, sourceID string) bool {
	return markSealing(ctx, sourceID, StatusDone)
}

// MarkStoppedAsSealing 用户停止后也先进待检索，封口周期结束时收成已停止。
func MarkStoppedAsSealing(ctx context.Context, sourceID string) bool {
	return markSealing(ctx, sourceID, StatusStopped)
}

func markSealing(ctx context.Context, sourceID, outcome string) bool {
	epoch, ok := ctx.Value(runEpochKey{}).(uint64)
	if !ok {
		return false
	}
	val, exists := store.Load(sourceID)
	if !exists {
		return false
	}
	state := val.(*progressState)
	state.mu.Lock()
	if state.epoch != epoch {
		state.mu.Unlock()
		return false
	}
	switch state.data.Status {
	case StatusRunning, StatusStarting, StatusStopping, StatusPaused:
	default:
		state.mu.Unlock()
		return false
	}
	if outcome == StatusDone && state.data.Status == StatusStopping {
		state.mu.Unlock()
		return false
	}
	state.data.Status = StatusSealing
	state.sealOutcome = outcome
	state.updated = time.Now()
	state.mu.Unlock()
	NotifyChange()
	FlushHotpathSideEffects(sourceID)
	return true
}

// MarkSealed 封口周期结束，把等检索的站点收成最终状态。已经重开或失败的保持原状。
func MarkSealed(sourceIDs []string) {
	finishSealing(sourceIDs, false)
}

// MarkSealFailed 检索索引重建连续失败达到上限后，释放待检索站点并标为失败。
func MarkSealFailed(sourceIDs []string) {
	finishSealing(sourceIDs, true)
}

func finishSealing(sourceIDs []string, failed bool) {
	now := time.Now()
	changed := false
	for _, sourceID := range sourceIDs {
		val, ok := store.Load(sourceID)
		if !ok {
			continue
		}
		state := val.(*progressState)
		state.mu.Lock()
		if state.data.Status == StatusSealing {
			if failed {
				state.data.Status = StatusFailed
				state.data.Sealed = false
			} else {
				if state.sealOutcome == StatusStopped {
					state.data.Status = StatusStopped
				} else {
					state.data.Status = StatusDone
				}
				state.data.Sealed = true
			}
			state.sealOutcome = ""
			state.updated = now
			changed = true
		}
		state.mu.Unlock()
	}
	if changed {
		NotifyChange()
	}
}

func IsSealing(sourceID string) bool {
	if progress, ok := Snapshot(sourceID); ok {
		return progress.Status == StatusSealing
	}
	return false
}

func FlushHotpathSideEffects(sourceIDs ...string) {
	if statsFlusher == nil {
		return
	}
	if len(sourceIDs) == 0 {
		statsFlusher()
		return
	}
	statsFlusher(sourceIDs...)
}

// 返回各站标记后的进度代次，供调用方在同一时刻记下"这一轮属于我"。
func MarkSourcesCollectStarting(sources []model.FilmSource, queueID string) map[string]uint64 {
	queueID = strings.TrimSpace(queueID)
	epochs := make(map[string]uint64, len(sources))
	for _, source := range sources {
		state := ensure(source.Id, source.Name)
		state.mu.Lock()
		state.data.Total = 0
		state.data.Current = 0
		state.data.Success = 0
		state.data.Failed = 0
		state.data.FailedPages = nil
		state.data.Sealed = false
		state.sealOutcome = ""
		state.probed = false
		state.data.Status = StatusStarting
		state.data.QueueId = queueID
		state.epoch++
		epochs[source.Id] = state.epoch
		state.updated = time.Now()
		state.mu.Unlock()
	}
	NotifyChange()
	return epochs
}

// MarkStoppedIfEpoch 仅当进度仍是 epoch 这一代且在排队/运行中时标成已停止，返回是否属于这一代。
// 判断和修改在同一把锁里完成，不会和新开采集的 MarkSourcesCollectStarting 交错。
func MarkStoppedIfEpoch(sourceID string, epoch uint64) bool {
	return markActiveIfEpoch(sourceID, epoch, StatusStopped)
}

// MarkFailedIfEpoch 仅当进度仍是 epoch 这一代且在排队/运行中时标成失败，返回是否属于这一代。
// 旧批次探页失败晚于一键终止 + 重试时，不能把重试的进度改成失败。
func MarkFailedIfEpoch(sourceID string, epoch uint64) bool {
	return markActiveIfEpoch(sourceID, epoch, StatusFailed)
}

func markActiveIfEpoch(sourceID string, epoch uint64, status string) bool {
	val, ok := store.Load(sourceID)
	if !ok {
		return true
	}
	state := val.(*progressState)
	state.mu.Lock()
	if state.epoch != epoch {
		state.mu.Unlock()
		return false
	}
	changed := false
	if state.data.Status == StatusStarting || state.data.Status == StatusRunning || state.data.Status == StatusPaused {
		state.data.Status = status
		state.updated = time.Now()
		changed = true
	}
	state.mu.Unlock()
	if changed {
		NotifyChange()
	}
	return true
}

func MarkStopped(sourceID string) {
	Update(sourceID, func(progress *model.CollectProgress) {
		if progress.Status == StatusStarting || progress.Status == StatusRunning || progress.Status == StatusPaused {
			progress.Status = StatusStopped
		}
	})
}
