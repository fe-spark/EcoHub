package collect

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"server/internal/film/catalog"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
	"sync/atomic"
	"time"

	"server/internal/collect/engine"
	"server/internal/collect/progress"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/model"
	"server/internal/utils"
)

/*
	采集逻辑 v3
*/

var defaultEngineRunner = engine.NewRunner(engine.Runner{
	ResolveProxy:   repobase.ResolveSpiderProxy,
	SavePage:       saveCollectedFilmForCollect,
	OnSourceFailed: emitSourceFailedNotify,
})

// collectPagesFn 分页拉取实现；默认调用 engine.Runner.CollectPages（支持复用探页第 1 页），测试可替换。
var collectPagesFn = func(ctx context.Context, pageCount int, targetPages []int, s *model.FilmSource, h int, batch engine.Batch) (bool, int, int, []int, error) {
	var seed *engine.Page1Result
	if bc, ok := batch.(*collectBatchContext); ok {
		seed = bc.takePage1Seed(s.Id)
	}
	return defaultEngineRunner.CollectPages(ctx, pageCount, targetPages, seed, s, h, batch)
}

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
		ok, proxy, err := repobase.ResolveSpiderProxy()
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

// init 注册进度包所需的状态回调。
func init() {
	progress.SetOccupyChecker(isOccupiedCollectSource)
	progress.SetStatsFlusher(FlushCollectSourceStats)
}

