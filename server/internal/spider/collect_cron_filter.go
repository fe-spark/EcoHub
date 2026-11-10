package spider

import (
	"fmt"
	"log"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
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

// autoCollect 返回会让本轮采集整体无法开始的错误（例如读取采集站失败），交给定时任务告警。
// 跳过（维护中、索引更新中、无可采站点）不算错误。
func autoCollect(trigger string, h int) error {
	if searchIndexBlocksCollect() {
		log.Printf("[Spider] 定时采集跳过：%s", errSearchIndexUpdating.Error())
		return nil
	}
	if isMaintenanceRunning() {
		log.Printf("[Spider] 自动采集跳过：维护任务执行中（%s）", maintenanceRunningDetail())
		return nil
	}
	if trigger == "" {
		trigger = model.CollectTriggerCron
	}
	sources, err := repository.GetCollectSourceList()
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
		skip, err := repository.TrippedSourceIDsToday(time.Now())
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
	log.Printf("[Spider] 自动采集开启: 共 %d 个站点进入批量采集队列 (trigger=%s, h=%d)", len(ready.Sources), trigger, h)
	BatchCollectPrepared(trigger, h, ready)
	return nil
}
