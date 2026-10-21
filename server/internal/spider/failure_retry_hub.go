package spider

import (
	"context"
	"sync"
)

// FailureRetryEvent 一条失败记录的重试状态。只推给订阅了该 id 的连接。
type FailureRetryEvent struct {
	ID         uint   `json:"id"`
	Retrying   bool   `json:"retrying"`
	HasResult  bool   `json:"hasResult"`
	Status     int    `json:"status"`
	RetryCount int    `json:"retryCount"`
	Cause      string `json:"cause"`
}

type failureRetrySub struct {
	ids     map[uint]struct{}
	pending map[uint]FailureRetryEvent
	kick    chan struct{}
}

type failureRetryHub struct {
	mu       sync.Mutex
	inflight map[uint]struct{}
	subs     map[*failureRetrySub]struct{}
}

var failureRetries = newFailureRetryHub()

func newFailureRetryHub() *failureRetryHub {
	return &failureRetryHub{
		inflight: map[uint]struct{}{},
		subs:     map[*failureRetrySub]struct{}{},
	}
}

func beginFailureRetry(id uint) {
	if id == 0 {
		return
	}
	failureRetries.publish(FailureRetryEvent{ID: id, Retrying: true})
}

// TryBeginFailureRetry 把记录放进重试队列。已在队列中时返回 false。
func TryBeginFailureRetry(id uint) bool {
	if id == 0 {
		return false
	}
	failureRetries.mu.Lock()
	defer failureRetries.mu.Unlock()
	if _, ok := failureRetries.inflight[id]; ok {
		return false
	}
	failureRetries.publishLocked(FailureRetryEvent{ID: id, Retrying: true})
	return true
}

func settleFailureRetry(id uint, status, retryCount int, cause string, hasResult bool) {
	if id == 0 {
		return
	}
	failureRetries.publish(FailureRetryEvent{
		ID:         id,
		HasResult:  hasResult,
		Status:     status,
		RetryCount: retryCount,
		Cause:      cause,
	})
}

// abandonFailureRetry 记录没进入执行就离开队列，页面恢复原状态。
func abandonFailureRetry(id uint) {
	settleFailureRetry(id, 0, 0, "", false)
}

func (h *failureRetryHub) publish(ev FailureRetryEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.publishLocked(ev)
}

func (h *failureRetryHub) publishLocked(ev FailureRetryEvent) {
	if ev.Retrying {
		h.inflight[ev.ID] = struct{}{}
	} else {
		delete(h.inflight, ev.ID)
	}
	for sub := range h.subs {
		if _, ok := sub.ids[ev.ID]; !ok {
			continue
		}
		sub.pending[ev.ID] = ev
		select {
		case sub.kick <- struct{}{}:
		default:
		}
	}
}

// FailureRetrySnapshot 当前页里正在重试的记录。
func FailureRetrySnapshot(ids []uint) []FailureRetryEvent {
	failureRetries.mu.Lock()
	defer failureRetries.mu.Unlock()
	out := make([]FailureRetryEvent, 0)
	for _, id := range ids {
		if _, ok := failureRetries.inflight[id]; ok {
			out = append(out, FailureRetryEvent{ID: id, Retrying: true})
		}
	}
	return out
}

// SubscribeFailureRetry 只接收 ids 里的状态变化。
func SubscribeFailureRetry(ctx context.Context, ids []uint) <-chan []FailureRetryEvent {
	set := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			set[id] = struct{}{}
		}
	}
	sub := &failureRetrySub{
		ids:     set,
		pending: map[uint]FailureRetryEvent{},
		kick:    make(chan struct{}, 1),
	}
	failureRetries.mu.Lock()
	failureRetries.subs[sub] = struct{}{}
	failureRetries.mu.Unlock()

	out := make(chan []FailureRetryEvent, 8)
	go func() {
		defer close(out)
		defer func() {
			failureRetries.mu.Lock()
			delete(failureRetries.subs, sub)
			failureRetries.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sub.kick:
				failureRetries.mu.Lock()
				batch := make([]FailureRetryEvent, 0, len(sub.pending))
				for _, ev := range sub.pending {
					batch = append(batch, ev)
				}
				sub.pending = map[uint]FailureRetryEvent{}
				failureRetries.mu.Unlock()
				if len(batch) == 0 {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case out <- batch:
				}
			}
		}
	}()
	return out
}
