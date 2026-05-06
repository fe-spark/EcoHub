package spider

import (
	"context"
	"log"
	"strings"
	"sync"

	"server/internal/model"
)

const (
	collectWriteMaxPendingPages = 200
	collectWriteLaneWorkers     = 1
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

func (s *collectWriteScheduler) beginSources(sources []model.FilmSource) {
	s.lane.beginSources(sources)
}

func (s *collectWriteScheduler) endSources(sources []model.FilmSource) {
	s.lane.endSources(sources)
}

func (s *collectWriteScheduler) finishSource(_ model.SourceGrade, sourceID string) {
	s.lane.finishSource(sourceID)
}

type collectWriteLane struct {
	name    string
	mu      sync.Mutex
	cond    *sync.Cond
	queues  map[string]*collectWriteQueue
	active  map[string]struct{}
	done    map[string]struct{}
	writing bool
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
		active: make(map[string]struct{}),
		done:   make(map[string]struct{}),
	}
	lane.cond = sync.NewCond(&lane.mu)
	return lane
}

func (l *collectWriteLane) beginSources(sources []model.FilmSource) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, source := range sources {
		if source.Id == "" {
			continue
		}
		l.active[source.Id] = struct{}{}
		delete(l.done, source.Id)
	}
	l.cond.Broadcast()
}

func (l *collectWriteLane) endSources(sources []model.FilmSource) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, source := range sources {
		if source.Id == "" {
			continue
		}
		delete(l.active, source.Id)
		delete(l.done, source.Id)
	}
	l.cond.Broadcast()
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
		l.done[sourceID] = struct{}{}
		l.cond.Broadcast()
		return
	}
	queue.done = true
	if len(queue.pending) == 0 && !queue.writing {
		delete(l.queues, sourceID)
		l.done[sourceID] = struct{}{}
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
		log.Printf("[Spider][WriteScheduler] %s lane worker=%d 开始写入 sources=%s pending=%d tail=%t", l.name, workerID, meta.sourceName, len(jobs), meta.tail)
		failed := 0
		for _, job := range jobs {
			mids, err := job.write()
			if err != nil {
				failed++
			}
			job.complete(collectWriteCompletion{page: job.page, mids: mids, err: err, stage: "save"})
		}
		log.Printf("[Spider][WriteScheduler] %s lane worker=%d 完成写入 sources=%s pending=%d failed=%d tail=%t", l.name, workerID, meta.sourceName, len(jobs), failed, meta.tail)
		finish()
	}
}

type collectWriteBatchMeta struct {
	sourceName string
	tail       bool
}

func (l *collectWriteLane) nextJobs() ([]collectWriteJob, collectWriteBatchMeta, func()) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for {
		selected := l.selectQueuesLocked()
		if len(selected) > 0 {
			jobs, meta, finish := l.takeJobsLocked(selected)
			return jobs, meta, finish
		}
		l.cond.Wait()
	}
}

func (l *collectWriteLane) takeJobsLocked(selected []*collectWriteQueue) ([]collectWriteJob, collectWriteBatchMeta, func()) {
	jobs := make([]collectWriteJob, 0)
	sourceIDs := make([]string, 0, len(selected))
	sourceNames := make([]string, 0, len(selected))
	tail := true
	for _, queue := range selected {
		jobs = append(jobs, queue.pending...)
		queue.pending = nil
		queue.writing = true
		queue.readyLog = false
		sourceIDs = append(sourceIDs, queue.sourceID)
		sourceNames = append(sourceNames, queue.sourceName)
		if !queue.done {
			tail = false
		}
	}
	l.writing = true
	meta := collectWriteBatchMeta{
		sourceName: strings.Join(sourceNames, ","),
		tail:       tail,
	}
	l.cond.Broadcast()
	return jobs, meta, func() {
		l.finishWriting(sourceIDs)
	}
}

func (l *collectWriteLane) finishWriting(sourceIDs []string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writing = false
	for _, sourceID := range sourceIDs {
		queue, ok := l.queues[sourceID]
		if !ok {
			continue
		}
		queue.writing = false
		if queue.done && len(queue.pending) == 0 {
			delete(l.queues, sourceID)
			l.done[sourceID] = struct{}{}
		} else {
			l.markReadyLocked(queue)
		}
	}
	l.cond.Broadcast()
}

func (l *collectWriteLane) selectQueuesLocked() []*collectWriteQueue {
	if l.writing {
		return nil
	}
	selected := make([]*collectWriteQueue, 0)
	for _, queue := range l.queues {
		if !queue.isReady() {
			continue
		}
		selected = append(selected, queue)
	}
	if len(l.active) > 0 && !l.activeSourcesReadyLocked() {
		return nil
	}
	return selected
}

func (l *collectWriteLane) activeSourcesReadyLocked() bool {
	for sourceID := range l.active {
		if _, ok := l.done[sourceID]; ok {
			continue
		}
		queue, ok := l.queues[sourceID]
		if !ok || !queue.isReady() {
			return false
		}
	}
	return true
}

func (l *collectWriteLane) selectQueueLocked() *collectWriteQueue {
	selected := l.selectQueuesLocked()
	if len(selected) == 0 {
		return nil
	}
	return selected[0]
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
