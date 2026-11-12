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
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/spider/progress"
	"server/internal/spider/scheduler"
	"server/internal/utils"
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

// 连续失败暂停：站点已有成功页后，连续 sourcePauseFailStreak 页取页失败（多半是被源站按 IP 限流），
// 整站暂停 sourcePauseDuration 再从断点继续；失败的页照常记为失败，留给「重试失败页」。
// 连续暂停 sourcePauseMaxTimes 次仍未恢复，再撞上一轮连续失败就结束本站这一轮。包级变量，测试可调小。
//
// EOF、连接重置和单页超时经常是同一小波并发一起失败。这种页先整站短暂停，再补抓，不立刻记失败。
var (
	sourcePauseFailStreak = 20
	sourcePauseDuration   = 5 * time.Minute
	sourcePauseMaxTimes   = 3
	// transientPageRetries 短暂停之后，同一页最多再补几轮。
	transientPageRetries = 2
	// transientPauseAfter 连续这么多页临时失败就停手，避免并发把源站按死。
	transientPauseAfter = 4
	transientRetryPause = 15 * time.Second
	// maxTransientPauses 短暂停次数。用完后不再补抓，改走上面的长暂停。
	maxTransientPauses = 3
)

type pageFetch struct {
	pg        int
	transient int
}

// pageFetchQueue 待抓页。补抓放回队尾；全部落地或采集被取消后关闭。
type pageFetchQueue struct {
	mu     sync.Mutex
	ch     chan pageFetch
	closed bool
	left   int
}

