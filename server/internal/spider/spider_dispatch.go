package spider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
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

const collectDispatchDelay = 200 * time.Millisecond

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
func newCollectQueueID() (string, error) {
	salt, err := utils.GenerateSalt()
	if err != nil {
		return "", err
	}
	return "q-" + salt, nil
}

func occupyAndMarkCollectSources(sources []model.FilmSource, tag string) ([]model.FilmSource, error) {
	claim, err := claimCollectSources(sources, tag)
	return claim.Sources, err
}

// claimCollectSources 占用并标成排队中，同时记下占用令牌与进度代次，交给随后建的批次。
func claimCollectSources(sources []model.FilmSource, tag string) (CollectClaim, error) {
	occupied, tokens := occupyCollectSourcesWithTokens(sources, tag)
	if len(occupied) == 0 {
		return CollectClaim{Sources: occupied}, nil
	}
	queueID, err := newCollectQueueID()
	if err != nil {
		releaseCollectTokens(tokens)
		return CollectClaim{}, err
	}
	epochs := progress.MarkSourcesCollectStarting(occupied, queueID)
	return CollectClaim{Sources: occupied, tokens: tokens, epochs: epochs}, nil
}

func runSourcesWithLimit(sources []model.FilmSource, h int, tag, trigger string) {
	if len(sources) == 0 {
		return
	}
	claim, err := claimCollectSources(sources, tag)
	if err != nil {
		syslog.Errorf("[%s] 分配采集队列失败: %v", tag, err)
		return
	}
	runSourcesWithLimitCore(claim, h, tag, trigger)
}

func runSourcesWithLimitCore(claim CollectClaim, h int, tag, trigger string) {
	if len(claim.Sources) == 0 {
		return
	}

	sources := prioritizeCollectSources(claim.Sources)
	startedAt := time.Now()
	batchCtx := newCollectBatchContext(trigger, tag, sources, startedAt).withClaim(claim).withHour(h)
	if err := batchCtx.recordStart(); err != nil {
		log.Printf("[%s] 初始采集记录写入失败 batchID=%s: %v", tag, batchCtx.batchID, err)
		for _, s := range sources {
			abandonQueuedCollectSource(s, batchCtx)
		}
		batchCtx.close()
		return
	}
	runPreparedBatch(batchCtx, h, tag)
}

func runPreparedBatch(batchCtx *collectBatchContext, h int, tag string) {
	if batchCtx == nil {
		return
	}
	defer batchCtx.close()

	runVersion := stopAllVersion.Load()
	sourceLimit := config.CollectSourceConcurrency
	if sourceLimit < 0 {
		sourceLimit = 0
	}
	limitDesc := "不限制"
	if sourceLimit > 0 {
		limitDesc = fmt.Sprintf("%d", sourceLimit)
	}
	log.Printf("[%s] 采集派发 站点数=%d 站点并发=%s 页并发=%d 取数在途=%d 写阀 inflight=%d",
		tag, len(batchCtx.sources), limitDesc, config.CollectPageWorkers, config.CollectFetchInFlight,
		config.CollectWriteMaxInflight)
	runSourcesGroupWithLimit(batchCtx.sources, h, tag, sourceLimit, runVersion, batchCtx)
	if err := batchCtx.flushAndFinalize(); err != nil {
		syslog.Errorf("[%s] 批量采集缓存刷新失败: %v", tag, err)
	}
	batchCtx.emitSummary()
	triggerCollectDoneHooks()
}

// fetchProbePageCount 按采集时长请求采集站总页。测试替换它来模拟慢/失败的探页。
var fetchProbePageCount = func(ctx context.Context, s *model.FilmSource, hours int) (int, error) {
	r, err := requestForSource(s)
	if err != nil {
		return 0, err
	}
	if hours > 0 {
		r.Params.Set("h", fmt.Sprint(hours))
	}
	return fetcher.GetPageCountWithRetry(ctx, s, r)
}

