package spider

import (
	"log"
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
	tasks, skipped := planCronBatch(batch, isMaintenanceRunning())
	for _, id := range skipped {
		log.Printf("[Spider] 定时自动采集避让：与非高频维护任务同时触发，跳过本次采集 Task[%s]", id)
	}
	for _, ft := range tasks {
		go executeTask(ft)
	}
}

// planCronBatch 同一批里先保留维护任务。只要有维护任务，或维护任务已在跑，本批定时采集全部跳过。
func planCronBatch(batch []model.FilmCollectTask, maintenanceBusy bool) (run []model.FilmCollectTask, skipped []string) {
	seen := make(map[string]struct{}, len(batch))
	var maintenance, collects []model.FilmCollectTask
	for _, ft := range batch {
		if ft.Id != "" {
			if _, ok := seen[ft.Id]; ok {
				continue
			}
			seen[ft.Id] = struct{}{}
		}
		if ft.Model == model.TaskModelAutoCollect {
			collects = append(collects, ft)
			continue
		}
		maintenance = append(maintenance, ft)
	}
	if maintenanceBusy || len(maintenance) > 0 {
		skipped = make([]string, len(collects))
		for i, ft := range collects {
			skipped[i] = ft.Id
		}
		return maintenance, skipped
	}
	return collects, nil
}
