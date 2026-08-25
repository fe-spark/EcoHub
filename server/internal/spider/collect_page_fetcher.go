package spider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/utils"
)

const (
	pageCountRetryTimes   = 3
	filmDetailRetryTimes  = 3
	rateLimitRetryTimes   = 6
	collectDBWriteRetries = 3
	recoverMaxRetryCount  = 5
)

const (
	collectSourceConsecutiveFailureLimit = 10
	collectProgressLogPageStep           = 100
	collectFailureLogStep                = 10
)

func getPageCountWithRetry(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) (int, error) {
	var lastErr error
	maxAttempts := pageCountRetryTimes
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		release, err := waitSourceRequestTurn(ctx, s, fmt.Sprintf("页数请求 attempt=%d ", attempt))
		if err != nil {
			return 0, err
		}
		pageCount, err := spiderCore.GetPageCount(r)
		if err == nil {
			release(nil)
			return pageCount, nil
		}
		release(err)
		lastErr = err
		if utils.IsRateLimitedErr(lastErr) && maxAttempts < rateLimitRetryTimes {
			maxAttempts = rateLimitRetryTimes
		}
		if attempt < maxAttempts {
			if waitErr := waitRetryBackoff(ctx, attempt); waitErr != nil {
				return 0, waitErr
			}
		}
	}
	return 0, lastErr
}

func getFilmDetailWithRetry(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
	var lastErr error
	page := r.Params.Get("pg")
	if page == "" {
		page = "-"
	}
	maxAttempts := filmDetailRetryTimes
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		release, err := waitSourceRequestTurn(ctx, s, fmt.Sprintf("分页请求 pg=%s attempt=%d ", page, attempt))
		if err != nil {
			return nil, err
		}
		list, err := spiderCore.GetFilmDetail(r)
		if err == nil && len(list) > 0 {
			release(nil)
			return list, nil
		}
		release(err)
		if err != nil {
			lastErr = err
		} else {
			lastErr = errors.New("response list is empty")
		}
		if utils.IsRateLimitedErr(lastErr) && maxAttempts < rateLimitRetryTimes {
			maxAttempts = rateLimitRetryTimes
		}
		if attempt < maxAttempts {
			if waitErr := waitRetryBackoff(ctx, attempt); waitErr != nil {
				return nil, waitErr
			}
		}
	}
	return nil, lastErr
}

