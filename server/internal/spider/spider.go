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
		if p.Status == "done" || p.Status == "idle" {
			// 只收口本轮停等的重建终态；idle/无关进度不能把 sealing 标成完成。
			searchReseal.onSearchIndexTerminal(p.Status)
		}
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
		WaitTurn:           waitSourceRequestTurn,
		SavePage:           saveCollectedFilmForCollect,
		SkipPublishOnError: shouldSkipCollectPublishOnError,
		ResolveSpiderProxy: repository.ResolveSpiderProxy,
		ResolveSourceProxy: repository.ResolveSourceProxy,
		OnSourceFailed:     emitSourceFailedNotify,
	})
}

func handleCollectWithStopVersion(id string, h int, runVersion *uint64, batchCtx *collectBatchContext) (retErr error) {
	hadWrites := false
	successPagesCount := 0
	failedPagesCount := 0
	var failedPagesList []int
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
		reason := currentStopAllReason()
		log.Printf("[Spider] 站点 %s 停止采集：%s", id, reason)
		return fmt.Errorf("停止采集：%s", reason)
	}
	if runVersion != nil && (progress.IsStopped(id) || progress.IsStopping(id)) {
		reason := collectStopReason(id)
		log.Printf("[Spider] 站点 %s 停止采集：%s", id, reason)
		return fmt.Errorf("停止采集：%s", reason)
	}
	if runVersion == nil && progress.IsStarting(id) {
		return errors.New("该采集站已在批量队列中，已跳过本次采集")
	}

	s := repository.FindCollectSourceById(id)
	if s == nil {
		return errors.New("采集站点不存在")
	} else if !s.State {
		return errors.New("采集站点已停用")
	}
	if err := collectLifecycle.beginSource(id); err != nil {
		if errors.Is(err, errCollectDuringMaintenance) {
			log.Printf("[Spider] 站点 %s 停止采集：维护任务执行中（%s）", id, maintenanceRunningDetail())
			progress.MarkStopped(id)
			if batchCtx != nil {
				batchCtx.releaseSourceOccupy(id)
			} else {
				releaseCollectSourceIDs(id)
			}
			releasedPreparedOccupy = true
			return nil
		}
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
	defer func() {
		originalErr := retErr
		// 用户停止也走待检索：先等本次收尾，封口周期结束再落成已停止。
		if collectCtx != nil && progress.IsStopping(s.Id) {
			defer func() {
				if progress.MarkStoppedAsSealing(collectCtx, s.Id) {
					searchReseal.addSealing(s.Id)
				}
			}()
		}
		if originalErr != nil && batchCtx != nil && !errors.Is(originalErr, context.Canceled) {
			batchCtx.RecordSourceError(s.Id, originalErr.Error())
		}
		runOK := shouldNoteCronCollectSuccess(originalErr, collectCtx, s.Id)
		trigger := ""
		startedAt := time.Time{}
		if batchCtx != nil {
			trigger = batchCtx.trigger
			startedAt = batchCtx.startedAt
		}
		if statsOwned {
			repository.UnsuppressCollectSourceStats(s.Id)
		}
		if shouldWriteCronAnchor(trigger, runOK, startedAt) {
			repository.NoteCollectSourceStatsAt(s.Id, startedAt)
			log.Printf("[Spider] 站点 %s 定时采集成功，last_collect_time=%s（本次开始）", s.Name, startedAt.Format("2006-01-02 15:04:05"))
		} else if statsOwned {
			repository.DropCollectSourceStatsPending(s.Id)
		}
		effectiveSuccess := successPagesCount
		if runOK || effectiveSuccess > 0 || hadWrites {
			repository.NoteCollectSourceRun(s.Id, effectiveSuccess)
		}
		progress.FlushHotpathSideEffects(s.Id)
		if originalErr != nil && !hadWrites {
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
			if msg := formatCategorySkip(batchCtx.CategorySkip(s.Id)); msg != "" {
				log.Printf("[Spider] 站点 %s %s", s.Name, msg)
				if errMsg != "" {
					errMsg = errMsg + "；" + msg
				} else {
					errMsg = msg
				}
			}
			status, errMsg = settleSourceStatus(status, errMsg, successPagesCount)

			totalP := pageCount
			if totalP <= 0 {
				totalP = snap.Total
			}
			if totalP <= 0 && len(failedPagesList) > 0 {
				totalP = len(failedPagesList)
			}
			succP := successPagesCount
			if succP <= 0 {
				succP = snap.Success
			}
			// 失败页只计真正失败的页。没抓到的页留在总数差额里，不能算失败。
			failP := failedPagesCount
			if failP <= 0 && len(failedPagesList) > 0 {
				failP = len(failedPagesList)
			}

			batchCtx.recordSourceSnapshot(model.CollectRecordSourceDetail{
				SourceID:    s.Id,
				SourceName:  s.Name,
				Status:      status,
				Hour:        batchCtx.sourceHour(s.Id),
				PageTotal:   totalP,
				PageCurrent: succP,
				SuccessCnt:  succP,
				FailedCnt:   failP,
				FailedPages: failedPagesList,
				FilmsTotal:  batchCtx.sourceFilmCount(s.Id),
				Error:       errMsg,
			})
			batchCtx.markSourceFinished(*s)
			if err := batchCtx.sealFinishedSource(*s); err != nil {
				log.Printf("[Spider] 站点 %s 封口入队失败: %v", s.Name, err)
			}
		}
	}()

	reqId, err := utils.GenerateSalt()
	if err != nil {
		return err
	}

	if beforeRegisterCollectRunHook != nil {
		beforeRegisterCollectRunHook(id)
	}
	ctx, err := progress.TryRegisterTask(id, reqId, func() error {
		return canRegisterCollectRun(id, runVersion, batchCtx)
	})
	switch {
	case errors.Is(err, progress.ErrDispatchStopped):
		reason := currentStopAllReason()
		log.Printf("[Spider] 站点 %s 停止采集：%s", id, reason)
		return fmt.Errorf("停止采集：%s", reason)
	case errors.Is(err, progress.ErrStopRequested):
		reason := collectStopReason(id)
		log.Printf("[Spider] 站点 %s 停止采集：%s", id, reason)
		return fmt.Errorf("停止采集：%s", reason)
	case errors.Is(err, progress.ErrTaskExists):
		log.Printf("[Spider] 站点 %s 已有任务正在运行，跳过本次采集...\n", id)
		return fmt.Errorf("站点 %s 已有任务正在运行，已跳过本次采集", id)
	case err != nil:
		return err
	}
	runEpoch, hasEpoch := batchCtx.progressEpoch(id)
	if !hasEpoch {
		runEpoch = progress.Epoch(id)
	}
	collectCtx = progress.WithRunEpoch(ctx, runEpoch)
	repository.SuppressCollectSourceStats(s.Id)
	statsOwned = true

	defer func() {
		if progress.UnregisterTask(id, reqId) {
			progress.Update(id, func(cur *model.CollectProgress) {
				// 待检索已经挂上封口队列，退出不能再改回失败。
				if retErr != nil && cur.Status != progress.StatusStopped && cur.Status != progress.StatusStopping && cur.Status != progress.StatusSealing {
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
	targetPages := batchCtx.getTargetPages(id)
	if len(targetPages) > 0 {
		pageCount = len(targetPages)
		batchCtx.setSourcePageTotal(id, pageCount)
	} else if pageCount < 0 {
		if err := probeSourcePageCount(s, h, trigger, batchCtx); err != nil {
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
	if len(targetPages) > 0 {
		log.Printf("[Spider] 站点 %s 重试失败页 共 %d 页 (%v)，开始采集...\n", s.Name, len(targetPages), targetPages)
	} else {
		log.Printf("[Spider] 站点 %s 共 %d 页，开始采集...\n", s.Name, pageCount)
	}

	hadWrites, successPagesCount, failedPagesCount, failedPagesList, err = fetcher.CollectPages(collectCtx, pageCount, targetPages, s, h, batchCtx)
	// 有成功页的失败也进待检索。零成功直接失败。停止中由 MarkSourceSealingIfRun 拒绝。
	if err != nil && !hadWrites {
		return err
	}
	if !progress.MarkSourceSealingIfRun(collectCtx, id) {
		return err
	}
	searchReseal.addSealing(id)
	return err
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

// BatchCollectPrepared 执行 PrepareBatchCollectStart 占好的站。批次沿用占用那一刻的令牌和进度代次。
func BatchCollectPrepared(trigger string, h int, claim CollectClaim) {
	if len(claim.Sources) == 0 {
		return
	}
	if reason := collectPauseReason(); reason != "" {
		log.Printf("[Spider] 批量采集跳过：%s", reason)
		claim.abandon()
		return
	}
	if trigger == "" {
		trigger = model.CollectTriggerManual
	}
	tag := model.CollectTagBatchCollect
	if trigger == model.CollectTriggerCron {
		tag = model.CollectTagCronCollect
	}
	runSourcesWithLimitCore(claim, h, tag, trigger)
}

// StartBatchCollectRetry 先写入 running 批次，再异步执行失败站点重试。
// claim 来自 PrepareBatchCollectStart，批次沿用占用那一刻的令牌和进度代次。
func StartBatchCollectRetry(parentBatchID string, hour int, claim CollectClaim, sourceTargetPages ...map[string][]int) (string, error) {
	if len(claim.Sources) == 0 {
		return "", errors.New("没有可重试的站点")
	}
	if _, err := tryBeginCollectQueue(); err != nil {
		return "", err
	}
	sources := prioritizeCollectSources(claim.Sources)
	startedAt := time.Now()
	batchCtx := newCollectBatchContext(model.CollectTriggerRetry, model.CollectTagRetryCollect, sources, startedAt).
		withClaim(claim).withHour(hour).withParentBatchID(parentBatchID)
	if len(sourceTargetPages) > 0 && len(sourceTargetPages[0]) > 0 {
		batchCtx = batchCtx.withTargetPages(sourceTargetPages[0])
	}
	if err := batchCtx.recordStart(); err != nil {
		endCollectQueue()
		for _, s := range sources {
			abandonQueuedCollectSource(s, batchCtx)
		}
		batchCtx.close()
		return "", err
	}
	goBackgroundCollect(func() { runPreparedBatch(batchCtx, hour, "失败重试", true) })
	return batchCtx.batchID, nil
}

// beforeRegisterCollectRunHook 仅供测试：登记运行之前调用，用于在这个窗口里模拟单站停止 + 重试。
var beforeRegisterCollectRunHook func(id string)

// canRegisterCollectRun 登记运行前（任务表锁内）复核本次运行仍可开跑：
// 一键终止过则不开；有批次时占用令牌和进度代次都必须还是本批次的。
// 单站停止后立刻重试，重试会换掉占用令牌、进度代次加一；旧批次这时才走到登记，
// 不能顶替重试登记任务、沿用重试的代次去改进度，否则重试会因任务已存在退出并把进度标成失败。
func canRegisterCollectRun(id string, runVersion *uint64, batchCtx *collectBatchContext) error {
	if runVersion != nil && isDispatchStopped(*runVersion) {
		return progress.ErrDispatchStopped
	}
	if batchCtx == nil {
		return nil
	}
	if !batchCtx.ownsOccupy(id) {
		return progress.ErrStopRequested
	}
	if epoch, ok := batchCtx.progressEpoch(id); ok && progress.Epoch(id) != epoch {
		return progress.ErrStopRequested
	}
	return nil
}