func handleCollectWithStopVersion(id string, h int, runVersion *uint64, batchCtx *collectBatchContext) (retErr error) {
	hadWrites := false
	successPagesCount := 0
	failedPagesCount := 0
	var failedPagesList []int
	pageCount := 0
	var collectCtx context.Context
	var runEpoch uint64
	statsOwned := false
	releasedPreparedOccupy := false
	collectPagesStarted := false
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
		recordSourceStatusSnapshot(batchCtx, model.FilmSource{Id: id}, progress.StatusStopped, "", true)
		return fmt.Errorf("停止采集：%s", reason)
	}
	if runVersion != nil && (progress.IsStopped(id) || progress.IsStopping(id)) {
		reason := collectStopReason(id)
		log.Printf("[Spider] 站点 %s 停止采集：%s", id, reason)
		recordSourceStatusSnapshot(batchCtx, model.FilmSource{Id: id}, progress.StatusStopped, "", true)
		return fmt.Errorf("停止采集：%s", reason)
	}
	if runVersion == nil && progress.IsStarting(id) {
		return errors.New("该采集站已在批量队列中，已跳过本次采集")
	}

	s := sourcecatalog.FindCollectSourceById(id)
	if s == nil {
		err := errors.New("采集站点不存在")
		if batchCtx != nil {
			batchCtx.RecordSourceError(id, err.Error())
			recordSourceStatusSnapshot(batchCtx, model.FilmSource{Id: id}, progress.StatusFailed, err.Error(), true)
		}
		return err
	} else if !s.State {
		err := errors.New("采集站点已停用")
		if batchCtx != nil {
			batchCtx.RecordSourceError(s.Id, err.Error())
			recordSourceStatusSnapshot(batchCtx, *s, progress.StatusFailed, err.Error(), true)
		}
		return err
	}
	if err := collectLifecycle.beginSource(id); err != nil {
		if errors.Is(err, errCollectDuringMaintenance) {
			log.Printf("[Spider] 站点 %s 停止采集：维护任务执行中（%s）", id, maintenanceRunningDetail())
			if batchCtx != nil {
				if epoch, ok := batchCtx.progressEpoch(id); ok {
					progress.MarkStoppedIfEpoch(id, epoch)
				} else {
					progress.MarkStopped(id)
				}
				recordSourceStatusSnapshot(batchCtx, *s, progress.StatusStopped, "", false)
				batchCtx.releaseSourceOccupy(id)
			} else {
				progress.MarkStopped(id)
				releaseCollectSourceIDs(id)
			}
			releasedPreparedOccupy = true
			return nil
		}
		log.Printf("[Spider] 站点 %s 无法启动采集: %v\n", id, err)
		if batchCtx != nil {
			batchCtx.RecordSourceError(s.Id, err.Error())
			recordSourceStatusSnapshot(batchCtx, *s, progress.StatusFailed, err.Error(), true)
		}
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
		recordSourceStatusSnapshot(batchCtx, *s, progress.StatusFailed, err.Error(), true)
		retErr = err
		return err
	}
	defer func() {
		originalErr := retErr
		// 只有真正落盘写入过的停止任务才需要进入待检索封口；0 页被中断直接标记已停止避免统计为完成。
		if collectCtx != nil && progress.IsStopping(s.Id) && hadWrites {
			defer func() {
				if progress.MarkStoppedAsSealing(collectCtx, s.Id) {
					searchReseal.addSealing(s.Id)
				}
			}()
		}
		if originalErr != nil && batchCtx != nil && !errors.Is(originalErr, context.Canceled) {
			batchCtx.RecordSourceError(s.Id, originalErr.Error())
		}
		if !collectPagesStarted && originalErr != nil && !skipSourceFailedNotify(originalErr) {
			if batchCtx == nil || batchCtx.ownsOccupy(s.Id) {
				emitSourceFailedNotify(s.Id, s.Name, originalErr.Error())
			}
		}
		runOK := shouldNoteCronCollectSuccess(originalErr, collectCtx, s.Id)
		trigger := ""
		startedAt := time.Time{}
		if batchCtx != nil {
			trigger = batchCtx.trigger
			startedAt = batchCtx.startedAt
		}
		if statsOwned {
			UnsuppressCollectSourceStats(s.Id)
		}
		if shouldWriteCronAnchor(trigger, runOK, startedAt) {
			NoteCollectSourceStatsAt(s.Id, startedAt)
			log.Printf("[Spider] 站点 %s 定时采集成功，last_collect_time=%s（本次开始）", s.Name, startedAt.Format("2006-01-02 15:04:05"))
		} else if statsOwned {
			DropCollectSourceStatsPending(s.Id)
		}
		effectiveSuccess := successPagesCount
		if runOK || effectiveSuccess > 0 || hadWrites {
			NoteCollectSourceRun(s.Id, effectiveSuccess)
		}
		displacedOrStopped := errors.Is(originalErr, progress.ErrStopRequested) || errors.Is(originalErr, progress.ErrDispatchStopped)
		ownsProgress := batchCtx == nil || (batchCtx.ownsOccupy(s.Id) && (runEpoch == 0 || progress.Epoch(s.Id) == runEpoch))
		if ownsProgress {
			progress.FlushHotpathSideEffects(s.Id)
		}
		if batchCtx != nil {
			var snap model.CollectProgress
			if ownsProgress && !displacedOrStopped {
				snap, _ = progress.Snapshot(s.Id)
			}
			status := snap.Status
			errMsg := ""
			if originalErr != nil && !errors.Is(originalErr, context.Canceled) && !displacedOrStopped {
				errMsg = originalErr.Error()
			}
			if !ownsProgress || displacedOrStopped || status == progress.StatusStopping || status == progress.StatusStopped {
				status = progress.StatusStopped
				errMsg = ""
			} else if status == "" || status == progress.StatusStarting || status == progress.StatusRunning {
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
			totalP := pageCount
			if totalP <= 0 && ownsProgress && !displacedOrStopped {
				totalP = snap.Total
			}
			if totalP <= 0 && len(failedPagesList) > 0 {
				totalP = len(failedPagesList)
			}
			succP := successPagesCount
			if succP <= 0 && ownsProgress && !displacedOrStopped {
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
				Error:       errMsg,
			})
			if originalErr != nil && !hadWrites {
				return
			}
			batchCtx.markSourceFinished(*s)
			// 被用户停止或已失权时不再同步抢 publishMu 刷快照窗口，避免已停站点卡在「停止中」或干预新重试；
			// 已入库数据会在批次收尾 flushAndFinalize 与 searchReseal 中统一刷新并检索封口。
			stoppedByUser := !ownsProgress || displacedOrStopped || (collectCtx != nil && (progress.IsStopping(s.Id) || progress.IsStopped(s.Id)))
			if !stoppedByUser {
				if err := batchCtx.sealFinishedSource(*s); err != nil {
					log.Printf("[Spider] 站点 %s 封口入队失败: %v", s.Name, err)
				}
			}
		} else if originalErr != nil && !hadWrites {
			return
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
		return fmt.Errorf("停止采集：%s: %w", reason, progress.ErrDispatchStopped)
	case errors.Is(err, progress.ErrStopRequested):
		reason := collectStopReason(id)
		log.Printf("[Spider] 站点 %s 停止采集：%s", id, reason)
		return fmt.Errorf("停止采集：%s: %w", reason, progress.ErrStopRequested)
	case errors.Is(err, progress.ErrTaskExists):
		log.Printf("[Spider] 站点 %s 已有任务正在运行，跳过本次采集...\n", id)
		return fmt.Errorf("站点 %s 已有任务正在运行，已跳过本次采集: %w", id, progress.ErrTaskExists)
	case err != nil:
		return err
	}
	batchEpoch, hasEpoch := batchCtx.progressEpoch(id)
	if !hasEpoch {
		batchEpoch = progress.Epoch(id)
	}
	runEpoch = batchEpoch
	collectCtx = progress.WithRunEpoch(ctx, runEpoch)
	SuppressCollectSourceStats(s.Id)
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
	if already, ok := batchCtx.resolvedSourceHour(s.Id); ok {
		h = already
	} else if shouldCatchUpCollectHours(trigger, h) {
		origH := h
		last, err := GetLastCollectTime(s.Id)
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
		if err := probeSourcePageCount(collectCtx, s, h, trigger, batchCtx); err != nil {
			return err
		}
		if collectCtx.Err() != nil {
			return collectCtx.Err()
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

	collectPagesStarted = true
	hadWrites, successPagesCount, failedPagesCount, failedPagesList, err = collectPagesFn(collectCtx, pageCount, targetPages, s, h, batchCtx)
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
	ready := catalog.SourceIDsWithCategory(ids)
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
	// 名额已经在 Prepare 里占下的批次直接开工。再查一次暂停会把已经告诉调用方的站丢掉。
	if !claim.queueHeld {
		if reason := collectPauseReason(); reason != "" {
			log.Printf("[Spider] 批量采集跳过：%s", reason)
			claim.abandon()
			return
		}
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
	if !claim.queueHeld {
		if _, err := tryBeginCollectQueue(); err != nil {
			claim.abandon()
			return "", err
		}
	}
	sources := prioritizeCollectSources(claim.Sources)
	startedAt := time.Now()
	batchCtx := newCollectBatchContext(model.CollectTriggerRetry, model.CollectTagRetryCollect, sources, startedAt).
		withClaim(claim).withHour(hour).withParentBatchID(parentBatchID)
	if len(sourceTargetPages) > 0 && len(sourceTargetPages[0]) > 0 {
		batchCtx = batchCtx.withTargetPages(sourceTargetPages[0])
	}
	if err := batchCtx.recordStart(); err != nil {
		log.Printf("[失败重试] 初始采集记录写入失败，采集继续 batchID=%s: %v", batchCtx.batchID, err)
	}
	goBackgroundCollect(func() { runPreparedBatch(batchCtx, hour, "失败重试", true) })
	return batchCtx.batchID, nil
}

// beforeRegisterCollectRunHook 仅供测试：登记运行之前调用，用于在这个窗口里模拟单站停止 + 重试。
var beforeRegisterCollectRunHook func(id string)
