// Package fetcher 采集取数：分页结果投递（页并发 / 写调度 / 连续失败停抓）与进度回报。
//
// 与 fetcher.go 同属一个「取数」模块：本文件负责单站分页的并发调度与收尾判定，
// 外部能力（落库、通知、状态机）全部通过 Deps 注入，不反向依赖 spider 根包。
package fetcher

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/spider/progress"
	"server/internal/spider/scheduler"
)

// Batch 采集批次上下文在抓取阶段需要的能力；调用方可传 nil（全部方法需 nil-safe）。
type Batch interface {
	// IsStandalone 单站采集（不与其他站点合并发布）。
	IsStandalone() bool
	// AddAffectedMIDs 记录影响到的全局 mid，供本轮发布使用。
	AddAffectedMIDs(s *model.FilmSource, h int, mids []int64)
	// NoteCollectedMIDs 累计本源应进更新列表的 mid。
	NoteCollectedMIDs(sourceID, sourceName string, mids []int64)
}

const (
	// sourceConsecutiveFailureLimit 单站连续失败达到该值即终止本次采集，转收尾发布。
	sourceConsecutiveFailureLimit = 10
	// progressLogPageStep 采集进度日志的页步长。
	progressLogPageStep = 100
	// failureLogStep 失败日志的步长。
	failureLogStep = 10
)

// shouldWrapUpAfterFetchAbort 连续失败停抓后，已有成功入库且允许发布时进入收尾，而不是立刻 failed/stopped。
func shouldWrapUpAfterFetchAbort(success int, skipPublish bool) bool {
	return success > 0 && !skipPublish
}

type pageStats struct {
	latestPage int
	success    int
	failed     int
}

func shouldLogProgress(done, total int) bool {
	return done == total || done%progressLogPageStep == 0
}

func shouldLogFailure(failed int) bool {
	return failed == 1 || failed%failureLogStep == 0
}

