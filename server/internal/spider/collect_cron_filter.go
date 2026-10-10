package spider

import (
	"log"
	"math/rand"
	"time"

	"server/internal/model"
	"server/internal/repository"
)

const (
	backupCollectMinHours = 6
	backupCollectMaxHours = 12
	cronDispatchJitterMin = 5
	cronDispatchJitterMax = 15
)

func collectDispatchDelay(batchCtx *collectBatchContext) time.Duration {
	if batchCtx != nil && batchCtx.trigger == model.NotifyTriggerCron {
		span := cronDispatchJitterMax - cronDispatchJitterMin + 1
		return time.Duration(cronDispatchJitterMin+rand.Intn(span)) * time.Second
	}
	return 200 * time.Millisecond
}

func backupCollectInterval(sourceID string) time.Duration {
	sum := 0
	for _, c := range sourceID {
		sum += int(c)
	}
	hours := backupCollectMinHours + sum%(backupCollectMaxHours-backupCollectMinHours+1)
	return time.Duration(hours) * time.Hour
}

func AutoCollect(h int) {
	AutoCollectTriggered(model.NotifyTriggerManual, h)
}

func AutoCollectTriggered(trigger string, h int) {
	enabled := filterEnabledSources(repository.GetCollectSourceList())
	if len(enabled) == 0 {
		log.Println("[Spider] 自动采集：未找到任何启用的站点")
		return
	}
	if trigger == "" {
		trigger = model.NotifyTriggerManual
	}
	if trigger == model.NotifyTriggerCron {
		enabled = filterCronIncrementalSources(enabled)
		if len(enabled) == 0 {
			log.Println("[Spider] 自动采集：错峰过滤后无待采集站点")
			return
		}
	}
	enabled = filterSourcesReadyForCollect(enabled)
	if len(enabled) == 0 {
		log.Println("[Spider] 自动采集：采集站都还没有分类")
		return
	}
	runSourcesWithLimit(enabled, h, "Auto-Collect", trigger)
}

func filterCronIncrementalSources(sources []model.FilmSource) []model.FilmSource {
	if len(sources) == 0 {
		return sources
	}
	primary := repository.PickPrimarySourceForCategory()
	primaryID := ""
	if primary != nil {
		primaryID = primary.Id
	}
	ids := make([]string, 0, len(sources))
	for _, s := range sources {
		ids = append(ids, s.Id)
	}
	stats := repository.GetCollectSourceStats(ids)
	now := time.Now()
	out := make([]model.FilmSource, 0, len(sources))
	for _, s := range sources {
		if s.Id == primaryID {
			out = append(out, s)
			continue
		}
		last := stats[s.Id]
		interval := backupCollectInterval(s.Id)
		if last == nil || last.IsZero() || now.Sub(*last) >= interval {
			out = append(out, s)
			continue
		}
		log.Printf("[Spider] 备用源错峰跳过 name=%s id=%s interval=%s last=%s",
			s.Name, s.Id, interval, last.Format(time.RFC3339))
	}
	return out
}
