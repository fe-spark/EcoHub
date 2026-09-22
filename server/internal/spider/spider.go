package spider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/repository"
	"server/internal/spider/fetcher"
	"server/internal/spider/progress"
	"server/internal/spider/scheduler"
	"server/internal/utils"
)

/*
	采集逻辑 v3
*/

var spiderCore = &JsonCollect{}

// requestForSource 构造发往采集站的请求，并按站点代理配置写入 ProxyURL。
func requestForSource(uri, sourceID string) utils.RequestInfo {
	r := utils.RequestInfo{Uri: uri, Params: url.Values{}}
	if ok, proxy := repository.ResolveSourceProxy(sourceID); ok {
		r.ProxyURL = proxy
	}
	return r
}

// stopAllVersion 用于打断批量/自动采集的外层派发循环。
// 每次执行一键终止都会递增版本号，旧版本调度器检测到版本变化后不再继续启动新站点任务。
var stopAllVersion atomic.Uint64

// init 把采集编排层的进度超时通知与取数能力注入子包（子包不反向依赖本包）。
func init() {
	progress.SetStaleNotifier(emitProgressStaleNotify)
	progress.SetOccupyChecker(isOccupiedCollectSource)
	fetcher.Configure(fetcher.Deps{
		GetPageCount:        func(r utils.RequestInfo) (int, error) { return spiderCore.GetPageCount(r) },
		GetFilmDetail:       func(r utils.RequestInfo) ([]model.MovieDetail, error) { return spiderCore.GetFilmDetail(r) },
		WaitTurn:            waitSourceRequestTurn,
		LiveTaskCount:       countLiveCollectTasks,
		SavePage:            saveCollectedFilmForCollect,
		SavePageFailure:     saveFilmPageFailure,
		SkipPublishOnError:  shouldSkipCollectPublishOnError,
		NoteSourceError:     noteSourceError,
		NotifySourceFailed:  emitSourceFailedNotify,
		BatchSummaryEnabled: func() bool { return notify.IsEventEnabled(model.NotifyEventCollectBatchSummary) },
		ResolveSourceProxy:  repository.ResolveSourceProxy,
	})
}

func isDispatchStopped(runVersion uint64) bool {
	return stopAllVersion.Load() != runVersion
}

func countLiveCollectTasks() int {
	return progress.TaskCount()
}

