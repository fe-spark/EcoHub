package spider

import (
	"context"
	"log"
	"sync"

	"server/internal/model"
)

const (
	collectWriteMaxPendingPages = 200
	collectWriteLaneWorkers     = 2
)

var collectWrites = newCollectWriteScheduler()

type collectWriteCompletion struct {
	page  int
	mids  []int64
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

type collectWriteLane struct {
	name   string
	mu     sync.Mutex
	cond   *sync.Cond
	queues map[string]*collectWriteQueue
}

type collectWriteQueue struct {
	sourceID   string
	sourceName string
	pending    []collectWriteJob
	done       bool
	writing    bool
	readyLog   bool
}

func newCollectWriteLane(name string) *collectWriteLane {
	lane := &collectWriteLane{
		name:   name,
		queues: make(map[string]*collectWriteQueue),
	}
	lane.cond = sync.NewCond(&lane.mu)
	return lane
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

	l.mu.Lock()
	defer l.mu.Unlock()

	queue := l.queueFor(job)
	for len(queue.pending) >= collectWriteMaxPendingPages {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.cond.Wait()
	}
	if err := ctx.Err(); err != nil {
		return err
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
	if len(queue.pending) == 0 && !queue.writing {
		delete(l.queues, sourceID)
		l.cond.Broadcast()
		return
	}
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

func (l *collectWriteLane) start() {
	workerCount := collectWriteLaneWorkers
	if workerCount <= 0 {
		workerCount = 1
	}
	for workerID := 1; workerID <= workerCount; workerID++ {
		go l.run(workerID)
	}
}

func (l *collectWriteLane) run(workerID int) {
	for {
		jobs, meta, finish := l.nextJobs()
		log.Printf("[Spider][WriteScheduler] %s lane worker=%d 开始写入 source=%s pending=%d tail=%t", l.name, workerID, meta.sourceName, len(jobs), meta.tail)
		failed := 0
		for _, job := range jobs {
			mids, err := job.write()
			if err != nil {
				failed++
			}
			job.complete(collectWriteCompletion{page: job.page, mids: mids, err: err, stage: "save"})
		}
		log.Printf("[Spider][WriteScheduler] %s lane worker=%d 完成写入 source=%s pending=%d failed=%d tail=%t", l.name, workerID, meta.sourceName, len(jobs), failed, meta.tail)
		finish()
	}
}

type collectWriteBatchMeta struct {
	sourceName string
	tail       bool
}

// nextJobs blocks until exactly one ready queue is available and returns its
// pending jobs. Each worker takes a single source so multiple workers can write
// different sources concurrently — same-source serialization is preserved by
// the per-queue writing flag.
func (l *collectWriteLane) nextJobs() ([]collectWriteJob, collectWriteBatchMeta, func()) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for {
		queue := l.selectQueueLocked()
		if queue != nil {
			return l.takeQueueLocked(queue)
		}
		l.cond.Wait()
	}
}

func (l *collectWriteLane) takeQueueLocked(queue *collectWriteQueue) ([]collectWriteJob, collectWriteBatchMeta, func()) {
	jobs := queue.pending
	queue.pending = nil
	queue.writing = true
	queue.readyLog = false
	sourceID := queue.sourceID
	meta := collectWriteBatchMeta{
		sourceName: queue.sourceName,
		tail:       queue.done,
	}
	l.cond.Broadcast()
	return jobs, meta, func() {
		l.finishWriting(sourceID)
	}
}

func (l *collectWriteLane) finishWriting(sourceID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	queue, ok := l.queues[sourceID]
	if !ok {
		l.cond.Broadcast()
		return
	}
	queue.writing = false
	if queue.done && len(queue.pending) == 0 {
		delete(l.queues, sourceID)
	} else {
		l.markReadyLocked(queue)
	}
	l.cond.Broadcast()
}

func (l *collectWriteLane) selectQueueLocked() *collectWriteQueue {
	for _, queue := range l.queues {
		if queue.isReady() {
			return queue
		}
	}
	return nil
}

func (l *collectWriteLane) markReadyLocked(queue *collectWriteQueue) {
	if !queue.isReady() {
		return
	}
	if queue.readyLog {
		return
	}
	queue.readyLog = true
	log.Printf("[Spider][WriteScheduler] %s lane 站点 %s 进入写入队列 pending=%d tail=%t", l.name, queue.sourceName, len(queue.pending), queue.done)
}

func (q *collectWriteQueue) isReady() bool {
	if q.writing || len(q.pending) == 0 {
		return false
	}
	return true
}
