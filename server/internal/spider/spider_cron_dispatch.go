package spider

import (
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/model"
)

var cronCoalesceWait = time.Second

var (
	cronDispatchOnce sync.Once
	cronFlushCh      chan struct{}
	cronMu           sync.Mutex
	cronPending      []model.FilmCollectTask
)

func enqueueCronTask(ft model.FilmCollectTask) {
	if !ft.State {
		return
	}
	cronDispatchOnce.Do(func() {
		cronFlushCh = make(chan struct{}, 1)
		go cronDispatchLoop()
	})
	cronMu.Lock()
	cronPending = append(cronPending, ft)
	cronMu.Unlock()
	select {
	case cronFlushCh <- struct{}{}:
	default:
	}
}

func cronDispatchLoop() {
	for range cronFlushCh {
		time.Sleep(cronCoalesceWait)
		flushCronQueue()
	}
}

func flushCronQueue() {
	cronMu.Lock()
	batch := cronPending
	cronPending = nil
	cronMu.Unlock()
	if len(batch) == 0 {
		return
	}
	run, light, skippedCollect, skippedMaintenance := planCronBatch(batch, isMaintenanceRunning())
	for _, ft := range light {
		go runCronTaskSafely(ft)
	}
	due := make([]string, 0, len(run))
	for _, ft := range run {
		if isMaintenanceModel(ft.Model) {
			due = append(due, ft.Id)
		}
	}
	for _, id := range skippedCollect {
		if len(due) > 0 {
			log.Printf("[Spider] 定时自动采集跳过：同一秒有维护任务（%s），Task[%s]", strings.Join(due, "，"), id)
			continue
		}
		log.Printf("[Spider] 定时自动采集跳过：维护任务执行中（%s），Task[%s]", maintenanceRunningDetail(), id)
	}
	if len(skippedMaintenance) > 0 {
		byID := make(map[string]model.FilmCollectTask, len(batch))
		for _, ft := range batch {
			byID[ft.Id] = ft
		}
		for _, id := range skippedMaintenance {
			log.Printf("[Spider] 定时维护延后：已有维护任务在执行（%s），Task[%s]", maintenanceRunningDetail(), id)
			deferMaintenance(byID[id])
		}
	}
	if len(run) == 0 {
		return
	}
	if isMaintenanceModel(run[0].Model) {
		go runMaintenanceBatch(run)
		return
	}
	for _, ft := range run {
		go runCronTaskSafely(ft)
	}
}

// runMaintenanceBatch 同一秒的维护按入队顺序执行。前一条结束后再开始下一条。
func runMaintenanceBatch(tasks []model.FilmCollectTask) {
	for _, ft := range tasks {
		runCronTaskSafely(ft)
	}
}

// planCronBatch 裁决同一秒内的计划任务。
// 轻量任务（日志清理、采集记录清理）不受维护影响，总是放行，也不让采集跳过。
// 已有维护在跑时，本批采集和维护全部跳过。
// 本批含维护且当前没有维护在跑时，只保留维护，采集跳过。
func planCronBatch(batch []model.FilmCollectTask, maintenanceBusy bool) (run, light []model.FilmCollectTask, skippedCollect, skippedMaintenance []string) {
	seen := make(map[string]struct{}, len(batch))
	var maintenance, collects []model.FilmCollectTask
	for _, ft := range batch {
		if ft.Id != "" {
			if _, ok := seen[ft.Id]; ok {
				continue
			}
			seen[ft.Id] = struct{}{}
		}
		switch {
		case ft.Model == model.TaskModelAutoCollect:
			collects = append(collects, ft)
		case isLightCronModel(ft.Model):
			light = append(light, ft)
		default:
			maintenance = append(maintenance, ft)
		}
	}
	collectIDs := func(tasks []model.FilmCollectTask) []string {
		ids := make([]string, len(tasks))
		for i, ft := range tasks {
			ids[i] = ft.Id
		}
		return ids
	}
	if maintenanceBusy {
		return nil, light, collectIDs(collects), collectIDs(maintenance)
	}
	if len(maintenance) > 0 {
		return maintenance, light, collectIDs(collects), nil
	}
	return collects, light, nil, nil
}