// prioritizeCollectSources 主采集站优先派发，便于有限站并发时先跑主站。
func prioritizeCollectSources(sources []model.FilmSource) []model.FilmSource {
	if len(sources) <= 1 {
		return sources
	}
	out := make([]model.FilmSource, 0, len(sources))
	for _, s := range sources {
		if s.Grade == model.MasterCollect {
			out = append(out, s)
		}
	}
	for _, s := range sources {
		if s.Grade != model.MasterCollect {
			out = append(out, s)
		}
	}
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

func newCollectQueueID() string {
	return "q-" + utils.GenerateSalt()
}

func occupyAndMarkCollectSources(sources []model.FilmSource, tag string) []model.FilmSource {
	occupied := filterCollectableSources(sources, tag)
	if len(occupied) == 0 {
		return occupied
	}
	progress.MarkSourcesCollectStarting(occupied, newCollectQueueID())
	return occupied
}

func runSourcesWithLimit(sources []model.FilmSource, h int, tag, trigger string) {
	if len(sources) == 0 {
		return
	}
	sources = occupyAndMarkCollectSources(sources, tag)
	if len(sources) == 0 {
		return
	}
	runSourcesWithLimitCore(sources, h, tag, trigger)
}

func runSourcesWithLimitCore(sources []model.FilmSource, h int, tag, trigger string) {
	if len(sources) == 0 {
		return
	}

	if db.Mdb != nil {
		var categoryCount int64
		_ = db.Mdb.Model(&model.Category{}).Count(&categoryCount).Error
		if categoryCount == 0 {
			syslog.Warnf("[Spider] 检测到本地分类树为空(0 个分类)，尝试从主站同步分类树...")
			target := repository.PickMasterSourceForCategory()
			if target == nil {
				syslog.Warnf("[Spider] 无主采集站，跳过分类树同步（没有主站就不能有分类树）")
			} else if err := CollectCategory(target); err != nil {
				syslog.Errorf("[Spider] 采集前自动同步主站分类失败 name=%s state=%v: %v",
					target.Name, target.State, err)
			} else {
				repository.RefreshCategoryCache()
				syslog.Infof("[Spider] 采集前自动同步主站分类成功 name=%s state=%v",
					target.Name, target.State)
			}
		}
	}

	sources = prioritizeCollectSources(sources)
	batch := notify.StartChangeBatch()
	startedAt := time.Now()
	runVersion := stopAllVersion.Load()

	batchCtx := newCollectBatchContext(trigger, tag, sources, batch, startedAt)
	defer batchCtx.close()

	sourceLimit := config.CollectSourceConcurrency
	if sourceLimit < 0 {
		sourceLimit = 0
	}
	limitDesc := "不限制"
	if sourceLimit > 0 {
		limitDesc = fmt.Sprintf("%d", sourceLimit)
	}
	log.Printf("[%s] 采集派发 站点数=%d 站点并发=%s 页并发=%d 写阀 inflight=%d pages/s=%d",
		tag, len(sources), limitDesc, config.CollectPageWorkers,
		config.CollectWriteMaxInflight, config.CollectWritePagesPerSec)
	runSourcesGroupWithLimit(sources, h, tag, sourceLimit, runVersion, batchCtx)
	var finalizeErr error
	if err := batchCtx.flushAndFinalize(); err != nil {
		syslog.Errorf("[%s] 批量采集收尾刷新失败: %v", tag, err)
		finalizeErr = err
	}
	batchCtx.emitSummary(finalizeErr)
	triggerCollectDoneHooks()
}

func runSourcesGroupWithLimit(sources []model.FilmSource, h int, tag string, limit int, runVersion uint64, batchCtx *collectBatchContext) {
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
				scheduler.FinishSource(skipped.Grade, skipped.Id)
				abandonQueuedCollectSource(skipped, batchCtx)
			}
			break
		}
		if progress.IsStopped(src.Id) {
			log.Printf("[%s] 站点 %s 已在排队中停止，跳过派发", tag, src.Name)
			scheduler.FinishSource(src.Grade, src.Id)
			abandonQueuedCollectSource(src, batchCtx)
			continue
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
			defer scheduler.FinishSource(fs.Grade, fs.Id)
			if isDispatchStopped(runVersion) {
				log.Printf("[%s] 站点 %s 在启动前被一键终止拦截", tag, fs.Name)
				abandonQueuedCollectSource(fs, batchCtx)
				return
			}
			if progress.IsStopped(fs.Id) {
				log.Printf("[%s] 站点 %s 已在启动前停止，跳过采集", tag, fs.Name)
				abandonQueuedCollectSource(fs, batchCtx)
				return
			}
			if err := handleCollectWithStopVersion(fs.Id, h, &runVersion, false, false, batchCtx); err != nil {
				syslog.Errorf("[%s] 采集站点 %s 失败: %v", tag, fs.Name, err)
			}
		}(src)
	}
	wg.Wait()
}

func HandlePreparedCollect(id string, h int) error {
	return handleCollectWithStopVersion(id, h, nil, true, true, nil)
}

