package spider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/fetcher"
	"server/internal/spider/progress"
	"server/internal/spider/scheduler"
	"server/internal/utils"
)

func isDispatchStopped(runVersion uint64) bool {
	return stopAllVersion.Load() != runVersion
}

func countLiveCollectTasks() int {
	return progress.TaskCount()
}

// prioritizeCollectSources 采集站排序（按后台设置的 Sort 升序优先派发）。
func prioritizeCollectSources(sources []model.FilmSource) []model.FilmSource {
	if len(sources) <= 1 {
		return sources
	}
	out := make([]model.FilmSource, len(sources))
	copy(out, sources)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].Id < out[j].Id
	})
	return out
}

func filterEnabledSources(sources []model.FilmSource) []model.FilmSource {
	enabled := make([]model.FilmSource, 0, len(sources))
	for _, s := range sources {
		if s.State {
			enabled = append(enabled, s)
		}
	}
	return enabled
}

// filterCollectableSources 占用尚未在采的源；正采集队列已包含的源直接跳过。
// 全量 / 定时 / 手动 / 恢复均走同一套占用逻辑，两条队列互不等待。
func filterCollectableSources(sources []model.FilmSource, tag string) []model.FilmSource {
	return occupyCollectSources(sources, tag)
}

func newCollectQueueID() (string, error) {
	salt, err := utils.GenerateSalt()
	if err != nil {
		return "", err
	}
	return "q-" + salt, nil
}

func occupyAndMarkCollectSources(sources []model.FilmSource, tag string) ([]model.FilmSource, error) {
	occupied := filterCollectableSources(sources, tag)
	if len(occupied) == 0 {
		return occupied, nil
	}
	queueID, err := newCollectQueueID()
	if err != nil {
		return nil, err
	}
	progress.MarkSourcesCollectStarting(occupied, queueID)
	return occupied, nil
}

func runSourcesWithLimit(sources []model.FilmSource, h int, tag, trigger string) {
	if len(sources) == 0 {
		return
	}
	sources, err := occupyAndMarkCollectSources(sources, tag)
	if err != nil {
		syslog.Errorf("[%s] 分配采集队列失败: %v", tag, err)
		return
	}
	if len(sources) == 0 {
		return
	}
	runSourcesWithLimitCore(sources, h, tag, trigger)
}

func runSourcesWithLimitCore(sources []model.FilmSource, h int, tag, trigger string) {
	if len(sources) == 0 {
		return
	}

	sources = prioritizeCollectSources(sources)
	startedAt := time.Now()
	runVersion := stopAllVersion.Load()
	batchCtx := newCollectBatchContext(trigger, tag, sources, startedAt)
	defer batchCtx.close()

	sourceLimit := config.CollectSourceConcurrency
	if sourceLimit < 0 {
		sourceLimit = 0
	}
	limitDesc := "不限制"
	if sourceLimit > 0 {
		limitDesc = fmt.Sprintf("%d", sourceLimit)
	}
	log.Printf("[%s] 采集派发 站点数=%d 站点并发=%s 页并发=%d 取数在途=%d 写阀 inflight=%d pages/s=%d",
		tag, len(sources), limitDesc, config.CollectPageWorkers, config.CollectFetchInFlight,
		config.CollectWriteMaxInflight, config.CollectWritePagesPerSec)
	runSourcesGroupWithLimit(sources, h, tag, sourceLimit, runVersion, batchCtx)
	if err := batchCtx.flushAndFinalize(); err != nil {
		syslog.Errorf("[%s] 批量采集缓存刷新失败: %v", tag, err)
	}
	batchCtx.emitSummary()
	triggerCollectDoneHooks()
}


func probeSourcePageCount(s *model.FilmSource, h int, trigger string) error {
	if progress.IsStopped(s.Id) || progress.IsStopping(s.Id) {
		return nil
	}
	resolved := h
	if shouldCatchUpCollectHours(trigger, h) {
		last, err := repository.GetLastCollectTime(s.Id)
		if err != nil {
			return err
		}
		resolved = resolveCollectHours(h, last, time.Now())
	}
	if resolved == 0 {
		return errors.New("采集时长不能为 0")
	}
	r, err := requestForSource(s)
	if err != nil {
		return err
	}
	if resolved > 0 {
		r.Params.Set("h", fmt.Sprint(resolved))
	}
	if s.ResolveFormat() == model.SourceFormatXML && r.Params.Get("ac") == "" {
		r.Params.Set("ac", "videolist")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stopCheck := time.NewTicker(200 * time.Millisecond)
	defer stopCheck.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCheck.C:
				if progress.IsStopped(s.Id) || progress.IsStopping(s.Id) {
					cancel()
					return
				}
			}
		}
	}()
	pageCount, err := fetcher.GetPageCountWithRetry(ctx, s, r)
	if progress.IsStopped(s.Id) || progress.IsStopping(s.Id) {
		return nil
	}
	if err != nil {
		progress.Update(s.Id, func(cur *model.CollectProgress) {
			if cur.Status == progress.StatusStarting {
				cur.Status = progress.StatusFailed
			}
		})
		return err
	}
	progress.SetPageTotal(s.Id, pageCount)
	if pageCount > 0 && resolved <= 0 {
		_ = repository.UpdateSitePageTotal(s.Id, pageCount)
	}
	log.Printf("[Spider] 站点 %s 总页=%d\n", s.Name, pageCount)
	return nil
}

