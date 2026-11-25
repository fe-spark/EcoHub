package collect

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"server/internal/collect/ledger"
	"server/internal/collect/progress"
	"server/internal/infra/syslog"
	"server/internal/model"
	"strings"
	"time"
)

// AutoCollect 自动采集（内部或手动调用）。
func AutoCollect(h int) {
	AutoCollectTriggered(model.CollectTriggerManual, h)
}

// AutoCollectTriggered 定时或自动触发采集，完全复用采集中心的批量采集逻辑。
func AutoCollectTriggered(trigger string, h int) {
	if err := autoCollect(trigger, h); err != nil {
		syslog.Errorf("[Spider] 自动采集失败: %v", err)
	}
}

// autoCollectFn 定时采集入口。测试替换它，避免真的去拉站点。
var (
	autoCollectFn        = autoCollect
	defaultAutoCollectFn = reflect.ValueOf(autoCollect).Pointer()
)

// runAutoCollectTask 跑一轮自动采集（在采站点由 PrepareBatchCollectStart 按站跳过，不做整轮补采）。
func runAutoCollectTask(ft model.FilmCollectTask, manual bool) {
	var err error
	if reflect.ValueOf(autoCollectFn).Pointer() == defaultAutoCollectFn {
		err = autoCollectForTask(ft.Id, model.CollectTriggerCron, ft.Time)
	} else {
		err = autoCollectFn(model.CollectTriggerCron, ft.Time)
	}
	if err == nil {
		return
	}
	remark := strings.TrimSpace(ft.Remark)
	if remark == "" {
		remark = "自动采集"
	}
	syslog.Errorf("[Cron] %s 失败: %v", remark, err)
	if !manual {
		emitCronFailedNotify(ft.Id, remark, err.Error())
	}
}

// autoCollect 返回会让本轮采集整体无法开始的错误（例如读取采集站失败），交给定时任务告警。
// 跳过（维护中、索引更新中、无可采站点）不算错误。
func autoCollect(trigger string, h int) error {
	return autoCollectForTask("", trigger, h)
}

func autoCollectForTask(taskID, trigger string, h int) error {
	if reason := collectPauseReason(); reason != "" {
		log.Printf("[Spider] 定时采集跳过：%s", reason)
		return nil
	}
	if isMaintenanceRunning() {
		log.Printf("[Spider] 自动采集跳过：维护任务执行中（%s）", maintenanceRunningDetail())
		return nil
	}
	if trigger == "" {
		trigger = model.CollectTriggerCron
	}
	sources, err := GetCollectSourceList()
	if err != nil {
		return fmt.Errorf("读取采集站失败: %w", err)
	}
	ids := make([]string, 0, len(sources))
	for _, s := range sources {
		if s.State {
			ids = append(ids, s.Id)
		}
	}
	if trigger == model.CollectTriggerCron {
		skip, err := ledger.TrippedSourceIDsToday(time.Now())
		if err != nil {
			log.Printf("[Spider] 自动采集：读取熔断站点失败 (fail-open 降级全量): %v", err)
		} else if len(skip) > 0 {
			kept := make([]string, 0, len(ids))
			for _, id := range ids {
				if _, ok := skip[id]; ok {
					log.Printf("[Spider] 自动采集降频跳过熔断站点: id=%s", id)
					continue
				}
				kept = append(kept, id)
			}
			ids = kept
		}
	}
	if len(ids) == 0 {
		log.Println("[Spider] 自动采集：没有需要采集的启用站点")
		return nil
	}

	ready, result := PrepareBatchCollectStart(ids)
	for _, skip := range result.Skipped {
		log.Printf("[Spider] 自动采集跳过站点: id=%s name=%s reason=%s", skip.Id, skip.Name, skip.Reason)
	}
	if len(ready.Sources) == 0 {
		log.Println("[Spider] 自动采集：没有可采集的就绪站点")
		return nil
	}
	ready.cronTaskID = strings.TrimSpace(taskID)
	log.Printf("[Spider] 自动采集开启: 共 %d 个站点进入批量采集队列 (trigger=%s, h=%d)", len(ready.Sources), trigger, h)
	BatchCollectPrepared(trigger, h, ready)
	return nil
}

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
