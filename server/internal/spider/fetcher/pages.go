// Package fetcher 采集取数：分页结果投递（页并发 / 写调度 / 连续抓取失败中断）与进度回报。
//
// 与 fetcher.go 同属一个「取数」模块：本文件负责单站分页的并发调度与收尾判定，
// 外部能力（落库、通知、状态机）全部通过 Deps 注入，不反向依赖 spider 根包。
package fetcher

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/config"
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
	// AddNotifyMIDs 记录应进通知更新列表的全局 mid（新片或播放线路实质变更）。
	AddNotifyMIDs(sourceID string, mids []int64)
	// RecordSourceError 记录站点采集错误原因。
	RecordSourceError(sourceID, reason string)
	// TouchRecord 把已写入的影片和页数落到运行中的采集记录。
	TouchRecord()
	// AddCategorySkip 累计分类未映射而没有入库的部数。页请求仍算成功。
	AddCategorySkip(sourceID string, unmapped int)
}

const (
	// consecutiveFetchFailLimit 连续这么多页抓取失败先暂停一次。
	// 暂停结束后再连续这么多页失败，才中断本站这一轮。
	consecutiveFetchFailLimit = 20
	// progressLogPageStep 采集进度日志的页步长。
	progressLogPageStep = 100
	// failureLogStep 失败日志的步长。
	failureLogStep = 10
)

// fetchFailPause 连续失败后的限流暂停。只暂停一次，不再加长。测试可调短。
var fetchFailPause = 2 * time.Minute

type pageStats struct {
	latestPage  int
	success     int
	failed      int
	failedPages []int
	lastErr     string
}

func shouldLogProgress(done, total int) bool {
	return done == total || done%progressLogPageStep == 0
}

func shouldLogFailure(failed int) bool {
	return failed == 1 || failed%failureLogStep == 0
}

// pageFetchQueue 待抓页。全部落地或采集被取消后关闭。
type pageFetchQueue struct {
	mu     sync.Mutex
	ch     chan int
	closed bool
	left   int
}

func newPageFetchQueue(pages []int) *pageFetchQueue {
	queue := &pageFetchQueue{
		ch:   make(chan int, len(pages)),
		left: len(pages),
	}
	for _, pg := range pages {
		queue.ch <- pg
	}
	return queue
}

func (q *pageFetchQueue) finished() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed
}

func (q *pageFetchQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closeLocked()
}

func (q *pageFetchQueue) closeLocked() {
	if q.closed {
		return
	}
	q.closed = true
	close(q.ch)
}

func (q *pageFetchQueue) done() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.left--
	if q.left <= 0 {
		q.closeLocked()
	}
}

