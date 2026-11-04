// Package fetcher 采集取数：分页结果投递（页并发 / 写调度 / 零成功停站）与进度回报。
//
// 与 fetcher.go 同属一个「取数」模块：本文件负责单站分页的并发调度与收尾判定，
// 外部能力（落库、通知、状态机）全部通过 Deps 注入，不反向依赖 spider 根包。
package fetcher

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

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
}

const (
	// sourceStartupFailLimit 没有任何成功页时，取页失败达到该次数就停止。
	sourceStartupFailLimit = 32
	// progressLogPageStep 采集进度日志的页步长。
	progressLogPageStep = 100
	// failureLogStep 失败日志的步长。
	failureLogStep = 10
)

func shouldStopUnprovenSource(fetchOK, fetchFails int) bool {
	return fetchOK == 0 && fetchFails >= sourceStartupFailLimit
}

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

type dynamicLimiter struct {
	mu      sync.Mutex
	cond    *sync.Cond
	running int
	limitFn func() int
}

func newDynamicLimiter(limitFn func() int) *dynamicLimiter {
	l := &dynamicLimiter{limitFn: limitFn}
	l.cond = sync.NewCond(&l.mu)
	return l
}

func (l *dynamicLimiter) startProbe(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				l.mu.Lock()
				l.cond.Broadcast()
				l.mu.Unlock()
				return
			case <-ticker.C:
				l.mu.Lock()
				if l.running < l.limitFn() {
					l.cond.Broadcast()
				}
				l.mu.Unlock()
			}
		}
	}()
}

func (l *dynamicLimiter) acquire(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() {
		l.mu.Lock()
		l.cond.Broadcast()
		l.mu.Unlock()
	})
	defer stop()

	l.mu.Lock()
	defer l.mu.Unlock()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		limit := l.limitFn()
		if limit <= 0 {
			limit = 1
		}
		if l.running < limit {
			l.running++
			return nil
		}
		l.cond.Wait()
	}
}

func (l *dynamicLimiter) release() {
	l.mu.Lock()
	if l.running > 0 {
		l.running--
	}
	l.cond.Broadcast()
	l.mu.Unlock()
}

