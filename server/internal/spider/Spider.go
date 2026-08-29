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
	"server/internal/utils"
)

/*
	采集逻辑 v3
*/

var spiderCore = &JsonCollect{}

// activeTasks 存储当前活跃采集任务的信息
var activeTasks sync.Map

// stopAllVersion 用于打断批量/自动采集的外层派发循环。
// 每次执行一键终止都会递增版本号，旧版本调度器检测到版本变化后不再继续启动新站点任务。
var stopAllVersion atomic.Uint64

// taskMu 保护同一站点 cancel+Store 的原子性，防止并发截停竞态
var taskMu sync.Mutex

type collectTask struct {
	cancel context.CancelFunc
	reqId  string
}

func isDispatchStopped(runVersion uint64) bool {
	return stopAllVersion.Load() != runVersion
}

func countLiveCollectTasks() int {
	n := 0
	activeTasks.Range(func(key, value any) bool {
		n++
		return true
	})
	return n
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

func getEnabledSourcesByGrade(grade model.SourceGrade) []model.FilmSource {
	return filterEnabledSources(repository.GetCollectSourceListByGrade(grade))
}

func runSourcesWithLimit(sources []model.FilmSource, h int, tag, trigger string) {
	if len(sources) == 0 {
		return
	}
	sources = filterCollectableSources(sources, tag)
	if len(sources) == 0 {
		return
	}
	markSourcesCollectStarting(sources)
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
	runSourcesGroupWithLimit(sources, h, tag, sourceLimit, runVersion, batch, trigger)
	var finalizeErr error
	if err := collectLifecycle.flushPending(); err != nil {
		syslog.Errorf("[%s] 批量采集收尾刷新失败: %v", tag, err)
		finalizeErr = err
	}
	emitBatchSummaryForSources(batch, trigger, sources, startedAt, finalizeErr)
}

func runSourcesGroupWithLimit(sources []model.FilmSource, h int, tag string, limit int, runVersion uint64, batch *notify.ChangeBatch, trigger string) {
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
				collectWrites.finishSource(skipped.Grade, skipped.Id)
			}
			break
		}
		if isCollectProgressStopped(src.Id) {
			log.Printf("[%s] 站点 %s 已在排队中停止，跳过派发", tag, src.Name)
			collectWrites.finishSource(src.Grade, src.Id)
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
			defer collectWrites.finishSource(fs.Grade, fs.Id)
			if isDispatchStopped(runVersion) {
				log.Printf("[%s] 站点 %s 在启动前被一键终止拦截", tag, fs.Name)
				return
			}
			if isCollectProgressStopped(fs.Id) {
				log.Printf("[%s] 站点 %s 已在启动前停止，跳过采集", tag, fs.Name)
				return
			}
			if err := handleCollectWithStopVersion(fs.Id, h, &runVersion, false, false, batch, trigger); err != nil {
				syslog.Errorf("[%s] 采集站点 %s 失败: %v", tag, fs.Name, err)
			}
		}(src)
	}
	wg.Wait()
}

// HandleCollect 影视采集 id-采集站ID h-时长/h
func HandleCollect(id string, h int) error {
	return handleCollectWithStopVersion(id, h, nil, true, false, nil, model.NotifyTriggerManual)
}

func HandlePreparedCollect(id string, h int) error {
	return handleCollectWithStopVersion(id, h, nil, true, true, nil, model.NotifyTriggerManual)
}

