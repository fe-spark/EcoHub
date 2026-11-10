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
	busy := isMaintenanceRunning()
	collects, maintenance, skippedCollect := planCronBatch(batch, busy)
	due := make([]string, 0, len(maintenance))
	for _, ft := range maintenance {
		due = append(due, ft.Id)
	}
	// 先把维护按到点顺序入队，再裁决采集，保证采集跳过时维护已经占住“维护中”状态。
	for _, ft := range maintenance {
		enqueueMaintenance(ft)
	}
	for _, id := range skippedCollect {
		if len(due) > 0 {
			log.Printf("[Spider] 定时自动采集跳过：同一时段有维护任务（%s），Task[%s]", strings.Join(due, "，"), id)
			continue
		}
		log.Printf("[Spider] 定时自动采集跳过：维护任务执行中（%s），Task[%s]", maintenanceRunningDetail(), id)
	}
	for _, ft := range collects {
		go runCronTaskSafely(ft)
	}
}

// planCronBatch 裁决同一时段（1 秒合并窗口）内到点的计划任务。
// 维护优先：本批有维护，或已有维护在执行/排队时，本批采集全部跳过（不排队、不补跑）。
// 本批所有维护按到点顺序返回，由调用方依次加入维护串行队列，一条都不丢。
func planCronBatch(batch []model.FilmCollectTask, maintenanceBusy bool) (collects, maintenance []model.FilmCollectTask, skippedCollect []string) {
	seen := make(map[string]struct{}, len(batch))
	for _, ft := range batch {
		if ft.Id != "" {
			if _, ok := seen[ft.Id]; ok {
				continue
			}
			seen[ft.Id] = struct{}{}
		}
		if isMaintenanceModel(ft.Model) {
			maintenance = append(maintenance, ft)
			continue
		}
		collects = append(collects, ft)
	}
	if maintenanceBusy || len(maintenance) > 0 {
		for _, ft := range collects {
			skippedCollect = append(skippedCollect, ft.Id)
		}
		return nil, maintenance, skippedCollect
	}
	return collects, maintenance, nil
}