func handleCollectWithStopVersion(id string, h int, runVersion *uint64, isStandalone bool, allowPreparedStart bool, batchCtx *collectBatchContext) (retErr error) {
	hadWrites := false
	var collectCtx context.Context
	statsOwned := false
	releasedPreparedOccupy := false
	defer func() {
		// 失败必须立刻放占用并把 starting 标 failed，否则 UI 仍显示采集中、重试被挡住。
		if !releasedPreparedOccupy && retErr != nil {
			abortUnstartedCollect(id, batchCtx)
		}
	}()
	if runVersion != nil && isDispatchStopped(*runVersion) {
		return errors.New("任务已被一键终止，跳过启动")
	}
	if (runVersion != nil || allowPreparedStart) && progress.IsStopped(id) {
		return errors.New("任务已被停止，跳过启动")
	}
	if runVersion == nil && !allowPreparedStart && progress.IsStarting(id) {
		return errors.New("该采集站已在批量队列中，已跳过本次采集")
	}

	s := repository.FindCollectSourceById(id)
	if s == nil {
		return errors.New("采集站点不存在")
	} else if !s.State {
		return errors.New("采集站点已停用")
	}
	if err := collectLifecycle.beginSource(id); err != nil {
		log.Printf("[Spider] 站点 %s 无法启动采集: %v\n", id, err)
		return err
	}
	defer collectLifecycle.endSource(id)

	if err := ensureMasterCategoriesReady(s); err != nil {
		retErr = err
		return err
	}
	if isStandalone {
		batchCtx = newCollectBatchContext(model.NotifyTriggerManual, "单站采集", []model.FilmSource{*s}, nil, time.Now(), true)
	}
	isMasterFullCollect := s.Grade == model.MasterCollect && h < 0
	if isMasterFullCollect && batchCtx != nil {
		batchCtx.beginMasterRebuild(s.Id)
	}
	defer func() {
		originalErr := retErr
		if batchCtx != nil && batchCtx.trigger == model.NotifyTriggerCron && statsOwned {
			repository.UnsuppressCollectSourceStats(s.Id)
			if shouldNoteCronCollectSuccess(originalErr, collectCtx, s.Id) {
				repository.NoteCollectSourceStats(s.Id)
			}
		}
		progress.FlushHotpathSideEffects(s.Id)
		if originalErr != nil && (!hadWrites || shouldSkipCollectPublishOnError(*s, h)) {
			if isMasterFullCollect && batchCtx != nil {
				batchCtx.discardPendingMasterMIDs(s.Id)
			}
			if isStandalone && batchCtx != nil {
				noteSourceError(s.Id, originalErr.Error())
				batchCtx.emitSummary(originalErr)
				releasedPreparedOccupy = true
			}
			return
		}
		if isMasterFullCollect && batchCtx != nil {
			batchCtx.publishPendingMasterMIDs(s.Id)
		}
		if batchCtx != nil {
			batchCtx.markSourceFinished(*s)
		}
		if isStandalone && batchCtx != nil {
			flushErr := batchCtx.flushAndFinalize()
			if originalErr == nil && flushErr != nil {
				retErr = flushErr
			}
			batchCtx.emitSummary(flushErr)
			releasedPreparedOccupy = true
			return
		}
		if !progress.IsStopped(s.Id) {
			progress.Update(s.Id, func(cur *model.CollectProgress) {
				switch cur.Status {
				case progress.StatusRunning, progress.StatusStarting, progress.StatusPageDone:
					cur.Status = progress.StatusWaitingPublish
				}
			})
		}
	}()

	reqId := utils.GenerateSalt()

	ctx, err := progress.TryRegisterTask(id, reqId, func() bool {
		return runVersion == nil || !isDispatchStopped(*runVersion)
	})
	switch {
	case errors.Is(err, progress.ErrDispatchStopped):
		return errors.New("任务已被一键终止，跳过启动")
	case errors.Is(err, progress.ErrTaskExists):
		log.Printf("[Spider] 站点 %s 已有任务正在运行，跳过本次采集...\n", id)
		return fmt.Errorf("站点 %s 已有任务正在运行，已跳过本次采集", id)
	case err != nil:
		return err
	}
	collectCtx = ctx
	if batchCtx != nil && batchCtx.trigger == model.NotifyTriggerCron {
		repository.SuppressCollectSourceStats(s.Id)
		statsOwned = true
	}

	defer func() {
		if progress.UnregisterTask(id, reqId) {
			progress.Update(id, func(cur *model.CollectProgress) {
				if retErr != nil && cur.Status != progress.StatusStopped {
					cur.Status = progress.StatusFailed
					return
				}
			})
			if retErr != nil {
				noteSourceError(id, retErr.Error())
			}
			log.Printf("[Spider] 站点 %s 任务结束\n", id)
		}
	}()

	log.Printf("[Spider] 站点 %s 任务启动 (reqId: %s)\n", id, reqId)
	progress.Ensure(id, s.Name)

	r := requestForSource(s.Uri, s.Id)
	if h == 0 {
		return errors.New("采集时长不能为 0")
	}
	trigger := model.NotifyTriggerManual
	if batchCtx != nil && batchCtx.trigger != "" {
		trigger = batchCtx.trigger
	}
	if shouldCatchUpCollectHours(trigger, h) {
		origH := h
		h = resolveCollectHours(h, repository.GetLastCollectTime(s.Id), time.Now())
		if h > origH {
			log.Printf("[Spider] 站点 %s 定时采集窗口补齐 %dh → %dh（距上次成功采集）\n", s.Name, origH, h)
		}
	}
	if h > 0 {
		r.Params.Set("h", fmt.Sprint(h))
	}

	pageCount, err := fetcher.GetPageCountWithRetry(ctx, s, r)
	if err != nil {
		return err
	}
	if pageCount <= 0 {
		progress.Update(id, func(cur *model.CollectProgress) {
			cur.Total = 0
			cur.Current = 0
			cur.Success = 0
			cur.Failed = 0
			if isStandalone {
				cur.Status = progress.StatusPageDone
			} else {
				cur.Status = progress.StatusWaitingPublish
			}
		})
		log.Printf("[Spider] 站点 %s 无需分页 (pageCount=%d，该时间段无新内容) isStandalone=%v\n", s.Name, pageCount, isStandalone)
		return nil
	}
	progress.Update(id, func(cur *model.CollectProgress) {
		cur.Total = pageCount
		cur.Current = 0
		cur.Success = 0
		cur.Failed = 0
		cur.Status = progress.StatusRunning
	})
	log.Printf("[Spider] 站点 %s 共 %d 页，开始采集...\n", s.Name, pageCount)

	pageWorkerLimit := fetcher.GetSourcePageConcurrency(s)
	hadWrites, err = fetcher.CollectPages(ctx, pageCount, pageWorkerLimit, s, h, batchCtx)
	if err != nil {
		return err
	}
	if progress.IsStopped(id) {
		log.Printf("[Spider] 站点 %s 已停止接收新分页，等待收尾刷新\n", s.Name)
	} else {
		progress.MarkSourcePagesFinished(id, isStandalone)
	}
	return nil
}