func CollectPages(parentCtx context.Context, pageCount int, s *model.FilmSource, h int, batch Batch) (bool, int, int, error) {
	if pageCount <= 0 {
		return false, 0, 0, nil
	}
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	maxWorkers := min(pageCount, GetSourcePageConcurrencyMax(s))
	if maxWorkers <= 0 {
		maxWorkers = 1
	}

	limiter := newDynamicLimiter(func() int {
		return GetSourcePageConcurrency(s)
	})
	limiter.startProbe(ctx, 300*time.Millisecond)

	pages := make(chan int, maxWorkers)
	writeCompletions := make(chan scheduler.Completion, 2048)
	go func() {
		defer close(pages)
		for pg := 1; pg <= pageCount; pg++ {
			select {
			case <-ctx.Done():
				return
			case pages <- pg:
			}
		}
	}()

	var writeWG sync.WaitGroup
	var fetchMu sync.Mutex
	fetchOK := 0
	fetchFails := 0
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
	recordPageFinished := func(page int, success bool, err error) pageStats {
		statsMu.Lock()
		if page > stats.latestPage {
			stats.latestPage = page
		}
		if success {
			stats.success++
		} else {
			stats.failed++
			if len(stats.failedPages) < 10 {
				stats.failedPages = append(stats.failedPages, page)
			}
			if err != nil {
				stats.lastErr = err.Error()
			}
		}
		snapshot := stats
		statsMu.Unlock()
		return snapshot
	}
	noteFetchOK := func() {
		fetchMu.Lock()
		fetchOK++
		fetchMu.Unlock()
	}
	noteFetchFail := func(page int, err error) {
		fetchMu.Lock()
		fetchFails++
		ok, fails := fetchOK, fetchFails
		fetchMu.Unlock()
		if !shouldStopUnprovenSource(ok, fails) {
			return
		}
		stopOnce.Do(func() {
			stopErr = fmt.Errorf("站点 %s 已失败 %d 页且尚无成功页，已停止本次采集", s.Name, sourceStartupFailLimit)
			syslog.Errorf("[Spider] 站点 %s 尚无成功页，停止采集 page=%d err=%v", s.Name, page, err)
			if batch != nil {
				batch.RecordSourceError(s.Id, stopErr.Error())
			}
			if deps.OnSourceFailed != nil {
				deps.OnSourceFailed(s.Id, s.Name, stopErr.Error())
			}
			cancel()
		})
	}
	markStopped := func() {
		// 零成功停站不写成用户「已停止」。
		if stopErr != nil {
			return
		}
		progress.MarkStopped(s.Id)
	}
	recordPageFailure := func(page int, stage string, err error) {
		snapshot := recordPageFinished(page, false, err)
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			cur.Failed = snapshot.failed
			if page > cur.Current {
				cur.Current = page
			}
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
			if page > cur.Current {
				cur.Current = page
			}
		})
		if batch != nil {
			batch.TouchRecord()
		}
		logProgress(false)
	}

	var requestWG sync.WaitGroup
	requestWG.Add(maxWorkers)
	for i := 0; i < maxWorkers; i++ {
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
					if err := limiter.acquire(ctx); err != nil {
						markStopped()
						return
					}
					list, err := func() ([]model.MovieDetail, error) {
						defer limiter.release()
						progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
							progress.StampPageRunning(cur, pg)
						})
						req, reqErr := buildPageRequest(s, h, pg)
						if reqErr != nil {
							return nil, reqErr
						}
						return GetFilmDetailWithRetry(ctx, s, req)
					}()
					if err == nil && len(list) == 0 {
						err = errors.New("response list is empty")
					}
					if err != nil {
						if ctx.Err() != nil {
							markStopped()
							return
						}
						recordPageFailure(pg, "fetch", err)
						noteFetchFail(pg, err)
						continue
					}
					noteFetchOK()
					page := pg
					items := list
					writeWG.Add(1)
					// 停站取消这次提交。已经开始的写事务仍用独立上下文写完。
					submitErr := scheduler.Submit(ctx, scheduler.Job{
						SourceID:   s.Id,
						SourceName: s.Name,
						Page:       page,
						Write: func() (scheduler.Mids, error) {
							return deps.SavePage(context.Background(), s, page, items)
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
		}()
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
		recordPageSuccess(completion.Page, completion.NotifyMIDs, completion.AffectedMIDs)
	}
	logProgress(true)
	if ctx.Err() != nil {
		log.Printf("[Spider] 站点 %s 并发采集任务已中断，worker 已全部退出\n", s.Name)
	}
	if stopErr != nil {
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			if progress.IsTerminalStatus(cur.Status) || cur.Status == progress.StatusStopping {
				return
			}
			cur.Status = progress.StatusFailed
			cur.QueueId = ""
		})
		return stats.success > 0, stats.success, stats.failed, stopErr
	}
	if ctx.Err() != nil {
		return stats.success > 0, stats.success, stats.failed, ctx.Err()
	}
	if stats.failed > 0 {
		errMsg := formatFailedPagesError(stats.failedPages, stats.failed, stats.lastErr)
		return stats.success > 0, stats.success, stats.failed, fmt.Errorf("站点 %s %s", s.Name, errMsg)
	}
	return stats.success > 0, stats.success, stats.failed, nil
}

func formatFailedPagesError(failedPages []int, totalFailed int, lastErr string) string {
	if totalFailed <= 0 {
		return ""
	}
	var pagesDesc string
	if len(failedPages) == 1 {
		pagesDesc = fmt.Sprintf("第 %d 页", failedPages[0])
	} else if len(failedPages) > 0 {
		strPages := make([]string, len(failedPages))
		for i, p := range failedPages {
			strPages[i] = strconv.Itoa(p)
		}
		if totalFailed > len(failedPages) {
			pagesDesc = fmt.Sprintf("共 %d 页失败 (第 %s... 页)", totalFailed, strings.Join(strPages, ", "))
		} else {
			pagesDesc = fmt.Sprintf("第 %s 页", strings.Join(strPages, ", "))
		}
	} else {
		pagesDesc = fmt.Sprintf("共 %d 页", totalFailed)
	}

	trimmedErr := strings.TrimSpace(lastErr)
	if trimmedErr != "" {
		if len(trimmedErr) > 120 {
			trimmedErr = trimmedErr[:120] + "..."
		}
		return fmt.Sprintf("%s抓取失败: %s", pagesDesc, trimmedErr)
	}
	return fmt.Sprintf("%s抓取失败", pagesDesc)
}
