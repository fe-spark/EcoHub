package spider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sync/atomic"
	"time"

	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider/fetcher"
	"server/internal/spider/progress"
	"server/internal/utils"
)

/*
	采集逻辑 v3
*/

var spiderCore = &JsonCollect{}

func init() {
	filmsnapshot.AddSearchIndexProgressListener(func(p filmsnapshot.SearchIndexProgress) {
		progress.BroadcastSearchIndexProgress(progress.SearchIndexProgressEvent{
			Total:   p.Total,
			Current: p.Current,
			Percent: p.Percent,
			Status:  p.Status,
		})
	})
}

func requestForSource(s *model.FilmSource) (utils.RequestInfo, error) {
	if s == nil {
		return utils.RequestInfo{Params: url.Values{}}, nil
	}
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}, SkipTLSVerify: s.SkipTLSVerify}
	if s.ProxyCollect {
		ok, proxy, err := repository.ResolveSpiderProxy()
		if err != nil {
			return r, err
		}
		if ok {
			r.ProxyURL = proxy
		}
		return r, nil
	}
	if s.Id != "" {
		ok, proxy, err := repository.ResolveSourceProxy(s.Id)
		if err != nil {
			return r, err
		}
		if ok {
			r.ProxyURL = proxy
		}
	}
	return r, nil
}

// stopAllVersion 用于打断批量/自动采集的外层派发循环。
// 每次执行一键终止都会递增版本号，旧版本调度器检测到版本变化后不再继续启动新站点任务。
var stopAllVersion atomic.Uint64

// init 把采集编排层的取数能力注入子包（子包不反向依赖本包）。
func init() {
	progress.SetOccupyChecker(isOccupiedCollectSource)
	fetcher.Configure(fetcher.Deps{
		GetPageCount: func(s *model.FilmSource, r utils.RequestInfo) (int, error) {
			return ResolveCollector(s.ResolveFormat()).GetPageCount(r)
		},
		GetFilmDetail: func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
			return ResolveCollector(s.ResolveFormat()).GetFilmDetail(r)
		},
		WaitTurn:      waitSourceRequestTurn,
		LiveTaskCount: countLiveCollectTasks,
		SavePage:      saveCollectedFilmForCollect,

		SkipPublishOnError:     shouldSkipCollectPublishOnError,
		ResolveSpiderProxy:     repository.ResolveSpiderProxy,
		ResolveSourceProxy:     repository.ResolveSourceProxy,
		OnSourceFailed:         emitSourceFailedNotify,
		SourceConcurrencyScale: GetSourceConcurrencyScale,
	})
}

func HandlePreparedCollect(id string, h int) error {
	if s := repository.FindCollectSourceById(id); s != nil {
		if err := probeSourcePageCount(s, h, model.CollectTriggerManual); err != nil {
			abortUnstartedCollect(id, nil)
			return err
		}
	}
	return handleCollectWithStopVersion(id, h, nil, true, true, nil)
}