func handleCollectWithStopVersion(id string, h int, runVersion *uint64, flushAtEnd bool, allowPreparedStart bool, batch *notify.ChangeBatch, trigger string) (retErr error) {
	hadWrites := false
	collectStartedAt := time.Now()
	var collectCtx context.Context
	statsOwned := false
	if runVersion != nil && isDispatchStopped(*runVersion) {
		return errors.New("任务已被一键终止，跳过启动")
	}
	if (runVersion != nil || allowPreparedStart) && isCollectProgressStopped(id) {
		return errors.New("任务已被停止，跳过启动")
	}
	if runVersion == nil && !allowPreparedStart && isCollectProgressStarting(id) {
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
	if err := ensureMasterCategoriesReady(s); err != nil {
		retErr = err
		return err
	}
	if flushAtEnd {
		batch = notify.StartChangeBatch()
	}
	isMasterFullCollect := s.Grade == model.MasterCollect && h < 0
	if isMasterFullCollect {
		collectLifecycle.beginMasterRebuild(s.Id)
	}
	defer func() {
		originalErr := retErr
		if trigger == model.NotifyTriggerCron && statsOwned {
			repository.UnsuppressCollectSourceStats(s.Id)
			if shouldNoteCronCollectSuccess(originalErr, collectCtx, s.Id) {
				repository.NoteCollectSourceStats(s.Id)
			}
		}
		flushCollectHotpathSideEffects(s.Id)
		if originalErr != nil && (!hadWrites || shouldSkipCollectPublishOnError(*s, h)) {
			if isMasterFullCollect {
				collectLifecycle.discardPendingMasterMIDs(s.Id)
			}
			collectLifecycle.endSource(s.Id)
			if flushAtEnd {
				noteSourceError(s.Id, originalErr.Error())
				emitBatchSummaryForSources(batch, model.NotifyTriggerManual, []model.FilmSource{*s}, collectStartedAt, originalErr)
			}
			return
		}
		if isMasterFullCollect {
			collectLifecycle.publishPendingMasterMIDs(s.Id)
		}
		if flushAtEnd {
			updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
				if canEnterFinalizing(progress.Status) {
					progress.Status = progressStatusFinalizing
				}
			})
			flushErr := collectLifecycle.finishSourceAndFlush(*s)
			if originalErr == nil && flushErr != nil {
				retErr = flushErr
			}
			updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
				if flushErr != nil && progress.Status == progressStatusFinalizing {
					progress.Status = progressStatusFailed
					return
				}
				if progress.Status == progressStatusFinalizing {
					progress.Status = progressStatusDone
				}
			})
			emitBatchSummaryForSources(batch, model.NotifyTriggerManual, []model.FilmSource{*s}, collectStartedAt, flushErr)
			return
		}
		if !isCollectProgressStopped(s.Id) {
			updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
				switch progress.Status {
				case progressStatusRunning, progressStatusStarting, progressStatusPageDone:
					progress.Status = progressStatusWaitingPublish
				}
			})
		}
		collectLifecycle.endSourceAndQueueFlush(*s)
	}()

	reqId := utils.GenerateSalt()

	taskMu.Lock()
	if runVersion != nil && isDispatchStopped(*runVersion) {
		taskMu.Unlock()
		return errors.New("任务已被一键终止，跳过启动")
	}
	if _, ok := activeTasks.Load(id); ok {
		taskMu.Unlock()
		log.Printf("[Spider] 站点 %s 已有任务正在运行，跳过本次采集...\n", id)
		return fmt.Errorf("站点 %s 已有任务正在运行，已跳过本次采集", id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	collectCtx = ctx
	activeTasks.Store(id, collectTask{cancel: cancel, reqId: reqId})
	taskMu.Unlock()
	if trigger == model.NotifyTriggerCron {
		repository.SuppressCollectSourceStats(s.Id)
		statsOwned = true
	}

	defer func() {
		if val, ok := activeTasks.Load(id); ok {
			if val.(collectTask).reqId == reqId {
				activeTasks.Delete(id)
				updateCollectProgress(id, func(progress *model.CollectProgress) {
					if retErr != nil && progress.Status != progressStatusStopped {
						progress.Status = progressStatusFailed
						return
					}
				})
				if retErr != nil {
					noteSourceError(id, retErr.Error())
				}
				log.Printf("[Spider] 站点 %s 任务结束\n", id)
			}
		}
	}()

	log.Printf("[Spider] 站点 %s 任务启动 (reqId: %s)\n", id, reqId)
	ensureCollectProgress(id, s.Name)

	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	if h == 0 {
		return errors.New("采集时长不能为 0")
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

	pageCount, err := getPageCountWithRetry(ctx, s, r)
	if err != nil {
		return err
	}
	if pageCount <= 0 {
		updateCollectProgress(id, func(progress *model.CollectProgress) {
			progress.Total = 0
			progress.Current = 0
			progress.Success = 0
			progress.Failed = 0
			if flushAtEnd {
				progress.Status = progressStatusPageDone
			} else {
				progress.Status = progressStatusWaitingPublish
			}
		})
		log.Printf("[Spider] 站点 %s 无需分页 (pageCount=%d，该时间段无新内容) flushAtEnd=%v\n", s.Name, pageCount, flushAtEnd)
		return nil
	}
	updateCollectProgress(id, func(progress *model.CollectProgress) {
		progress.Total = pageCount
		progress.Current = 0
		progress.Success = 0
		progress.Failed = 0
		progress.Status = progressStatusRunning
	})
	log.Printf("[Spider] 站点 %s 共 %d 页，开始采集...\n", s.Name, pageCount)

	pageWorkerLimit := getSourcePageConcurrency(s)
	hadWrites, err = collectFilmPages(ctx, pageCount, pageWorkerLimit, s, h, flushAtEnd, batch)
	if err != nil {
		return err
	}
	if isCollectProgressStopped(id) {
		log.Printf("[Spider] 站点 %s 已停止接收新分页，等待收尾刷新\n", s.Name)
	} else {
		markSourcePagesFinished(id, flushAtEnd)
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
	sources = filterCollectableSources(sources, "Batch-Collect")
	if len(sources) == 0 {
		return nil, fmt.Errorf("没有可启动的采集站（均未启用或已在采集中）")
	}
	markSourcesCollectStarting(sources)
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