func newPageFetchQueue(pages []int) *pageFetchQueue {
	queue := &pageFetchQueue{
		ch:   make(chan pageFetch, len(pages)),
		left: len(pages),
	}
	for _, pg := range pages {
		queue.ch <- pageFetch{pg: pg}
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

func (q *pageFetchQueue) requeue(ctx context.Context, job pageFetch) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || ctx.Err() != nil {
		return false
	}
	select {
	case q.ch <- job:
		return true
	default:
		return false
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

	maxWorkers := config.CollectWriteSlotMax
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
	// 连续失败暂停的状态，与 fetchOK/fetchFails 同受 fetchMu 保护。
	failStreak := 0
	transientStreak := 0
	transientPauses := 0
	pausesInRow := 0
	var pausedUntil time.Time
	noteFetchOK := func() {
		fetchMu.Lock()
		fetchOK++
		failStreak = 0
		transientStreak = 0
		pausesInRow = 0
		fetchMu.Unlock()
	}
	giveUp := func(reason string) {
		stopOnce.Do(func() {
			stopErr = errors.New(reason)
			syslog.Errorf("[Spider] %s", reason)
			if batch != nil {
				batch.RecordSourceError(s.Id, reason)
			}
			cancel()
		})
	}
	// waitPause 暂停中则等到暂停结束；停止或终止时立刻返回 false。
	waitPause := func() bool {
		for {
			fetchMu.Lock()
			wait := time.Until(pausedUntil)
			fetchMu.Unlock()
			if wait <= 0 {
				return ctx.Err() == nil
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false
			case <-timer.C:
			}
		}
	}
	// notePauseStreak 已有成功页的站连续失败到阈值时整站暂停；暂停次数用完则结束本轮。
	// 暂停期间才返回的失败（暂停前已发出的请求）不计入下一轮连续失败。
	notePauseStreak := func() {
		fetchMu.Lock()
		if fetchOK == 0 || time.Now().Before(pausedUntil) {
			fetchMu.Unlock()
			return
		}
		failStreak++
		if failStreak < sourcePauseFailStreak {
			fetchMu.Unlock()
			return
		}
		failStreak = 0
		if pausesInRow >= sourcePauseMaxTimes {
			fetchMu.Unlock()
			log.Printf("[Spider] 站点 %s 连续暂停 %d 次后仍连续失败 %d 页，结束本轮采集", s.Name, sourcePauseMaxTimes, sourcePauseFailStreak)
			giveUp(fmt.Sprintf("站点 %s 连续暂停 %d 次后仍连续失败 %d 页，已结束本轮采集，失败页可稍后重试", s.Name, sourcePauseMaxTimes, sourcePauseFailStreak))
			return
		}
		pausesInRow++
		pausedUntil = time.Now().Add(sourcePauseDuration)
		nth, until := pausesInRow, pausedUntil
		fetchMu.Unlock()
		log.Printf("[Spider] 站点 %s 连续失败 %d 页，暂停 %s（第 %d/%d 次）", s.Name, sourcePauseFailStreak, sourcePauseDuration, nth, sourcePauseMaxTimes)
		go func() {
			timer := time.NewTimer(time.Until(until))
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
				log.Printf("[Spider] 站点 %s 暂停结束，从断点继续采集（第 %d/%d 次暂停）", s.Name, nth, sourcePauseMaxTimes)
			}
		}()
	}
	// noteTransientBurst 连续临时失败时把整站短暂停一次。这次已经停着，只把恢复时间往后推。
	noteTransientBurst := func() {
		fetchMu.Lock()
		if fetchOK == 0 {
			fetchMu.Unlock()
			return
		}
		transientStreak++
		if transientStreak < transientPauseAfter {
			fetchMu.Unlock()
			return
		}
		transientStreak = 0
		now := time.Now()
		until := now.Add(transientRetryPause)
		if now.Before(pausedUntil) {
			if pausedUntil.Before(until) {
				pausedUntil = until
			}
			fetchMu.Unlock()
			return
		}
		if transientPauses >= maxTransientPauses {
			fetchMu.Unlock()
			return
		}
		transientPauses++
		pausedUntil = until
		nth := transientPauses
		fetchMu.Unlock()
		utils.CloseIdleHTTPConnections()
		log.Printf("[Spider] 站点 %s 连续 %d 页临时失败，暂停 %s 后重试这些页（第 %d/%d 次）",
			s.Name, transientPauseAfter, transientRetryPause, nth, maxTransientPauses)
	}
	noteFetchFail := func(page int, err error) {
		fetchMu.Lock()
		fetchFails++
		ok, fails := fetchOK, fetchFails
		fetchMu.Unlock()
		if !shouldStopUnprovenSource(ok, fails) {
			notePauseStreak()
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
	// retryTransient 把 EOF、断连和单页超时放回队尾。还没有成功页的站不补，避免死站放大重试。
	retryTransient := func(job pageFetch, err error) bool {
		if !isTransientFetchErr(err) || job.transient >= transientPageRetries {
			return false
		}
		fetchMu.Lock()
		proven := fetchOK > 0
		inPause := time.Now().Before(pausedUntil)
		budgetLeft := transientPauses < maxTransientPauses
		fetchMu.Unlock()
		if !proven || (!budgetLeft && !inPause) {
			return false
		}
		noteTransientBurst()
		job.transient++
		return pages.requeue(ctx, job)
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
				if !waitPause() {
					markStopped()
					return
				}
				select {
				case <-ctx.Done():
					markStopped()
					return
				case job, ok := <-pages.ch:
					if !ok {
						return
					}
					if ctx.Err() != nil {
						markStopped()
						return
					}
					pg := job.pg
					progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
						if progress.IsTerminalStatus(cur.Status) || cur.Status == progress.StatusStopping {
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
							markStopped()
							return
						}
						if retryTransient(job, err) {
							continue
						}
						recordPageFailure(pg, "fetch", err)
						noteFetchFail(pg, err)
						pages.done()
						continue
					}
					noteFetchOK()
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
		progress.UpdateRun(ctx, s.Id, func(cur *model.CollectProgress) {
			if progress.IsTerminalStatus(cur.Status) || cur.Status == progress.StatusStopping {
				return
			}
			cur.Status = progress.StatusFailed
			cur.QueueId = ""
		})
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
