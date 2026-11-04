package spider

import (
	"log"
	"time"

	"server/internal/model"
	"server/internal/repository"
)

// AutoCollect 自动采集（内部或手动调用）。
func AutoCollect(h int) {
	AutoCollectTriggered(model.CollectTriggerManual, h)
}

// AutoCollectTriggered 定时或自动触发采集，完全复用采集中心的批量采集逻辑。
func AutoCollectTriggered(trigger string, h int) {
	if isMaintenanceRunning() {
		log.Printf("[Spider] 自动采集跳过：维护任务执行中（%s）", maintenanceRunningDetail())
		return
	}
	if trigger == "" {
		trigger = model.CollectTriggerCron
	}
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		log.Printf("[Spider] 自动采集：读取采集站失败: %v", err)
		return
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
		return
	}

	ready, result := PrepareBatchCollectStart(ids)
	for _, skip := range result.Skipped {
		log.Printf("[Spider] 自动采集跳过站点: id=%s name=%s reason=%s", skip.Id, skip.Name, skip.Reason)
	}
	if len(ready) == 0 {
		log.Println("[Spider] 自动采集：没有可采集的就绪站点")
		return
	}
	log.Printf("[Spider] 自动采集开启: 共 %d 个站点进入批量采集队列 (trigger=%s, h=%d)", len(ready), trigger, h)
	BatchCollectPrepared(trigger, h, ready)
}
