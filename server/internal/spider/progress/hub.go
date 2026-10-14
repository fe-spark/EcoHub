package progress

import (
	"context"
	"sync"
	"time"

	"server/internal/model"
)

// Hub 采集进度全局广播中心
type Hub struct {
	mu          sync.RWMutex
	subscribers map[chan []model.CollectProgress]struct{}
	notifyCh    chan struct{}
}

// GlobalHub 全局单例
var GlobalHub = &Hub{
	subscribers: make(map[chan []model.CollectProgress]struct{}),
	notifyCh:    make(chan struct{}, 1),
}

func init() {
	go GlobalHub.run()
}

func (h *Hub) run() {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	var hasPending bool

	for {
		select {
		case <-h.notifyCh:
			hasPending = true
		case <-ticker.C:
			if hasPending {
				hasPending = false
				h.broadcast()
			}
		}
	}
}

func (h *Hub) broadcast() {
	h.mu.RLock()
	if len(h.subscribers) == 0 {
		h.mu.RUnlock()
		return
	}
	h.mu.RUnlock()

	data := GetActiveTaskProgress()

	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.subscribers {
		select {
		case ch <- data:
		default:
		}
	}
}

// NotifyChange 触发进度更新广播（非阻塞，300ms 聚合防抖）
func NotifyChange() {
	select {
	case GlobalHub.notifyCh <- struct{}{}:
	default:
	}
}

// Subscribe 订阅采集进度事件流
func Subscribe(ctx context.Context) <-chan []model.CollectProgress {
	ch := make(chan []model.CollectProgress, 8)

	GlobalHub.mu.Lock()
	GlobalHub.subscribers[ch] = struct{}{}
	GlobalHub.mu.Unlock()

	initial := GetActiveTaskProgress()
	select {
	case ch <- initial:
	default:
	}

	go func() {
		<-ctx.Done()
		GlobalHub.mu.Lock()
		delete(GlobalHub.subscribers, ch)
		GlobalHub.mu.Unlock()
		close(ch)
	}()

	return ch
}