// probeSourcePageCount 探总页并写进度。有批次时只写本批次那一代的进度：
// 一键终止后同站可能已开了新的重试，旧批次探页晚回来不能改掉重试的状态或总页。
func probeSourcePageCount(s *model.FilmSource, h int, trigger string, batchCtx *collectBatchContext) error {
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stopCheck := time.NewTicker(200 * time.Millisecond)
	defer stopCheck.Stop()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				syslog.Errorf("[Spider] 站点 %s 探页停止监听 panic: %v\n%s", s.Name, r, debug.Stack())
			}
		}()
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
	pageCount, err := fetchProbePageCount(ctx, s, resolved)
	if progress.IsStopped(s.Id) || progress.IsStopping(s.Id) {
		return nil
	}
	if err != nil {
		batchCtx.markSourceFailed(s.Id)
		return err
	}
	if !batchCtx.setSourcePageTotal(s.Id, pageCount) {
		log.Printf("[Spider] 站点 %s 已被新的采集接手，丢弃旧批次探页结果\n", s.Name)
		return nil
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
			reason := currentStopAllReason()
			log.Printf("[%s] 停止派发剩余站点：%s", tag, reason)
			for _, skipped := range sources[idx:] {
				log.Printf("[%s] 站点 %s 停止采集：%s", tag, skipped.Name, reason)
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
			time.Sleep(collectDispatchDelay)
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
			defer func() {
				if r := recover(); r != nil {
					reason := reportSourcePanic(tag, fs, batchCtx, r)
					// 进度已被新的采集接手时不告警，也不碰新采集的进度。
					if batchCtx.markSourceFailed(fs.Id) {
						emitSourceFailedNotify(fs.Id, fs.Name, reason)
					}
					batchCtx.releaseSourceOccupy(fs.Id)
				}
			}()
			if isDispatchStopped(runVersion) {
				log.Printf("[%s] 站点 %s 停止采集：%s", tag, fs.Name, currentStopAllReason())
				abandonQueuedCollectSource(fs, batchCtx)
				return
			}
			if batchCtx != nil && !batchCtx.ownsOccupy(fs.Id) {
				log.Printf("[%s] 站点 %s 已从本队列移除，跳过采集", tag, fs.Name)
				batchCtx.releaseSourceOccupy(fs.Id)
				return
			}
			targetPages := batchCtx.getTargetPages(fs.Id)
			if len(targetPages) > 0 {
				batchCtx.setSourcePageTotal(fs.Id, len(targetPages))
			} else if err := probeSourcePageCount(&fs, h, trigger, batchCtx); err != nil {
				syslog.Errorf("[%s] 站点 %s 总页探测失败: %v", tag, fs.Name, err)
				if batchCtx != nil {
					batchCtx.RecordSourceError(fs.Id, err.Error())
				}
				if batchCtx.markSourceFailed(fs.Id) {
					emitSourceFailedNotify(fs.Id, fs.Name, err.Error())
				}
				batchCtx.releaseSourceOccupy(fs.Id)
				return
			}
			// 探页期间被一键终止并由新采集接手：本批次不再继续，也不碰新采集的进度和占用。
			if batchCtx != nil && !batchCtx.ownsOccupy(fs.Id) {
				log.Printf("[%s] 站点 %s 已被新的采集接手，旧队列不再继续", tag, fs.Name)
				batchCtx.releaseSourceOccupy(fs.Id)
				return
			}
			if progress.IsStopped(fs.Id) || progress.IsStopping(fs.Id) {
				log.Printf("[%s] 站点 %s 停止采集：%s", tag, fs.Name, collectStopReason(fs.Id))
				abandonQueuedCollectSource(fs, batchCtx)
				return
			}
			if progress.PageTotal(fs.Id) <= 0 {
				log.Printf("[%s] 站点 %s 无页可采，跳过采集", tag, fs.Name)
				if batchCtx != nil {
					batchCtx.releaseSourceOccupy(fs.Id)
				}
				return
			}
			if err := handleCollectWithStopVersion(fs.Id, h, &runVersion, batchCtx); err != nil {
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