func handleCollectWithStopVersion(id string, h int, runVersion *uint64, isStandalone bool, allowPreparedStart bool, batchCtx *collectBatchContext) (retErr error) {
	hadWrites := false
	successPagesCount := 0
	failedPagesCount := 0
	pageCount := 0
	var collectCtx context.Context
	statsOwned := false
	releasedPreparedOccupy := false
	defer func() {
		if collectCtx != nil {
			progress.FinishRun(collectCtx, id, retErr)
		}
	}()
	defer func() {
		// 单站退出（无论完成、失败还是终止）立即释放自身占用锁，不等待批量内其他慢速站点。
		if !releasedPreparedOccupy {
			if retErr != nil {
				abortUnstartedCollect(id, batchCtx)
			} else {
				batchCtx.releaseSourceOccupy(id)
			}
		}
	}()
	if runVersion != nil && isDispatchStopped(*runVersion) {
		return errors.New("任务已被一键终止，跳过启动")
	}
	if (runVersion != nil || allowPreparedStart) && (progress.IsStopped(id) || progress.IsStopping(id)) {
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
	if batchCtx != nil && batchCtx.trigger == model.CollectTriggerCron {
		batchCtx.noteCronCollectStarted()
	}
	defer collectLifecycle.endSource(id)

	if err := ensureMasterCategoriesReady(s); err != nil {
		if batchCtx != nil {
			batchCtx.RecordSourceError(s.Id, err.Error())
		}
		retErr = err
		return err
	}
	if isStandalone {
		batchCtx = newCollectBatchContext(model.CollectTriggerManual, model.CollectTagSingleCollect, []model.FilmSource{*s}, time.Now(), true).withHour(h)
		if err := batchCtx.recordStart(); err != nil {
			log.Printf("[Spider] 初始采集记录写入失败 batchID=%s: %v", batchCtx.batchID, err)
			return err
		}
	}
	defer func() {
		originalErr := retErr
		if originalErr != nil && batchCtx != nil && !errors.Is(originalErr, context.Canceled) {
			batchCtx.RecordSourceError(s.Id, originalErr.Error())
		}
		runOK := shouldNoteCronCollectSuccess(originalErr, collectCtx, s.Id)
		if batchCtx != nil && batchCtx.trigger == model.CollectTriggerCron && statsOwned {
			repository.UnsuppressCollectSourceStats(s.Id)
			if runOK {
				repository.NoteCollectSourceStats(s.Id)
			}
		}
		effectiveSuccess := successPagesCount
		if runOK || effectiveSuccess > 0 || hadWrites {
			repository.NoteCollectSourceRun(s.Id, effectiveSuccess)
		}
		progress.FlushHotpathSideEffects(s.Id)
		if originalErr != nil && !hadWrites {
			if isStandalone && batchCtx != nil {
				batchCtx.emitSummary()
				batchCtx.close()
				releasedPreparedOccupy = true
			}
			return
		}
		if batchCtx != nil {
			snap, _ := progress.Snapshot(s.Id)
			status := snap.Status
			errMsg := ""
			if originalErr != nil && !errors.Is(originalErr, context.Canceled) {
				errMsg = originalErr.Error()
			}
			if status == "" || status == progress.StatusStarting || status == progress.StatusRunning {
				if originalErr != nil {
					if successPagesCount > 0 {
						status = model.CollectRecordStatusPartial
					} else {
						status = progress.StatusFailed
					}
				} else {
					status = progress.StatusDone
				}
			}
			status, errMsg = settleSourceStatus(status, errMsg, successPagesCount)

			totalP := pageCount
			if totalP <= 0 {
				totalP = snap.Total
			}
			succP := successPagesCount
			if succP <= 0 {
				succP = snap.Success
			}
			// 失败页只计真正失败的页。没抓到的页留在总数差额里，不能算失败。
			failP := failedPagesCount

			batchCtx.recordSourceSnapshot(model.CollectRecordSourceDetail{
				SourceID:    s.Id,
				SourceName:  s.Name,
				Status:      status,
				Hour:        batchCtx.sourceHour(s.Id),
				PageTotal:   totalP,
				PageCurrent: succP,
				SuccessCnt:  succP,
				FailedCnt:   failP,
				FilmsTotal:  batchCtx.sourceFilmCount(s.Id),
				Error:       errMsg,
			})
			batchCtx.markSourceFinished(*s)
			if !isStandalone && isPreferredCollectSource(s.Id) && originalErr == nil && !progress.IsStopped(s.Id) && !progress.IsStopping(s.Id) {
				if err := batchCtx.finalizePrimarySource(s.Id); err != nil {
					retErr = err
				}
			}
		}
		if isStandalone && batchCtx != nil {
			flushErr := batchCtx.flushAndFinalize()
			if originalErr == nil && flushErr != nil {
				retErr = flushErr
			}
			batchCtx.emitSummary()
			batchCtx.close()
			releasedPreparedOccupy = true
		}
	}()

	reqId, err := utils.GenerateSalt()
	if err != nil {
		return err
	}

	ctx, err := progress.TryRegisterTask(id, reqId, func() bool {
		return runVersion == nil || !isDispatchStopped(*runVersion)
	})
	switch {
	case errors.Is(err, progress.ErrDispatchStopped):
		return errors.New("任务已被一键终止，跳过启动")
	case errors.Is(err, progress.ErrStopRequested):
		return errors.New("任务已被停止，跳过启动")
	case errors.Is(err, progress.ErrTaskExists):
		log.Printf("[Spider] 站点 %s 已有任务正在运行，跳过本次采集...\n", id)
		return fmt.Errorf("站点 %s 已有任务正在运行，已跳过本次采集", id)
	case err != nil:
		return err
	}
	collectCtx = progress.WithRunEpoch(ctx, progress.Epoch(id))
	if batchCtx != nil && batchCtx.trigger == model.CollectTriggerCron {
		repository.SuppressCollectSourceStats(s.Id)
		statsOwned = true
	}

	defer func() {
		if progress.UnregisterTask(id, reqId) {
			progress.Update(id, func(cur *model.CollectProgress) {
				if retErr != nil && cur.Status != progress.StatusStopped && cur.Status != progress.StatusStopping {
					cur.Status = progress.StatusFailed
					return
				}
			})
			log.Printf("[Spider] 站点 %s 任务结束\n", id)
		}
	}()

	log.Printf("[Spider] 站点 %s 任务启动 (reqId: %s)\n", id, reqId)
	progress.Ensure(id, s.Name)

	if h == 0 {
		return errors.New("采集时长不能为 0")
	}
	trigger := model.CollectTriggerManual
	if batchCtx != nil && batchCtx.trigger != "" {
		trigger = batchCtx.trigger
	}
	if shouldCatchUpCollectHours(trigger, h) {
		origH := h
		last, err := repository.GetLastCollectTime(s.Id)
		if err != nil {
			return err
		}
		h = resolveCollectHours(h, last, time.Now())
		if h > origH {
			log.Printf("[Spider] 站点 %s 定时采集窗口补齐 %dh → %dh（距上次成功采集）\n", s.Name, origH, h)
		}
	}
	if batchCtx != nil {
		batchCtx.noteSourceHour(s.Id, h)
	}
	pageCount = progress.PageTotal(id)
	if pageCount < 0 {
		if err := probeSourcePageCount(s, h, trigger); err != nil {
			return err
		}
		pageCount = progress.PageTotal(id)
	}
	if pageCount <= 0 {
		log.Printf("[Spider] 站点 %s 无需分页 (pageCount=%d，该时间段无新内容)\n", s.Name, pageCount)
		return nil
	}
	progress.UpdateRun(collectCtx, id, func(cur *model.CollectProgress) {
		if cur.Status == progress.StatusStarting {
			cur.Status = progress.StatusRunning
		}
	})
	log.Printf("[Spider] 站点 %s 共 %d 页，开始采集...\n", s.Name, pageCount)

	hadWrites, successPagesCount, failedPagesCount, err = fetcher.CollectPages(collectCtx, pageCount, s, h, batchCtx)
	if err != nil {
		return err
	}
	progress.MarkSourceDoneIfRun(collectCtx, id)
	return nil
}

func filterSourcesReadyForCollect(sources []model.FilmSource) []model.FilmSource {
	if len(sources) == 0 {
		return sources
	}
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Id != "" {
			ids = append(ids, source.Id)
		}
	}
	ready := repository.SourceIDsWithCategory(ids)
	out := make([]model.FilmSource, 0, len(sources))
	for _, source := range sources {
		if _, ok := ready[source.Id]; !ok {
			log.Printf("[Spider] 采集站还没有分类，跳过采集: name=%s id=%s", source.Name, source.Id)
			continue
		}
		out = append(out, source)
	}
	return out
}

func BatchCollectPrepared(trigger string, h int, sources []model.FilmSource) {
	if len(sources) == 0 {
		return
	}
	if trigger == "" {
		trigger = model.CollectTriggerManual
	}
	tag := model.CollectTagBatchCollect
	if trigger == model.CollectTriggerCron {
		tag = model.CollectTagCronCollect
	}
	runSourcesWithLimitCore(sources, h, tag, trigger)
}

// StartBatchCollectRetry 先写入 running 批次，再异步执行失败站点重试。
func StartBatchCollectRetry(parentBatchID string, hour int, sources []model.FilmSource) (string, error) {
	if len(sources) == 0 {
		return "", errors.New("没有可重试的站点")
	}
	sources = prioritizeCollectSources(sources)
	startedAt := time.Now()
	batchCtx := newCollectBatchContext(model.CollectTriggerRetry, model.CollectTagRetryCollect, sources, startedAt).
		withHour(hour).withParentBatchID(parentBatchID)
	if err := batchCtx.recordStart(); err != nil {
		for _, s := range sources {
			abandonQueuedCollectSource(s, batchCtx)
		}
		batchCtx.close()
		return "", err
	}
	go runPreparedBatch(batchCtx, hour, "失败重试")
	return batchCtx.batchID, nil
}
