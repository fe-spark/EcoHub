package fetcher

import "sync"

// pageFetchQueue 待抓页。全部落地或采集被取消后关闭。
type pageFetchQueue struct {
	mu      sync.Mutex
	ch      chan int
	drained chan struct{}
	closed  bool
	left    int
}

func newPageFetchQueue(pages []int) *pageFetchQueue {
	queue := &pageFetchQueue{
		ch:      make(chan int, len(pages)),
		drained: make(chan struct{}),
		left:    len(pages),
	}
	for _, pg := range pages {
		queue.ch <- pg
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
	close(q.drained)
}

// putBack 把被限流取消的页放回队列。这一页还没完成，left 不变。
func (q *pageFetchQueue) putBack(page int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.ch <- page
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
