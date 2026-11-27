package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/collect/progress"
	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
)

// Batch 采集批次在抓取阶段需要的状态回调接口（实现需 nil-safe）。
type Batch interface {
	IsStandalone() bool
	AddAffectedMIDs(s *model.FilmSource, h int, mids []int64)
	RecordSourceError(sourceID, reason string)
	AddCategorySkip(sourceID string, unmapped int)
}

const (
	consecutiveFetchFailLimit = 20
	consecutiveEmptyPageLimit = 50
	progressLogPageStep       = 100
	failureLogStep            = 10
)

// fetchFailPause 连续失败触发一次的站点熔断暂停时长，测试可调短。
var fetchFailPause = 2 * time.Minute

// Runner 封装一次采集任务所需的所有可注入组件，避免散落全局可变状态。
type Runner struct {
	ResolveCollector func(format string) Collector
	ResolveProxy     ProxyResolver
	SavePage         func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (model.Mids, error)
	OnSourceFailed   func(sourceID, sourceName, reason string)
	Throttle         *ThrottleStore
	WritePool        *WritePool
}

// NewRunner 构造采集执行器，未传项自动回落默认实现。
func NewRunner(cfg Runner) *Runner {
	if cfg.ResolveCollector == nil {
		cfg.ResolveCollector = ResolveCollector
	}
	if cfg.Throttle == nil {
		cfg.Throttle = defaultThrottle
	}
	if cfg.WritePool == nil {
		cfg.WritePool = defaultWritePool
	}
	return &cfg
}

type pageStats struct {
	latestPage  int
	success     int
	failed      int
	failedPages []int
	lastErr     string
}

