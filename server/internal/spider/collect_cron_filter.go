package spider

import (
	"log"

	"server/internal/model"
	"server/internal/repository"
)

// AutoCollect 自动采集（内部或手动调用）。
func AutoCollect(h int) {
	AutoCollectTriggered(model.CollectTriggerManual, h)
}

// AutoCollectTriggered 定时或自动触发采集，完全复用采集中心的批量采集逻辑。
func AutoCollectTriggered(trigger string, h int) {
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
	if len(ids) == 0 {
		log.Println("[Spider] 自动采集：未找到任何启用的站点")
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
