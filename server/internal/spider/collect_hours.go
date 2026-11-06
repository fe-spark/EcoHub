package spider

import (
	"context"
	"time"

	"server/internal/model"
	"server/internal/spider/progress"
)

// shouldNoteCronCollectSuccess 整次跑完且未被停止，才算成功。
// 用户停止、ctx 取消、返回错误都不算成功，避免补窗被截断。
func shouldNoteCronCollectSuccess(runErr error, ctx context.Context, sourceID string) bool {
	if runErr != nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	return !progress.IsStopped(sourceID)
}

// shouldWriteCronAnchor 只有定时整次成功才写 last_collect_time。手动全量和重试不移动补窗锚点。
func shouldWriteCronAnchor(trigger string, runOK bool, startedAt time.Time) bool {
	return runOK && trigger == model.CollectTriggerCron && !startedAt.IsZero()
}

const (
	// collectCatchUpOverlapHours 定时补窗相对上次成功再多看的小时数，覆盖长采集中途写入。
	collectCatchUpOverlapHours = 1
	// collectCatchUpMaxHours 定时增量补窗上限（7 天）；更久需手工加长窗口或全量。
	collectCatchUpMaxHours = 168
)

// shouldCatchUpCollectHours 仅定时增量补窗；手动采集保持调用方选择的 h。
func shouldCatchUpCollectHours(trigger string, requested int) bool {
	return requested > 0 && trigger == model.CollectTriggerCron
}

// resolveCollectHours 定时增量时长：max(配置 h, 距上次成功小时数+重叠)，并封顶。
// requested<=0 表示非法 0 或全量 -1，原样返回。
func resolveCollectHours(requested int, lastSuccess *time.Time, now time.Time) int {
	if requested <= 0 {
		return requested
	}
	h := requested
	if lastSuccess != nil && !lastSuccess.IsZero() && !now.Before(*lastSuccess) {
		elapsed := int(now.Sub(*lastSuccess).Hours()) + collectCatchUpOverlapHours
		if elapsed > h {
			h = elapsed
		}
	}
	if h > collectCatchUpMaxHours {
		h = collectCatchUpMaxHours
	}
	return h
}