func CollectPages(parentCtx context.Context, pageCount int, targetPages []int, s *model.FilmSource, h int, batch Batch) (bool, int, int, []int, error) {
	totalWork := pageCount
	useTargetPages := len(targetPages) > 0
	if useTargetPages {
		totalWork = len(targetPages)
	}
	if totalWork <= 0 {
		return false, 0, 0, nil, nil
	}
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	maxWorkers := config.PageWorkerLimit()
	if totalWork < maxWorkers {
		maxWorkers = totalWork
	}

	seed := make([]int, 0, totalWork)
	if useTargetPages {
		seed = append(seed, targetPages...)
	} else {
		for pg := 1; pg <= pageCount; pg++ {
			seed = append(seed, pg)
		}
	}
	pages := newPageFetchQueue(seed)
	go func() {
		<-ctx.Done()
		pages.close()
	}()
	writeCompletions := make(chan scheduler.Completion, 2048)

	var writeWG sync.WaitGroup
	streak, stopStreakWait := newFetchStreak(ctx, s.Name)
	defer stopStreakWait()
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
			if done == lastLoggedDone || !shouldLogProgress(done, totalWork) {
				statsMu.Unlock()
				return
			}
		}
		lastLoggedDone = done
		statsMu.Unlock()

		log.Printf("[Spider] 站点 %s 采集进度 完成=%d/%d，成功=%d，失败=%d，最新页=%d", s.Name, done, totalWork, snapshot.success, snapshot.failed, snapshot.latestPage)
	}
	recordPageFinished := func(page int, success bool, err error) pageStats {
		statsMu.Lock()
		if page > stats.latestPage {
			stats.latestPage = page
		}
		if success {
			stats.success++
		} else {
			stats.failed++
			stats.failedPages = append(stats.failedPages, page)
			if err != nil {
				stats.lastErr = err.Error()
			}
		}
		snapshot := stats
		statsMu.Unlock()
		return snapshot
	}
	giveUp := func(reason string) {
		stopOnce.Do(func() {
			stopErr = errors.New(reason)
			log.Printf("[Spider] 站点 %s(%s) %s", s.Name, s.Id, reason)
			syslog.Errorf("[Spider] 站点 %s(%s) %s", s.Name, s.Id, reason)
			if batch != nil {
				batch.RecordSourceError(s.Id, reason)
			}
			proven := streak.succeeded()
			if !proven && deps.OnSourceFailed != nil {
				deps.OnSourceFailed(s.Id, s.Name, reason)
			}
			cancel()
		})
	}
	stopForStreak := func() {
		giveUp(fmt.Sprintf("暂停后仍连续 %d 页抓取失败，已中断采集", consecutiveFetchFailLimit))
	}
	// pauseForStreak 连续失败后整站停 2 分钟再继续。只此一次，暂停期间不占页并发和取数名额。
	pauseForStreak := func() bool {
		log.Printf("[Spider] 站点 %s 连续 %d 页抓取失败，暂停 %s 后继续", s.Name, consecutiveFetchFailLimit, fetchFailPause)
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			if progress.IsTerminalStatus(cur.Status) || cur.Status == progress.StatusStopping {
				return
			}
			cur.Status = progress.StatusPaused
		})
		timer := time.NewTimer(fetchFailPause)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
				if cur.Status == progress.StatusPaused {
					cur.Status = progress.StatusRunning
				}
			})
			return false
		case <-timer.C:
		}
		log.Printf("[Spider] 站点 %s 暂停结束，继续采集", s.Name)
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			if cur.Status == progress.StatusPaused {
				cur.Status = progress.StatusRunning
			}
		})
		return true
	}
	markStopped := func() {
		// 连续失败中断不写成用户「已停止」，否则采集记录会丢掉中断原因。
		if stopErr != nil {
			return
		}
		progress.MarkStopped(s.Id)
	}
	recordPageFailure := func(page int, stage string, err error) {
		snapshot := recordPageFinished(page, false, err)
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			cur.Failed = snapshot.failed
			cur.Current = snapshot.success + snapshot.failed
			cur.FailedPages = append(cur.FailedPages, page)
		})
		if shouldLogFailure(snapshot.failed) {
			syslog.Warnf("[Spider] 站点 %s 采集失败累计=%d，最近失败 page=%d stage=%s err=%v", s.Name, snapshot.failed, page, stage, err)
		}
		if batch != nil {
			batch.TouchRecord()
		}
		logProgress(false)
	}
	recordPageSuccess := func(page int, notifyMIDs, affectedMIDs []int64) {
		snapshot := recordPageFinished(page, true, nil)
		if batch != nil {
			batch.AddAffectedMIDs(s, h, affectedMIDs)
			batch.AddNotifyMIDs(s.Id, notifyMIDs)
		}
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			cur.Success = snapshot.success
			cur.Failed = snapshot.failed
			cur.Current = snapshot.success + snapshot.failed
		})
		if batch != nil {
			batch.TouchRecord()
		}
		logProgress(false)
	}

	var requestWG sync.WaitGroup
	requestWG.Add(maxWorkers)
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
				select {
				case <-ctx.Done():
					if streak.abandon() {
						stopForStreak()
					}
					markStopped()
					return
				case pg, ok := <-pages.ch:
					if !ok {
						if streak.abandon() {
							stopForStreak()
						}
						return
					}
					if ctx.Err() != nil {
						if streak.abandon() {
							stopForStreak()
						}
						markStopped()
						return
					}
					progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
						if progress.IsTerminalStatus(cur.Status) || cur.Status == progress.StatusStopping || cur.Status == progress.StatusPaused {
							return
						}
						cur.Status = progress.StatusRunning
					})
					req, reqErr := buildPageRequest(s, h, pg)
					var list []model.MovieDetail
					var err error
					if reqErr != nil {
						err = reqErr
					} else {
						list, err = GetFilmDetailWithRetry(ctx, s, req)
					}
					if err != nil {
						if ctx.Err() != nil {
							streak.abandon()
							markStopped()
							return
						}
						recordPageFailure(pg, "fetch", err)
						stop, pause := streak.finish(false)
						if stop {
							stopForStreak()
						}
						// 先入账再暂停。暂停的 2 分钟里点停止，这一页也已经从队列计数里去掉。
						pages.done()
						if pause && !pauseForStreak() {
							markStopped()
							return
						}
						continue
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
					pages.done()
					page := pg
					items := list
					writeWG.Add(1)
					submitErr := scheduler.Submit(ctx, scheduler.Job{
						SourceID:   s.Id,
						SourceName: s.Name,
						Page:       page,
						Write: func() (scheduler.Mids, error) {
							writeCtx, cancelWrite := context.WithTimeout(context.Background(), 30*time.Second)
							defer cancelWrite()
							return deps.SavePage(writeCtx, s, page, items)
						},
						Complete: func(completion scheduler.Completion) {
							select {
							case writeCompletions <- completion:
								writeWG.Done()
							default:
								go func() {
									writeCompletions <- completion
									writeWG.Done()
								}()
							}
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
		}(i)
	}
	go func() {
		requestWG.Wait()
		scheduler.FinishSource(s.Id)
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
		if batch != nil && completion.Unmapped > 0 {
			batch.AddCategorySkip(s.Id, completion.Unmapped)
		}
		recordPageSuccess(completion.Page, completion.NotifyMIDs, completion.AffectedMIDs)
	}
	logProgress(true)
	if ctx.Err() != nil {
		log.Printf("[Spider] 站点 %s 并发采集任务已中断，worker 已全部退出\n", s.Name)
	}
	if len(stats.failedPages) > 0 {
		sort.Ints(stats.failedPages)
	}
	if stopErr != nil {
		// 还有成功页时保持运行中，调用方才能把已经抓到的影片送去封口。
		if stats.success == 0 {
			progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
				if progress.IsTerminalStatus(cur.Status) || cur.Status == progress.StatusStopping {
					return
				}
				cur.Status = progress.StatusFailed
				cur.QueueId = ""
			})
		}
		return stats.success > 0, stats.success, stats.failed, stats.failedPages, stopErr
	}
	if ctx.Err() != nil {
		return stats.success > 0, stats.success, stats.failed, stats.failedPages, ctx.Err()
	}
	if stats.failed > 0 {
		errMsg := formatFailedPagesError(stats.failedPages, stats.failed, stats.lastErr)
		return stats.success > 0, stats.success, stats.failed, stats.failedPages, fmt.Errorf("站点 %s %s", s.Name, errMsg)
	}
	return stats.success > 0, stats.success, stats.failed, stats.failedPages, nil
}

func formatFailedPagesError(failedPages []int, totalFailed int, lastErr string) string {
	if totalFailed <= 0 {
		return ""
	}
	pagesDesc := fmt.Sprintf("共 %d 页失败", totalFailed)
	if totalFailed == 1 && len(failedPages) == 1 {
		pagesDesc = fmt.Sprintf("第 %d 页失败", failedPages[0])
	}
	trimmedErr := trimErrorRunes(lastErr, failedPageErrorRunes)
	if trimmedErr != "" {
		return pagesDesc + "。抓取失败: " + trimmedErr
	}
	return pagesDesc
}

const failedPageErrorRunes = 2000

func trimErrorRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