func CollectPages(parentCtx context.Context, pageCount int, requestWorkerLimit int, s *model.FilmSource, h int, batch Batch) (bool, error) {
	if pageCount <= 0 {
		return false, nil
	}
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	if requestWorkerLimit <= 0 {
		requestWorkerLimit = 1
	}
	requestWorkers := min(pageCount, requestWorkerLimit)

	pages := make(chan int, pageCount)
	writeCompletions := make(chan scheduler.Completion, pageCount)
	for pg := 1; pg <= pageCount; pg++ {
		pages <- pg
	}
	close(pages)

	var writeWG sync.WaitGroup
	var consecutiveFailuresMu sync.Mutex
	consecutiveFailures := 0
	var stopErr error
	var stopOnce sync.Once
	var statsMu sync.Mutex
	stats := pageStats{}
	lastLoggedDone := 0
	logProgress := func(force bool) {
		statsMu.Lock()
		snapshot := stats
		done := snapshot.success + snapshot.failed
		if !force {
			if done == lastLoggedDone || !shouldLogProgress(done, pageCount) {
				statsMu.Unlock()
				return
			}
		}
		lastLoggedDone = done
		statsMu.Unlock()

		log.Printf("[Spider] 站点 %s 采集进度 完成=%d/%d，成功=%d，失败=%d，最新页=%d", s.Name, done, pageCount, snapshot.success, snapshot.failed, snapshot.latestPage)
	}
	recordPageFinished := func(page int, success bool) pageStats {
		statsMu.Lock()
		if page > stats.latestPage {
			stats.latestPage = page
		}
		if success {
			stats.success++
		} else {
			stats.failed++
		}
		snapshot := stats
		statsMu.Unlock()
		return snapshot
	}
	recordFailure := func(page int, stage string, err error) {
		consecutiveFailuresMu.Lock()
		consecutiveFailures++
		currentFailures := consecutiveFailures
		consecutiveFailuresMu.Unlock()

		if currentFailures < sourceConsecutiveFailureLimit {
			return
		}
		stopOnce.Do(func() {
			stopErr = fmt.Errorf("站点 %s 连续采集失败 %d 次，已终止本次采集", s.Name, sourceConsecutiveFailureLimit)
			syslog.Errorf("[Spider] 站点 %s 连续失败达到阈值，终止采集 page=%d stage=%s err=%v", s.Name, page, stage, err)
			cancel()
		})
	}
	recordSuccess := func() {
		consecutiveFailuresMu.Lock()
		consecutiveFailures = 0
		consecutiveFailuresMu.Unlock()
	}
	markStopped := func() {
		// 连续失败停抓走收尾，不能写成用户「已停止」。
		if stopErr != nil {
			return
		}
		progress.MarkStopped(s.Id)
	}
	maybeMarkPagePhaseDone := func(cur *model.CollectProgress, snapshot pageStats) {
		if cur.Status != progress.StatusRunning && cur.Status != progress.StatusStarting {
			return
		}
		if cur.Total > 0 && snapshot.success+snapshot.failed >= cur.Total {
			cur.Status = progress.StatusPageDone
		}
	}
	recordPageFailure := func(page int, stage string, err error) {
		snapshot := recordPageFinished(page, false)
		progress.Update(s.Id, func(cur *model.CollectProgress) {
			cur.Failed = snapshot.failed
			if page > cur.Current {
				cur.Current = page
			}
			maybeMarkPagePhaseDone(cur, snapshot)
		})
		deps.SavePageFailure(s, h, page, stage, err)
		if shouldLogFailure(snapshot.failed) {
			syslog.Warnf("[Spider] 站点 %s 采集失败累计=%d，最近失败 page=%d stage=%s err=%v", s.Name, snapshot.failed, page, stage, err)
		}
		recordFailure(page, stage, err)
		logProgress(false)
	}
	recordPageSuccess := func(page int, notifyMIDs, affectedMIDs []int64) {
		snapshot := recordPageFinished(page, true)
		recordSuccess()
		if batch != nil {
			batch.NoteCollectedMIDs(s.Id, s.Name, notifyMIDs)
			batch.AddAffectedMIDs(s, h, affectedMIDs)
		}
		progress.Update(s.Id, func(cur *model.CollectProgress) {
			cur.Success = snapshot.success
			cur.Failed = snapshot.failed
			if page > cur.Current {
				cur.Current = page
			}
			maybeMarkPagePhaseDone(cur, snapshot)
		})
		logProgress(false)
	}

	var requestWG sync.WaitGroup
	requestWG.Add(requestWorkers)
	for i := 0; i < requestWorkers; i++ {
		go func() {
			defer requestWG.Done()
			for {
				select {
				case <-ctx.Done():
					markStopped()
					return
				case pg, ok := <-pages:
					if !ok {
						return
					}
					progress.Update(s.Id, func(cur *model.CollectProgress) {
						progress.StampPageRunning(cur, pg)
					})
					list, err := GetFilmDetailWithRetry(ctx, s, buildPageRequest(s, h, pg))
					if err == nil && len(list) == 0 {
						err = errors.New("response list is empty")
					}
					if err != nil {
						if ctx.Err() != nil {
							markStopped()
							return
						}
						recordPageFailure(pg, "fetch", err)
						continue
					}
					page := pg
					items := list
					writeWG.Add(1)
					submitErr := scheduler.Submit(ctx, scheduler.Job{
						SourceID:   s.Id,
						SourceName: s.Name,
						Grade:      s.Grade,
						Page:       page,
						Write: func() (scheduler.Mids, error) {
							return deps.SavePage(context.Background(), s, page, items)
						},
						Complete: func(completion scheduler.Completion) {
							writeCompletions <- completion
							writeWG.Done()
						},
					})
					if submitErr != nil {
						writeWG.Done()
						if ctx.Err() != nil {
							markStopped()
							return
						}
						recordPageFailure(page, "enqueue", submitErr)
						continue
					}
				}
			}
		}()
	}
	go func() {
		requestWG.Wait()
		scheduler.FinishSource(s.Grade, s.Id)
		writeWG.Wait()
		close(writeCompletions)
	}()

	for completion := range writeCompletions {
		if completion.Err != nil {
			if ctx.Err() != nil || errors.Is(completion.Err, context.Canceled) {
				continue
			}
			recordPageFailure(completion.Page, completion.Stage, completion.Err)
			continue
		}
		recordPageSuccess(completion.Page, completion.NotifyMIDs, completion.AffectedMIDs)
	}
	logProgress(true)
	if ctx.Err() != nil {
		log.Printf("[Spider] 站点 %s 并发采集任务已中断，worker 已全部退出\n", s.Name)
	}
	if stopErr != nil {
		skipPublish := deps.SkipPublishOnError(*s, h)
		if shouldWrapUpAfterFetchAbort(stats.success, skipPublish) {
			deps.NoteSourceError(s.Id, stopErr.Error())
			return true, nil
		}
		progress.Update(s.Id, func(cur *model.CollectProgress) {
			if !progress.IsTerminalStatus(cur.Status) {
				cur.Status = progress.StatusFailed
			}
		})
		isStandalone := batch.IsStandalone()
		if isStandalone || !deps.BatchSummaryEnabled() {
			deps.NotifySourceFailed(s.Id, s.Name, stopErr.Error())
		}
		deps.NoteSourceError(s.Id, stopErr.Error())
		return stats.success > 0, stopErr
	}
	if s.Grade == model.MasterCollect && h < 0 && stats.failed > 0 {
		return stats.success > 0, fmt.Errorf("主站全量采集存在失败页 failed=%d，跳过本次框架发布", stats.failed)
	}
	if ctx.Err() != nil {
		return stats.success > 0, nil
	}
	return stats.success > 0, nil
}
