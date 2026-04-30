package spider

import (
	"context"
	"sync"
	"time"

	"server/internal/model"
)

const (
	collectWriteMaxPendingPagesPerSource = 200
	collectWriteMaxPagesPerTurn          = 20
)

var collectWrites = newCollectWriteScheduler()

type collectWriteCompletion struct {
	page  int
	pids  []int64
	err   error
	stage string
}

type collectWriteJob struct {
	sourceID   string
	sourceName string
	grade      model.SourceGrade
	page       int
	write      func() ([]int64, error)
	complete   func(collectWriteCompletion)
}

type collectWriteScheduler struct {
	master *collectWriteLane
	slave  *collectWriteLane
}

func newCollectWriteScheduler() *collectWriteScheduler {
	s := &collectWriteScheduler{
		master: newCollectWriteLane(),
		slave:  newCollectWriteLane(),
	}
	go s.master.run()
	go s.slave.run()
	return s
}

func (s *collectWriteScheduler) submit(ctx context.Context, job collectWriteJob) error {
	if job.grade == model.SlaveCollect {
		return s.slave.submit(ctx, job)
	}
	return s.master.submit(ctx, job)
}

func (s *collectWriteScheduler) finishSource(grade model.SourceGrade, sourceID string) {
	if grade == model.SlaveCollect {
		s.slave.finishSource(sourceID)
		return
	}
	s.master.finishSource(sourceID)
}

type collectWriteLane struct {
	mu     sync.Mutex
	cond   *sync.Cond
	queues map[string]*collectWriteQueue
	seq    uint64
}

type collectWriteQueue struct {
	sourceID   string
	sourceName string
	pending    []collectWriteJob
	done       bool
	readySeq   uint64
}

func newCollectWriteLane() *collectWriteLane {
	lane := &collectWriteLane{queues: make(map[string]*collectWriteQueue)}
	lane.cond = sync.NewCond(&lane.mu)
	return lane
}

func (l *collectWriteLane) submit(ctx context.Context, job collectWriteJob) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	queue := l.queueFor(job)
	for len(queue.pending) >= collectWriteMaxPendingPagesPerSource {
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			l.mu.Lock()
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		l.mu.Lock()
		queue = l.queueFor(job)
	}

	queue.pending = append(queue.pending, job)
	l.markReadyLocked(queue)
	l.cond.Signal()
	return nil
}

func (l *collectWriteLane) finishSource(sourceID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	queue, ok := l.queues[sourceID]
	if !ok {
		return
	}
	queue.done = true
	l.markReadyLocked(queue)
	l.cond.Signal()
}

func (l *collectWriteLane) queueFor(job collectWriteJob) *collectWriteQueue {
	queue, ok := l.queues[job.sourceID]
	if ok {
		return queue
	}
	queue = &collectWriteQueue{sourceID: job.sourceID, sourceName: job.sourceName}
	l.queues[job.sourceID] = queue
	return queue
}

func (l *collectWriteLane) run() {
	for {
		batch := l.nextBatch()
		for _, job := range batch {
			pids, err := job.write()
			job.complete(collectWriteCompletion{page: job.page, pids: pids, err: err, stage: "save"})
		}
	}
}

func (l *collectWriteLane) nextBatch() []collectWriteJob {
	l.mu.Lock()
	defer l.mu.Unlock()

	for {
		selected := l.selectQueueLocked()
		if selected != nil {
			batchSize := min(len(selected.pending), collectWriteMaxPagesPerTurn)
			batch := append([]collectWriteJob(nil), selected.pending[:batchSize]...)
			selected.pending = selected.pending[batchSize:]
			selected.readySeq = 0
			if len(selected.pending) == 0 {
				delete(l.queues, selected.sourceID)
			} else {
				l.markReadyLocked(selected)
			}
			l.cond.Broadcast()
			return batch
		}
		l.cond.Wait()
	}
}

func (l *collectWriteLane) selectQueueLocked() *collectWriteQueue {
	var selected *collectWriteQueue
	for _, queue := range l.queues {
		if queue.readySeq == 0 || len(queue.pending) == 0 {
			continue
		}
		if selected == nil || queue.readySeq < selected.readySeq {
			selected = queue
		}
	}
	return selected
}

func (l *collectWriteLane) markReadyLocked(queue *collectWriteQueue) {
	if queue.readySeq > 0 || len(queue.pending) == 0 {
		return
	}
	if len(queue.pending) < collectWriteMaxPagesPerTurn && !queue.done {
		return
	}
	l.seq++
	queue.readySeq = l.seq
}
