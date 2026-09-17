package scheduler

import (
	"context"
	"log"
	"sort"
	"time"
)

type jobMeta struct {
	sourceName    string
	sourcePending int
	globalPending int
	tail          bool
}

func (l *writeLane) tryTakeOne() (Job, jobMeta, func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	queue := l.selectQueueRoundRobinLocked()
	if queue == nil {
		return Job{}, jobMeta{}, nil, false
	}
	job, meta, finish := l.takeOneLocked(queue)
	return job, meta, finish, true
}

func (l *writeLane) maybeLogDepth() {
	now := time.Now().UnixNano()
	last := l.lastDepthLog.Load()
	if last != 0 && now-last < int64(5*time.Second) {
		return
	}
	if !l.lastDepthLog.CompareAndSwap(last, now) {
		return
	}
	snap := l.snapshot()
	waits := l.backpressureWaits.Load()
	var avgWait time.Duration
	if waits > 0 {
		avgWait = time.Duration(l.backpressureWaitNs.Load() / waits)
	}
	log.Printf("[Spider][WriteScheduler] %s 水位 global_pending=%d/%d sources=%d writing=%d bp_waits=%d bp_avg_wait=%s",
		l.name, snap.PendingTotal, snap.MaxPendingGlobal, snap.PendingSources, snap.WritingSources, waits, avgWait.Round(time.Millisecond))
}

func (l *writeLane) selectQueueRoundRobinLocked() *writeQueue {
	n := len(l.order)
	if n == 0 {
		return nil
	}
	if l.rrCursor >= n {
		l.rrCursor = 0
	}
	for i := 0; i < n; i++ {
		idx := (l.rrCursor + i) % n
		queue := l.queues[l.order[idx]]
		if queue == nil {
			continue
		}
		if queue.isReady() {
			l.rrCursor = (idx + 1) % n
			return queue
		}
	}
	return nil
}

func (l *writeLane) takeOneLocked(queue *writeQueue) (Job, jobMeta, func()) {
	job := queue.pending[0]
	queue.pending = queue.pending[1:]
	if l.totalPending > 0 {
		l.totalPending--
	}
	queue.writing = true
	sourceID := queue.sourceID
	meta := jobMeta{
		sourceName:    queue.sourceName,
		sourcePending: len(queue.pending),
		globalPending: l.totalPending,
		tail:          queue.done && len(queue.pending) == 0,
	}
	l.cond.Broadcast()
	return job, meta, func() {
		l.finishWriting(sourceID)
	}
}

func (l *writeLane) finishWriting(sourceID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	queue, ok := l.queues[sourceID]
	if !ok {
		l.cond.Broadcast()
		return
	}
	queue.writing = false
	if queue.done && len(queue.pending) == 0 {
		l.removeQueueLocked(sourceID)
	}
	l.cond.Broadcast()
}

func (q *writeQueue) isReady() bool {
	return !q.writing && len(q.pending) > 0
}

func (l *writeLane) snapshot() writeSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()

	writing := 0
	for _, q := range l.queues {
		if q.writing {
			writing++
		}
	}
	return writeSnapshot{
		PendingTotal:     l.totalPending,
		PendingSources:   len(l.queues),
		WritingSources:   writing,
		MaxPendingSource: l.maxPendingPerSource(),
		MaxPendingGlobal: l.maxPendingGlobal(),
		PagesPerSec:      int(l.limiter.Limit()),
		MaxInflight:      l.workers,
	}
}

func shouldLogWrite(page int) bool {
	if page <= 0 {
		return true
	}
	if page <= 5 {
		return true
	}
	return page%50 == 0
}

func (l *writeLane) pendingSourceIDs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	ids := make([]string, 0, len(l.order))
	ids = append(ids, l.order...)
	sort.Strings(ids)
	return ids
}

func (l *writeLane) nextJob() (Job, jobMeta, func()) {
	for {
		l.waitUntilWork()
		job, meta, finish, ok := l.tryTakeOne()
		if ok {
			return job, meta, finish
		}
	}
}

func (l *writeLane) isIdleLocked() bool {
	if l.totalPending > 0 {
		return false
	}
	for _, q := range l.queues {
		if q.writing || len(q.pending) > 0 {
			return false
		}
	}
	return true
}

func (l *writeLane) waitPending(ctx context.Context) error {
	stopCancelWake := context.AfterFunc(ctx, func() {
		l.mu.Lock()
		l.cond.Broadcast()
		l.mu.Unlock()
	})
	defer stopCancelWake()

	l.mu.Lock()
	defer l.mu.Unlock()

	for {
		if l.isIdleLocked() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		l.cond.Wait()
	}
}

func (l *writeLane) cancelAll() {
	l.mu.Lock()
	var allDiscarded []Job
	for id, queue := range l.queues {
		discarded := queue.pending
		queue.pending = nil
		l.totalPending -= len(discarded)
		queue.done = true
		if !queue.writing {
			l.removeQueueLocked(id)
		}
		allDiscarded = append(allDiscarded, discarded...)
	}
	if l.totalPending < 0 {
		l.totalPending = 0
	}
	l.cond.Broadcast()
	l.mu.Unlock()

	for _, job := range allDiscarded {
		if job.Complete != nil {
			job.Complete(Completion{
				Page:  job.Page,
				Err:   context.Canceled,
				Stage: "canceled",
			})
		}
	}
}
