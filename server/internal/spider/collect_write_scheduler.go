package spider

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/config"
	"server/internal/model"

	"golang.org/x/time/rate"
)

// 写库调度采用「水龙头」模型：
//  1. 有活再取 token（避免空转吞 token）
//  2. take 单页 + 跨站 round-robin
//  3. 全局限速在 writing 占用之外完成，写槽只服务真实 SQL
//  4. 同站 writing 互斥；worker panic 必释放 writing
//  5. 单站 + 全局有界 buffer，满则反压拉页
//
// 外部 API：submit / finishSource / snapshot。

var collectWrites = newCollectWriteScheduler()

type collectWriteCompletion struct {
	page         int
	notifyMIDs   []int64 // 更新列表
	affectedMIDs []int64 // 快照/缓存收尾
	err          error
	stage        string
}

type collectWriteJob struct {
	sourceID   string
	sourceName string
	grade      model.SourceGrade
	page       int
	write      func() (collectWriteMids, error)
	complete   func(collectWriteCompletion)
}

// collectWriteSnapshot 写库缓冲水位快照，供日志与后续进度 API 使用。
type collectWriteSnapshot struct {
	PendingTotal     int `json:"pendingTotal"`
	PendingSources   int `json:"pendingSources"`
	WritingSources   int `json:"writingSources"`
	MaxPendingSource int `json:"maxPendingSource"`
	MaxPendingGlobal int `json:"maxPendingGlobal"`
	PagesPerSec      int `json:"pagesPerSec"`
	MaxInflight      int `json:"maxInflight"`
}

type collectWriteScheduler struct {
	lane *collectWriteLane
}

func newCollectWriteScheduler() *collectWriteScheduler {
	s := &collectWriteScheduler{lane: newCollectWriteLane("采集")}
	s.lane.start()
	return s
}

func (s *collectWriteScheduler) submit(ctx context.Context, job collectWriteJob) error {
	return s.lane.submit(ctx, job)
}

func (s *collectWriteScheduler) finishSource(_ model.SourceGrade, sourceID string) {
	s.lane.finishSource(sourceID)
}

func (s *collectWriteScheduler) cancelSource(_ model.SourceGrade, sourceID string) {
	s.lane.cancelSource(sourceID)
}

func (s *collectWriteScheduler) snapshot() collectWriteSnapshot {
	return s.lane.snapshot()
}

type collectWriteLane struct {
	name     string
	mu       sync.Mutex
	cond     *sync.Cond
	queues   map[string]*collectWriteQueue
	order    []string // sourceID 稳定顺序，用于 round-robin
	rrCursor int      // 下一次 RR 起始下标
	limiter  *rate.Limiter
	workers  int

	// 有界水箱（构造时从 config 拷贝，测试可覆盖）。
	maxPerSource int
	maxGlobal    int

	// totalPending 所有站 pending 页数之和，O(1) 做全局水位判断。
	totalPending int

	// 反压观测
	backpressureWaits        atomic.Int64
	backpressureWaitNs       atomic.Int64
	lastBackpressureLog      atomic.Int64 // unix nano，进入反压限频
	lastBackpressureLeaveLog atomic.Int64 // unix nano，离开反压限频
	lastDepthLog             atomic.Int64
}

type collectWriteQueue struct {
	sourceID   string
	sourceName string
	pending    []collectWriteJob
	done       bool
	writing    bool
}

type backpressureReason string

const (
	bpNone       backpressureReason = ""
	bpSourceFull backpressureReason = "source_full"
	bpGlobalFull backpressureReason = "global_full"
)

func newCollectWriteLane(name string) *collectWriteLane {
	pagesPerSec := float64(config.CollectWritePagesPerSec)
	if pagesPerSec <= 0 {
		pagesPerSec = float64(config.DefaultCollectWritePagesPerSec)
	}
	burst := config.CollectWriteBurstPages
	if burst <= 0 {
		burst = config.DefaultCollectWriteBurstPages
	}
	workers := config.CollectWriteMaxInflight
	if workers <= 0 {
		workers = config.DefaultCollectWriteMaxInflight
	}

	maxPerSource := config.CollectWriteMaxPendingPagesPerSource
	if maxPerSource <= 0 {
		maxPerSource = config.DefaultCollectWriteMaxPendingPagesPerSource
	}
	maxGlobal := config.CollectWriteMaxPendingPagesGlobal
	if maxGlobal <= 0 {
		maxGlobal = config.DefaultCollectWriteMaxPendingPagesGlobal
	}

	lane := &collectWriteLane{
		name:         name,
		queues:       make(map[string]*collectWriteQueue),
		order:        make([]string, 0, 16),
		limiter:      rate.NewLimiter(rate.Limit(pagesPerSec), burst),
		workers:      workers,
		maxPerSource: maxPerSource,
		maxGlobal:    maxGlobal,
	}
	lane.cond = sync.NewCond(&lane.mu)
	return lane
}

func (l *collectWriteLane) maxPendingPerSource() int {
	if l.maxPerSource <= 0 {
		return config.DefaultCollectWriteMaxPendingPagesPerSource
	}
	return l.maxPerSource
}

