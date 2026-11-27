package fetcher

import (
	"context"
	"sync"
)

// MaxCollectPageCount 采集站允许的合理最大分页数，防止恶意超大 pagecount 触发 OOM 与无限抓取。
const MaxCollectPageCount = 50000

// pageFetchQueue 惰性分页队列。普通全量抓取使用游标推进，避免一次性预分配超大页码切片与通道。
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

func newPageFetchQueue(pageCount int, targetPages []int) *pageFetchQueue {
	queue := &pageFetchQueue{
		wake:    make(chan struct{}, 1),
		drained: make(chan struct{}),
	}
	if len(targetPages) > 0 {
		queue.targets = append([]int(nil), targetPages...)
		queue.left = len(queue.targets)
	} else {
		queue.nextPage = 1
		queue.endPage = pageCount
		queue.left = pageCount
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

// putBack 把被限流取消的页放回队列。这一页还没完成，left 不变。
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
