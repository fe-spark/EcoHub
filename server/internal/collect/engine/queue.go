package engine

import (
	"context"
	"sync"
)

// pageFetchQueue 惰性分页队列。支持从指定起始页推进游标，避免预分配超大页码切片与通道。
type pageFetchQueue struct {
	mu       sync.Mutex
	nextPage int
	endPage  int
	targets  []int
	idx      int
	requeue  []int
	wake     chan struct{}
	drained  chan struct{}
	closed   bool
	left     int
}

func newPageFetchQueue(startPage, pageCount int, targetPages []int) *pageFetchQueue {
	queue := &pageFetchQueue{
		wake:    make(chan struct{}, 1),
		drained: make(chan struct{}),
	}
	if len(targetPages) > 0 {
		queue.targets = append([]int(nil), targetPages...)
		queue.left = len(queue.targets)
		return queue
	}
	if startPage < 1 {
		startPage = 1
	}
	if pageCount < startPage {
		queue.left = 0
		queue.closeLocked()
		return queue
	}
	queue.nextPage = startPage
	queue.endPage = pageCount
	queue.left = pageCount - startPage + 1
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
	close(q.drained)
}

func (q *pageFetchQueue) next(ctx context.Context) (int, bool) {
	for {
		q.mu.Lock()
		if q.closed {
			q.mu.Unlock()
			return 0, false
		}
		if n := len(q.requeue); n > 0 {
			pg := q.requeue[n-1]
			q.requeue = q.requeue[:n-1]
			q.mu.Unlock()
			return pg, true
		}
		if q.targets != nil {
			if q.idx < len(q.targets) {
				pg := q.targets[q.idx]
				q.idx++
				q.mu.Unlock()
				return pg, true
			}
		} else if q.nextPage <= q.endPage {
			pg := q.nextPage
			q.nextPage++
			q.mu.Unlock()
			return pg, true
		}
		q.mu.Unlock()

		select {
		case <-ctx.Done():
			return 0, false
		case <-q.drained:
			return 0, false
		case <-q.wake:
		}
	}
}

// putBack 将限流暂停取消的页放回队列，该页尚未完成因此 left 不递减。
func (q *pageFetchQueue) putBack(page int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.requeue = append(q.requeue, page)
	select {
	case q.wake <- struct{}{}:
	default:
	}
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