// CollectPages 并发抓取并落库单站全部目标分页；若传入 Page1Seed 则直接复用第 1 页明细省去重复请求。
func (r *Runner) CollectPages(
	parentCtx context.Context,
	pageCount int,
	targetPages []int,
	seed *Page1Result,
	s *model.FilmSource,
	h int,
	batch Batch,
) (bool, int, int, []int, error) {
	totalWork := pageCount
	useTargetPages := len(targetPages) > 0
	if useTargetPages {
		totalWork = len(targetPages)
	}
	if totalWork <= 0 {
		return false, 0, 0, nil, nil
	}
	if totalWork > MaxCollectPageCount {
		return false, 0, 0, nil, fmt.Errorf("采集页数 %d 超过最大限制 %d", totalWork, MaxCollectPageCount)
	}

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	fetches := newCollectFetch(ctx)
	defer fetches.stopRequests()

	startPage := 1
	seedPg1 := !useTargetPages && seed != nil
	if seedPg1 {
		startPage = 2
	}
	pages := newPageFetchQueue(startPage, pageCount, targetPages)
	go func() {
		<-ctx.Done()
		pages.close()
	}()

	writeCompletions := make(chan WriteResult, 2048)
	var writeWG sync.WaitGroup
	streak, stopStreakWait := newFetchStreak(ctx)
	defer stopStreakWait()
	emptyStreak := newEmptyPageStreak(!useTargetPages && pageCount > consecutiveEmptyPageLimit, consecutiveEmptyPageLimit)

	var emptyStopOnce, stopOnce sync.Once
	var stopErr error
	var stoppedByStreak atomic.Bool
	var statsMu sync.Mutex
	stats := pageStats{}
	lastLoggedDone := 0

	logProgress := func(force bool) {
		statsMu.Lock()
		snap := stats
		done := snap.success + snap.failed
		if !force && (done == lastLoggedDone || (done != totalWork && done%progressLogPageStep != 0)) {
			statsMu.Unlock()
			return
		}
		lastLoggedDone = done
		statsMu.Unlock()
		log.Printf("[Spider] 站点 %s 采集进度 完成=%d/%d，成功=%d，失败=%d，最新页=%d", s.Name, done, totalWork, snap.success, snap.failed, snap.latestPage)
	}

	recordFinished := func(page int, ok bool, err error) pageStats {
		statsMu.Lock()
		if page > stats.latestPage {
			stats.latestPage = page
		}
		if ok {
			stats.success++
		} else {
			stats.failed++
			stats.failedPages = append(stats.failedPages, page)
			if err != nil {
				stats.lastErr = err.Error()
			}
		}
		snap := stats
		statsMu.Unlock()
		return snap
	}

	giveUp := func(reason string) {
		stopOnce.Do(func() {
			stopErr = errors.New(reason)
			stoppedByStreak.Store(true)
			log.Printf("[Spider] 站点 %s(%s) %s", s.Name, s.Id, reason)
			syslog.Errorf("[Spider] 站点 %s(%s) %s", s.Name, s.Id, reason)
			if batch != nil {
				batch.RecordSourceError(s.Id, reason)
			}
			cancel()
		})
	}
	stopForStreak := func() {
		giveUp(fmt.Sprintf("暂停后仍连续 %d 页抓取失败，已中断采集", consecutiveFetchFailLimit))
	}
	clearPause := func() {
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			if cur.Status == progress.StatusPaused {
				cur.Status = progress.StatusRunning
			}
		})
	}
	pauseForStreak := func() bool {
		if ctx.Err() != nil {
			return false
		}
		if pages.finished() {
			return true
		}
		fetches.stopRequests()
		log.Printf("[Spider] 站点 %s 连续 %d 页抓取失败，中断在途请求，暂停 %s 后继续", s.Name, consecutiveFetchFailLimit, fetchFailPause)
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			if !progress.IsTerminalStatus(cur.Status) && cur.Status != progress.StatusStopping {
				cur.Status = progress.StatusPaused
			}
		})
		timer := time.NewTimer(fetchFailPause)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			clearPause()
			return false
		case <-pages.drained:
			if ctx.Err() != nil {
				clearPause()
				return false
			}
		case <-timer.C:
		}
		fetches.resume(ctx)
		clearPause()
		return true
	}
	markStopped := func() {
		if stoppedByStreak.Load() {
			return
		}
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			if cur.Status == progress.StatusStarting || cur.Status == progress.StatusRunning || cur.Status == progress.StatusPaused {
				cur.Status = progress.StatusStopped
			}
		})
	}
	recordFailure := func(page int, stage string, err error) {
		snap := recordFinished(page, false, err)
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			cur.Failed = snap.failed
			cur.Current = snap.success + snap.failed
			cur.FailedPages = append(cur.FailedPages, page)
		})
		if snap.failed == 1 || snap.failed%failureLogStep == 0 {
			syslog.Warnf("[Spider] 站点 %s 采集失败累计=%d，最近失败 page=%d stage=%s err=%v", s.Name, snap.failed, page, stage, err)
		}
		logProgress(false)
	}
	var affectWG sync.WaitGroup
	var affectCh chan []int64
	var affectCloseOnce sync.Once
	if batch != nil {
		affectCh = make(chan []int64, 512)
		affectWG.Add(1)
		go func() {
			defer affectWG.Done()
			for mids := range affectCh {
				batch.AddAffectedMIDs(s, h, mids)
			}
		}()
	}
	closeAffect := func() {
		if affectCh != nil {
			affectCloseOnce.Do(func() {
				close(affectCh)
				affectWG.Wait()
			})
		}
	}
	defer closeAffect()
	emitAffected := func(mids []int64) {
		if affectCh == nil || len(mids) == 0 {
			return
		}
		affectCh <- mids
	}

	recordSuccess := func(page int, affected []int64) {
		snap := recordFinished(page, true, nil)
		emitAffected(affected)
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			cur.Success = snap.success
			cur.Failed = snap.failed
			cur.Current = snap.success + snap.failed
		})
		logProgress(false)
	}

	submitPageWrite := func(page int, items []model.MovieDetail) {
		writeWG.Add(1)
		r.WritePool.Submit(ctx, page, func(writeCtx context.Context) (model.Mids, error) {
			if r.SavePage == nil {
				return model.Mids{}, errors.New("SavePage 未配置")
			}
			return r.SavePage(writeCtx, s, page, items)
		}, func(res WriteResult) {
			writeCompletions <- res
			writeWG.Done()
		})
	}

	if seedPg1 {
		if ctx.Err() != nil {
			markStopped()
			return false, 0, 0, nil, ctx.Err()
		}
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			if !progress.IsTerminalStatus(cur.Status) && cur.Status != progress.StatusStopping && cur.Status != progress.StatusPaused {
				cur.Status = progress.StatusRunning
			}
		})
		_, _ = emptyStreak.record(1, len(seed.Items) == 0, len(seed.Items) > 0)
		submitPageWrite(1, seed.Items)
	}

	remainingWork := totalWork
	if seedPg1 {
		remainingWork--
	}
	maxWorkers := config.PageWorkerLimit()
	if remainingWork < maxWorkers {
		maxWorkers = remainingWork
	}
	if maxWorkers < 0 {
		maxWorkers = 0
	}

	var requestWG sync.WaitGroup
	requestWG.Add(maxWorkers)
	collector := r.ResolveCollector(s.ResolveFormat())
	for i := 0; i < maxWorkers; i++ {
		go func(index int) {
			defer requestWG.Done()
			for {
				if ctx.Err() != nil {
					markStopped()
					return
				}
				if pages.finished() {
					return
				}
				if index >= config.PageWorkerLimit() {
					select {
					case <-ctx.Done():
						markStopped()
						return
					case <-time.After(20 * time.Millisecond):
					}
					continue
				}
				if !streak.tryBegin() {
					if ctx.Err() != nil {
						markStopped()
					}
					return
				}
				abandonStreak := func() {
					if streak.abandon() {
						stopForStreak()
					}
				}
				pg, ok := pages.next(ctx)
				if !ok {
					abandonStreak()
					if ctx.Err() != nil {
						markStopped()
					}
					return
				}
				if ctx.Err() != nil {
					abandonStreak()
					markStopped()
					return
				}
				progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
					if !progress.IsTerminalStatus(cur.Status) && cur.Status != progress.StatusStopping && cur.Status != progress.StatusPaused {
						cur.Status = progress.StatusRunning
					}
				})
				req, reqErr := BuildPageRequest(s, h, pg, r.ResolveProxy)
				var list []model.MovieDetail
				var err error
				var fetchCtx context.Context
				if reqErr != nil {
					err = reqErr
				} else {
					fetchCtx = fetches.current()
					list, err = GetFilmDetailWithRetry(fetchCtx, s, req, collector, r.Throttle)
				}
				if err != nil {
					if ctx.Err() != nil {
						abandonStreak()
						markStopped()
						return
					}
					if fetchCtx != nil && fetchCtx.Err() != nil {
						pages.putBack(pg)
						abandonStreak()
						continue
					}
					recordFailure(pg, "fetch", err)
					emptyStreak.record(pg, false, false)
					stop, pause := streak.finish(false)
					if stop {
						stopForStreak()
					}
					pages.done()
					if pause && !pauseForStreak() {
						markStopped()
						return
					}
					if pause {
						streak.resumeIntake()
					}
					continue
				}
				if ctx.Err() != nil {
					abandonStreak()
					markStopped()
					return
				}
				stop, pause := streak.finish(true)
				if stop {
					pages.done()
					continue
				}
				if pause && !pauseForStreak() {
					pages.done()
					markStopped()
					return
				}
				if pause {
					streak.resumeIntake()
				}
				pages.done()
				if ctx.Err() != nil {
					markStopped()
					return
				}
				if reached, hadData := emptyStreak.record(pg, len(list) == 0, len(list) > 0); reached {
					if hadData {
						emptyStopOnce.Do(func() {
							log.Printf("[Spider] 站点 %s 连续 %d 页返回空列表，提前结束后续分页抓取 (totalWork=%d)", s.Name, consecutiveEmptyPageLimit, totalWork)
							pages.close()
						})
					} else {
						giveUp(fmt.Sprintf("连续 %d 页返回空列表，已中断采集", consecutiveEmptyPageLimit))
					}
				}
				submitPageWrite(pg, list)
			}
		}(i)
	}

	go func() {
		requestWG.Wait()
		writeWG.Wait()
		close(writeCompletions)
	}()

	for res := range writeCompletions {
		if res.Err != nil {
			if batch != nil {
				if res.Unmapped > 0 {
					batch.AddCategorySkip(s.Id, res.Unmapped)
				}
				if len(res.AffectedMIDs) > 0 {
					emitAffected(res.AffectedMIDs)
				}
			}
			if ctx.Err() != nil || errors.Is(res.Err, context.Canceled) {
				continue
			}
			recordFailure(res.Page, res.Stage, res.Err)
			continue
		}
		if batch != nil && res.Unmapped > 0 {
			batch.AddCategorySkip(s.Id, res.Unmapped)
		}
		recordSuccess(res.Page, res.AffectedMIDs)
	}
	closeAffect()

	logProgress(true)
	if len(stats.failedPages) > 0 {
		sort.Ints(stats.failedPages)
	}
	if stopErr != nil {
		if stats.success == 0 {
			progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
				if !progress.IsTerminalStatus(cur.Status) && cur.Status != progress.StatusStopping {
					cur.Status = progress.StatusFailed
					cur.QueueId = ""
				}
			})
			if r.OnSourceFailed != nil {
				r.OnSourceFailed(s.Id, s.Name, stopErr.Error())
			}
		}
		return stats.success > 0, stats.success, stats.failed, stats.failedPages, stopErr
	}
	if ctx.Err() != nil {
		return stats.success > 0, stats.success, stats.failed, stats.failedPages, ctx.Err()
	}
	if stats.failed > 0 {
		err := fmt.Errorf("站点 %s %s", s.Name, formatFailedPagesError(stats.failedPages, stats.failed, stats.lastErr))
		if stats.success == 0 && r.OnSourceFailed != nil {
			r.OnSourceFailed(s.Id, s.Name, err.Error())
		}
		return stats.success > 0, stats.success, stats.failed, stats.failedPages, err
	}
	return stats.success > 0, stats.success, stats.failed, stats.failedPages, nil
}