func waitRetryBackoff(ctx context.Context, attempt int) error {
	if attempt <= 0 {
		attempt = 1
	}
	base := time.Duration(1<<uint(attempt-1)) * time.Second
	if base > 10*time.Second {
		base = 10 * time.Second
	}
	jitter := time.Duration(rand.Intn(500)) * time.Millisecond
	delay := base + jitter

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func getSourcePageConcurrency(_ *model.FilmSource) int {
	base := config.CollectPageWorkers
	if base <= 0 {
		base = config.DefaultCollectPageWorkers
	}
	solo := config.CollectPageWorkersSolo
	if solo <= 0 {
		solo = config.DefaultCollectPageWorkersSolo
	}
	if solo < base {
		solo = base
	}
	if countLiveCollectTasks() <= 1 {
		if solo <= 0 {
			return 1
		}
		return solo
	}
	if base <= 0 {
		return 1
	}
	return base
}

type collectPageStats struct {
	latestPage int
	success    int
	failed     int
}

func shouldLogCollectProgress(done, total int) bool {
	return done == total || done%collectProgressLogPageStep == 0
}

func shouldLogCollectFailure(failed int) bool {
	return failed == 1 || failed%collectFailureLogStep == 0
}

func buildPageRequest(s *model.FilmSource, h, pg int) utils.RequestInfo {
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("pg", fmt.Sprint(pg))
	if h > 0 {
		r.Params.Set("h", fmt.Sprint(h))
	}
	return r
}

func collectFilmPages(parentCtx context.Context, pageCount int, requestWorkerLimit int, s *model.FilmSource, h int, flushAtEnd bool, batch *notify.ChangeBatch) (bool, error) {
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
	writeCompletions := make(chan collectWriteCompletion, pageCount)
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
	stats := collectPageStats{}
	lastLoggedDone := 0
	logProgress := func(force bool) {
		statsMu.Lock()
		snapshot := stats
		done := snapshot.success + snapshot.failed
		if !force {
			if done == lastLoggedDone || !shouldLogCollectProgress(done, pageCount) {
				statsMu.Unlock()
				return
			}
		}
		lastLoggedDone = done
		statsMu.Unlock()

		log.Printf("[Spider] 站点 %s 采集进度 完成=%d/%d，成功=%d，失败=%d，最新页=%d", s.Name, done, pageCount, snapshot.success, snapshot.failed, snapshot.latestPage)
	}
	recordPageFinished := func(page int, success bool) collectPageStats {
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

		if currentFailures < collectSourceConsecutiveFailureLimit {
			return
		}
		stopOnce.Do(func() {
			stopErr = fmt.Errorf("站点 %s 连续采集失败 %d 次，已终止本次采集", s.Name, collectSourceConsecutiveFailureLimit)
			syslog.Errorf("[Spider] 站点 %s 连续失败达到阈值，终止采集 page=%d stage=%s err=%v", s.Name, page, stage, err)
			updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
				progress.Status = progressStatusFailed
			})
			if flushAtEnd || !notify.IsEventEnabled(model.NotifyEventCollectBatchSummary) {
				emitSourceFailedNotify(s.Id, s.Name, stopErr.Error())
			} else {
				noteSourceError(s.Id, stopErr.Error())
			}
			cancel()
		})
	}
	recordSuccess := func() {
		consecutiveFailuresMu.Lock()
		consecutiveFailures = 0
		consecutiveFailuresMu.Unlock()
	}
	markStopped := func() {
		markProgressStopped(s.Id)
	}
	maybeMarkPagePhaseDone := func(progress *model.CollectProgress, snapshot collectPageStats) {
		if progress.Status != progressStatusRunning && progress.Status != progressStatusStarting {
			return
		}
		if progress.Total > 0 && snapshot.success+snapshot.failed >= progress.Total {
			progress.Status = progressStatusPageDone
		}
	}
	recordPageFailure := func(page int, stage string, err error) {
		snapshot := recordPageFinished(page, false)
		updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
			progress.Failed = snapshot.failed
			if page > progress.Current {
				progress.Current = page
			}
			maybeMarkPagePhaseDone(progress, snapshot)
		})
		saveFilmPageFailure(s, h, page, stage, err)
		if shouldLogCollectFailure(snapshot.failed) {
			syslog.Warnf("[Spider] 站点 %s 采集失败累计=%d，最近失败 page=%d stage=%s err=%v", s.Name, snapshot.failed, page, stage, err)
		}
		recordFailure(page, stage, err)
		logProgress(false)
	}
	recordPageSuccess := func(page int, notifyMIDs, affectedMIDs []int64) {
		snapshot := recordPageFinished(page, true)
		recordSuccess()
		noteCollectedMIDs(batch, s.Id, s.Name, notifyMIDs)
		if s.Grade == model.MasterCollect && h < 0 {
			collectLifecycle.addPendingMasterMIDs(s.Id, affectedMIDs)
		} else if s.Grade == model.MasterCollect {
			collectLifecycle.addMasterAffectedMIDs(affectedMIDs)
		} else {
			collectLifecycle.addAffectedMIDs(affectedMIDs)
		}
		updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
			progress.Success = snapshot.success
			progress.Failed = snapshot.failed
			if page > progress.Current {
				progress.Current = page
			}
			maybeMarkPagePhaseDone(progress, snapshot)
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
					updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
						if pg > progress.Current {
							progress.Current = pg
						}
						progress.Status = progressStatusRunning
					})
					list, err := getFilmDetailWithRetry(ctx, s, buildPageRequest(s, h, pg))
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
					submitErr := collectWrites.submit(ctx, collectWriteJob{
						sourceID:   s.Id,
						sourceName: s.Name,
						grade:      s.Grade,
						page:       page,
						write: func() (collectWriteMids, error) {
							return saveCollectedFilmForCollect(context.Background(), s, page, items)
						},
						complete: func(completion collectWriteCompletion) {
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
		collectWrites.finishSource(s.Grade, s.Id)
		writeWG.Wait()
		close(writeCompletions)
	}()

	for completion := range writeCompletions {
		if completion.err != nil {
			recordPageFailure(completion.page, completion.stage, completion.err)
			continue
		}
		recordPageSuccess(completion.page, completion.notifyMIDs, completion.affectedMIDs)
	}
	logProgress(true)
	if ctx.Err() != nil {
		log.Printf("[Spider] 站点 %s 并发采集任务已中断，worker 已全部退出\n", s.Name)
	}
	if stopErr != nil {
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