func PrepareBatchCollectStart(ids []string) ([]model.FilmSource, error) {
	sources := make([]model.FilmSource, 0, len(ids))
	for _, id := range ids {
		if fs := repository.FindCollectSourceById(id); fs != nil && fs.State {
			sources = append(sources, *fs)
		}
	}
	sources = occupyAndMarkCollectSources(sources, "Batch-Collect")
	if len(sources) == 0 {
		return nil, fmt.Errorf("没有可启动的采集站（均未启用或已在采集中）")
	}
	return sources, nil
}

func BatchCollectPrepared(trigger string, h int, sources []model.FilmSource) {
	if len(sources) == 0 {
		return
	}
	if trigger == "" {
		trigger = model.NotifyTriggerManual
	}
	runSourcesWithLimitCore(sources, h, "Batch-Collect", trigger)
}

func BatchCollect(h int, ids ...string) {
	BatchCollectTriggered(model.NotifyTriggerManual, h, ids...)
}

func BatchCollectTriggered(trigger string, h int, ids ...string) {
	sources := make([]model.FilmSource, 0)
	for _, id := range ids {
		if fs := repository.FindCollectSourceById(id); fs != nil && fs.State {
			sources = append(sources, *fs)
		}
	}

	if len(sources) == 0 {
		return
	}
	if trigger == "" {
		trigger = model.NotifyTriggerManual
	}
	runSourcesWithLimit(sources, h, "Batch-Collect", trigger)
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
	runSourcesWithLimit(enabled, h, "Auto-Collect", trigger)
}