func (l *collectWriteLane) maxPendingGlobal() int {
	if l.maxGlobal <= 0 {
		return config.DefaultCollectWriteMaxPendingPagesGlobal
	}
	return l.maxGlobal
}

func (l *collectWriteLane) submitBlockedReason(queue *collectWriteQueue) backpressureReason {
	if len(queue.pending) >= l.maxPendingPerSource() {
		return bpSourceFull
	}
	if l.totalPending >= l.maxPendingGlobal() {
		return bpGlobalFull
	}
	return bpNone
}

func (l *collectWriteLane) submit(ctx context.Context, job collectWriteJob) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	stopCancelWake := context.AfterFunc(ctx, func() {
		l.mu.Lock()
		l.cond.Broadcast()
		l.mu.Unlock()
	})
	defer stopCancelWake()

	var (
		waitStart     time.Time
		waited        bool
		lastReason    backpressureReason
		logEnter      bool
		enterSource   string
		enterPending  int
		enterGlobal   int
		logLeave      bool
		leaveCanceled bool
		leaveSource   string
		leavePending  int
		leaveGlobal   int
		leaveReason   backpressureReason
		leaveWait     time.Duration
	)

	l.mu.Lock()
	queue := l.queueFor(job)
	for {
		reason := l.submitBlockedReason(queue)
		if reason == bpNone {
			break
		}
		lastReason = reason
		if err := ctx.Err(); err != nil {
			if waited {
				leaveWait = time.Since(waitStart)
				leaveCanceled = true
				leaveReason = lastReason
				leaveSource = queue.sourceName
				leavePending = len(queue.pending)
				leaveGlobal = l.totalPending
				logLeave = true // always log cancellations
				l.backpressureWaits.Add(1)
				l.backpressureWaitNs.Add(leaveWait.Nanoseconds())
			}
			l.mu.Unlock()
			if logLeave {
				l.logBackpressureLeave(leaveCanceled, leaveReason, leaveSource, leaveWait, leavePending, leaveGlobal)
			}
			return err
		}
		if !waited {
			waitStart = time.Now()
			waited = true
			enterSource = queue.sourceName
			enterPending = len(queue.pending)
			enterGlobal = l.totalPending
			logEnter = l.shouldLogBackpressureEnter()
		}
		l.cond.Wait()
		if q, ok := l.queues[job.sourceID]; ok {
			queue = q
		} else {
			queue = l.queueFor(job)
		}
	}
	if err := ctx.Err(); err != nil {
		if waited {
			leaveWait = time.Since(waitStart)
			leaveCanceled = true
			leaveReason = lastReason
			leaveSource = queue.sourceName
			leavePending = len(queue.pending)
			leaveGlobal = l.totalPending
			logLeave = true
			l.backpressureWaits.Add(1)
			l.backpressureWaitNs.Add(leaveWait.Nanoseconds())
		}
		l.mu.Unlock()
		if logLeave {
			l.logBackpressureLeave(leaveCanceled, leaveReason, leaveSource, leaveWait, leavePending, leaveGlobal)
		}
		return err
	}

	queue.pending = append(queue.pending, job)
	l.totalPending++
	if waited {
		leaveWait = time.Since(waitStart)
		leaveReason = lastReason
		leaveSource = queue.sourceName
		leavePending = len(queue.pending)
		leaveGlobal = l.totalPending
		logLeave = leaveWait >= 2*time.Second && l.shouldLogBackpressureLeave()
		l.backpressureWaits.Add(1)
		l.backpressureWaitNs.Add(leaveWait.Nanoseconds())
	}
	l.cond.Signal()
	l.mu.Unlock()

	if logEnter {
		log.Printf("[Spider][WriteScheduler] %s 反压等待 reason=%s source=%s source_pending=%d global_pending=%d/%d source_limit=%d",
			l.name, lastReason, enterSource, enterPending, enterGlobal, l.maxPendingGlobal(), l.maxPendingPerSource())
	}
	if logLeave {
		l.logBackpressureLeave(leaveCanceled, leaveReason, leaveSource, leaveWait, leavePending, leaveGlobal)
	}
	return nil
}

func (l *collectWriteLane) shouldLogBackpressureEnter() bool {
	now := time.Now().UnixNano()
	last := l.lastBackpressureLog.Load()
	if last != 0 && now-last < int64(2*time.Second) {
		return false
	}
	return l.lastBackpressureLog.CompareAndSwap(last, now)
}

func (l *collectWriteLane) shouldLogBackpressureLeave() bool {
	now := time.Now().UnixNano()
	last := l.lastBackpressureLeaveLog.Load()
	if last != 0 && now-last < int64(3*time.Second) {
		return false
	}
	return l.lastBackpressureLeaveLog.CompareAndSwap(last, now)
}

