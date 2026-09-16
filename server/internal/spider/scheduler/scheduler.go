package scheduler

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
// 对外只暴露 Submit / FinishSource / CancelSource / WaitPendingWrites 门面；
// 真实落盘逻辑由调用方通过 Job.Write 注入，本包不依赖采集编排层。

var writes = newWriteScheduler()

// 包级门面：调用方只依赖下列入口，无需感知 lane、worker 与反压实现。

// Submit 提交单页写入任务；队列满时会反压等待，ctx 取消则立即返回。
func Submit(ctx context.Context, job Job) error {
	return writes.submit(ctx, job)
}

// FinishSource 标记站点已无新页提交，队列排空后自动回收。
func FinishSource(grade model.SourceGrade, sourceID string) {
	writes.finishSource(grade, sourceID)
}

// CancelSource 丢弃站点仍在排队的写入任务（在途写入不受影响）。
func CancelSource(grade model.SourceGrade, sourceID string) {
	writes.cancelSource(grade, sourceID)
}

// Completion 单页写入的完成回调载荷。
type Completion struct {
	Page         int
	NotifyMIDs   []int64 // 更新列表
	AffectedMIDs []int64 // 快照/缓存收尾
	Err          error
	Stage        string
}

// Job 单页写入任务；实际落盘逻辑由调用方通过 Write 注入。
type Job struct {
	SourceID   string
	SourceName string
	Grade      model.SourceGrade
	Page       int
	Write      func() (Mids, error)
	Complete   func(Completion)
}

// Mids 一页写入产生的 mid 集合：Notify 用于变更通知，Affected 用于快照/缓存收尾。
type Mids struct {
	Notify   []int64
	Affected []int64
}

// writeSnapshot 写库缓冲水位快照，供日志与后续进度 API 使用。
type writeSnapshot struct {
	PendingTotal     int `json:"pendingTotal"`
	PendingSources   int `json:"pendingSources"`
	WritingSources   int `json:"writingSources"`
	MaxPendingSource int `json:"maxPendingSource"`
	MaxPendingGlobal int `json:"maxPendingGlobal"`
	PagesPerSec      int `json:"pagesPerSec"`
	MaxInflight      int `json:"maxInflight"`
}

type writeScheduler struct {
	lane *writeLane
}

func newWriteScheduler() *writeScheduler {
	s := &writeScheduler{lane: newWriteLane("采集")}
	s.lane.start()
	return s
}

func (s *writeScheduler) submit(ctx context.Context, job Job) error {
	return s.lane.submit(ctx, job)
}

func (s *writeScheduler) finishSource(_ model.SourceGrade, sourceID string) {
	s.lane.finishSource(sourceID)
}

func (s *writeScheduler) cancelSource(_ model.SourceGrade, sourceID string) {
	s.lane.cancelSource(sourceID)
}

func (s *writeScheduler) snapshot() writeSnapshot {
	return s.lane.snapshot()
}

func (s *writeScheduler) waitPending(ctx context.Context) error {
	return s.lane.waitPending(ctx)
}

// WaitPendingWrites 等待在途采集写调度队列全部落盘（供优雅停机排空在途写入）
func WaitPendingWrites(ctx context.Context) error {
	if writes == nil || writes.lane == nil {
		return nil
	}
	return writes.waitPending(ctx)
}

func (s *writeScheduler) cancelAll() {
	s.lane.cancelAll()
}