// ProbeSitePageTotal 探测采集站全站总页数（不带时间过滤）。
func ProbeSitePageTotal(s *model.FilmSource) (int, error) {
	if s == nil {
		return 0, errors.New("采集源为空")
	}
	r, err := requestForSource(s)
	if err != nil {
		return 0, err
	}
	if s.ResolveFormat() == model.SourceFormatXML && r.Params.Get("ac") == "" {
		r.Params.Set("ac", "videolist")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return fetcher.GetPageCountWithRetry(ctx, s, r)
}

func runSourcesGroupWithLimit(sources []model.FilmSource, h int, tag string, limit int, runVersion uint64, batchCtx *collectBatchContext) {
	trigger := ""
	if batchCtx != nil {
		trigger = batchCtx.trigger
	}
	if len(sources) == 0 {
		return
	}
	var sem chan struct{}
	if limit > 0 {
		sem = make(chan struct{}, limit)
	}
	var wg sync.WaitGroup

	for idx, src := range sources {
		if isDispatchStopped(runVersion) {
			log.Printf("[%s] 检测到一键终止，停止派发剩余站点任务", tag)
			for _, skipped := range sources[idx:] {
				scheduler.FinishSource(skipped.Id)
				abandonQueuedCollectSource(skipped, batchCtx)
			}
			break
		}
		if batchCtx != nil && !batchCtx.ownsOccupy(src.Id) {
			log.Printf("[%s] 站点 %s 已从本队列移除，跳过派发", tag, src.Name)
			scheduler.FinishSource(src.Id)
			batchCtx.releaseSourceOccupy(src.Id)
			continue
		}
		// 独立探页与抓取解耦：直接派发协程，由各站点探完总页后立即开跑，不再被死站阻塞
		if idx > 0 {
			time.Sleep(collectDispatchDelay(batchCtx))
		}
		wg.Add(1)
		if sem != nil {
			sem <- struct{}{}
		}
		go func(fs model.FilmSource) {
			defer wg.Done()
			defer func() {
				if sem != nil {
					<-sem
				}
			}()
			defer scheduler.FinishSource(fs.Id)
			if isDispatchStopped(runVersion) {
				log.Printf("[%s] 站点 %s 在启动前被一键终止拦截", tag, fs.Name)
				abandonQueuedCollectSource(fs, batchCtx)
				return
			}
			if batchCtx != nil && !batchCtx.ownsOccupy(fs.Id) {
				log.Printf("[%s] 站点 %s 已从本队列移除，跳过采集", tag, fs.Name)
				batchCtx.releaseSourceOccupy(fs.Id)
				return
			}
			if err := probeSourcePageCount(&fs, h, trigger); err != nil {
				syslog.Errorf("[%s] 站点 %s 总页探测失败: %v", tag, fs.Name, err)
				abandonQueuedCollectSource(fs, batchCtx)
				return
			}
			if progress.IsStopped(fs.Id) || progress.PageTotal(fs.Id) <= 0 {
				log.Printf("[%s] 站点 %s 无页可采或已停止，跳过采集", tag, fs.Name)
				if progress.PageTotal(fs.Id) == 0 && batchCtx != nil {
					batchCtx.releaseSourceOccupy(fs.Id)
					return
				}
				abandonQueuedCollectSource(fs, batchCtx)
				return
			}
			if err := handleCollectWithStopVersion(fs.Id, h, &runVersion, false, false, batchCtx); err != nil {
				if errors.Is(err, context.Canceled) {
					syslog.Infof("[%s] 站点 %s 采集已由用户取消", tag, fs.Name)
				} else {
					syslog.Errorf("[%s] 采集站点 %s 失败: %v", tag, fs.Name, err)
				}
			}
		}(src)
	}
	wg.Wait()
}