func (l *collectWriteLane) logBackpressureLeave(canceled bool, reason backpressureReason, sourceName string, wait time.Duration, sourcePending, globalPending int) {
	status := "resumed"
	if canceled {
		status = "canceled"
	}
	log.Printf("[Spider][WriteScheduler] %s 反压结束 status=%s reason=%s source=%s wait=%s source_pending=%d global_pending=%d/%d",
		l.name, status, reason, sourceName, wait.Round(time.Millisecond), sourcePending, globalPending, l.maxPendingGlobal())
}

func (l *collectWriteLane) finishSource(sourceID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	queue, ok := l.queues[sourceID]
	if !ok {
		return
	}
	queue.done = true
	if len(queue.pending) == 0 && !queue.writing {
		l.removeQueueLocked(sourceID)
		l.cond.Broadcast()
		return
	}
	l.cond.Signal()
}

func (l *collectWriteLane) cancelSource(sourceID string) {
	l.mu.Lock()
	queue, ok := l.queues[sourceID]
	if !ok {
		l.mu.Unlock()
		return
	}
	discarded := queue.pending
	queue.pending = nil
	l.totalPending -= len(discarded)
	if l.totalPending < 0 {
		l.totalPending = 0
	}
	queue.done = true
	if !queue.writing {
		l.removeQueueLocked(sourceID)
	}
	l.cond.Broadcast()
	l.mu.Unlock()

	for _, job := range discarded {
		if job.complete != nil {
			job.complete(collectWriteCompletion{
				page:  job.page,
				err:   context.Canceled,
				stage: "canceled",
			})
		}
	}
}

func (l *collectWriteLane) queueFor(job collectWriteJob) *collectWriteQueue {
	queue, ok := l.queues[job.sourceID]
	if ok {
		if job.sourceName != "" {
			queue.sourceName = job.sourceName
		}
		return queue
	}
	queue = &collectWriteQueue{sourceID: job.sourceID, sourceName: job.sourceName}
	l.queues[job.sourceID] = queue
	l.order = append(l.order, job.sourceID)
	return queue
}

func (l *collectWriteLane) removeQueueLocked(sourceID string) {
	delete(l.queues, sourceID)
	for i, id := range l.order {
		if id == sourceID {
			l.order = append(l.order[:i], l.order[i+1:]...)
			if len(l.order) == 0 {
				l.rrCursor = 0
			} else {
				l.rrCursor %= len(l.order)
			}
			break
		}
	}
}

func (l *collectWriteLane) start() {
	for workerID := 1; workerID <= l.workers; workerID++ {
		go l.run(workerID)
	}
	log.Printf("[Spider][WriteScheduler] %s lane 已启动 workers=%d pages_per_sec=%.0f burst=%d pending_per_source=%d pending_global=%d",
		l.name, l.workers, float64(l.limiter.Limit()), l.limiter.Burst(), l.maxPendingPerSource(), l.maxPendingGlobal())
}

func (l *collectWriteLane) run(workerID int) {
	for {
		// 1) 等到有可写任务（不占 writing、不吞 token）
		l.waitUntilWork()

		// 2) 预留限速 token（尚未 take）。若 take 失败必须 Cancel 退还，避免空烧页/秒额度。
		res := l.limiter.Reserve()
		if !res.OK() {
			// 极限配置（rate≈0）下兜底，避免忙等。
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if delay := res.Delay(); delay > 0 {
			time.Sleep(delay)
		}

		// 3) 取页；可能被其它 worker 抢先
		job, meta, finish, ok := l.tryTakeOne()
		if !ok {
			// 关键：退还本次 reservation，保证实际写库速率贴近 pages_per_sec。
			res.Cancel()
			continue
		}

		func() {
			defer finish()

			start := time.Now()
			var mids collectWriteMids
			var err error
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("[Spider][WriteScheduler] %s lane worker=%d panic source=%s page=%d: %v",
							l.name, workerID, job.sourceName, job.page, rec)
						err = fmt.Errorf("write panic: %v", rec)
					}
				}()
				mids, err = job.write()
			}()
			job.complete(collectWriteCompletion{
				page:         job.page,
				notifyMIDs:   mids.Notify,
				affectedMIDs: mids.Affected,
				err:          err,
				stage:        "save",
			})

			if shouldLogCollectWrite(job.page) || err != nil || meta.tail {
				status := "ok"
				if err != nil {
					status = "fail"
				}
				log.Printf("[Spider][WriteScheduler] %s lane worker=%d source=%s page=%d status=%s source_pending=%d global_pending=%d tail=%t cost=%s",
					l.name, workerID, meta.sourceName, job.page, status, meta.sourcePending, meta.globalPending, meta.tail, time.Since(start))
			}
		}()
		l.maybeLogDepth()
	}
}

func (l *collectWriteLane) waitUntilWork() {
	l.mu.Lock()
	defer l.mu.Unlock()
	// 仅探测是否有可写队列，不推进 RR cursor（避免空转吞轮转公平性）。
	for !l.hasReadyWorkLocked() {
		l.cond.Wait()
	}
}

func (l *collectWriteLane) hasReadyWorkLocked() bool {
	for _, id := range l.order {
		queue := l.queues[id]
		if queue != nil && queue.isReady() {
			return true
		}
	}
	return false
}