type writeLane struct {
	name     string
	mu       sync.Mutex
	cond     *sync.Cond
	queues   map[string]*writeQueue
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

type writeQueue struct {
	sourceID   string
	sourceName string
	pending    []Job
	done       bool
	writing    bool
}

func newWriteLane(name string) *writeLane {
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

	lane := &writeLane{
		name:         name,
		queues:       make(map[string]*writeQueue),
		order:        make([]string, 0, 16),
		limiter:      rate.NewLimiter(rate.Limit(pagesPerSec), burst),
		workers:      workers,
		maxPerSource: maxPerSource,
		maxGlobal:    maxGlobal,
	}
	lane.cond = sync.NewCond(&lane.mu)
	return lane
}

func (l *writeLane) maxPendingPerSource() int {
	if l.maxPerSource <= 0 {
		return config.DefaultCollectWriteMaxPendingPagesPerSource
	}
	return l.maxPerSource
}

func (l *writeLane) maxPendingGlobal() int {
	if l.maxGlobal <= 0 {
		return config.DefaultCollectWriteMaxPendingPagesGlobal
	}
	return l.maxGlobal
}

func (l *writeLane) submit(ctx context.Context, job Job) error {
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
		if q, ok := l.queues[job.SourceID]; ok {
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

func (l *writeLane) finishSource(sourceID string) {
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

func (l *writeLane) cancelSource(sourceID string) {
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
		if job.Complete != nil {
			job.Complete(Completion{
				Page:  job.Page,
				Err:   context.Canceled,
				Stage: "canceled",
			})
		}
	}
}

func (l *writeLane) queueFor(job Job) *writeQueue {
	queue, ok := l.queues[job.SourceID]
	if ok {
		if job.SourceName != "" {
			queue.sourceName = job.SourceName
		}
		return queue
	}
	queue = &writeQueue{sourceID: job.SourceID, sourceName: job.SourceName}
	l.queues[job.SourceID] = queue
	l.order = append(l.order, job.SourceID)
	return queue
}

func (l *writeLane) removeQueueLocked(sourceID string) {
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

func (l *writeLane) start() {
	for workerID := 1; workerID <= l.workers; workerID++ {
		go l.run(workerID)
	}
	log.Printf("[Spider][WriteScheduler] %s lane 已启动 workers=%d pages_per_sec=%.0f burst=%d pending_per_source=%d pending_global=%d",
		l.name, l.workers, float64(l.limiter.Limit()), l.limiter.Burst(), l.maxPendingPerSource(), l.maxPendingGlobal())
}

func (l *writeLane) run(workerID int) {
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
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("[Spider][WriteScheduler] %s lane worker=%d complete panic source=%s page=%d: %v",
						l.name, workerID, job.SourceName, job.Page, rec)
				}
			}()

			start := time.Now()
			var mids Mids
			var err error
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("[Spider][WriteScheduler] %s lane worker=%d panic source=%s page=%d: %v",
							l.name, workerID, job.SourceName, job.Page, rec)
						err = fmt.Errorf("write panic: %v", rec)
					}
				}()
				mids, err = job.Write()
			}()
			if job.Complete != nil {
				job.Complete(Completion{
					Page:         job.Page,
					NotifyMIDs:   mids.Notify,
					AffectedMIDs: mids.Affected,
					Err:          err,
					Stage:        "save",
				})
			}

			if shouldLogWrite(job.Page) || err != nil || meta.tail {
				status := "ok"
				if err != nil {
					status = "fail"
				}
				log.Printf("[Spider][WriteScheduler] %s lane worker=%d source=%s page=%d status=%s source_pending=%d global_pending=%d tail=%t cost=%s",
					l.name, workerID, meta.sourceName, job.Page, status, meta.sourcePending, meta.globalPending, meta.tail, time.Since(start))
			}
		}()
		l.maybeLogDepth()
	}
}

func (l *writeLane) waitUntilWork() {
	l.mu.Lock()
	defer l.mu.Unlock()
	// 仅探测是否有可写队列，不推进 RR cursor（避免空转吞轮转公平性）。
	for !l.hasReadyWorkLocked() {
		l.cond.Wait()
	}
}

func (l *writeLane) hasReadyWorkLocked() bool {
	for _, id := range l.order {
		queue := l.queues[id]
		if queue != nil && queue.isReady() {
			return true
		}
	}
	return false
}
