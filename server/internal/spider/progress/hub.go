package progress

import (
	"context"
	"sync"
	"time"

	"server/internal/model"
)

// CategorySyncEvent 采集站分类同步状态事件
type CategorySyncEvent struct {
	SourceID string `json:"sourceId"`
	Status   string `json:"status"` // "syncing" | "ready" | "failed"
}

// Hub 采集进度全局广播中心
type Hub struct {
	mu                  sync.RWMutex
	subscribers         map[chan []model.CollectProgress]struct{}
	categorySubscribers map[chan CategorySyncEvent]struct{}
	notifyCh            chan struct{}
}

// GlobalHub 全局单例
var GlobalHub = &Hub{
	subscribers:         make(map[chan []model.CollectProgress]struct{}),
	categorySubscribers: make(map[chan CategorySyncEvent]struct{}),
	notifyCh:            make(chan struct{}, 1),
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

var (
	syncingSourcesMu sync.RWMutex
	syncingSources   = make(map[string]bool)
)

// SetCategorySyncing 设置采集站分类采集运行态
func SetCategorySyncing(sourceID string, syncing bool) {
	syncingSourcesMu.Lock()
	if syncing {
		syncingSources[sourceID] = true
	} else {
		delete(syncingSources, sourceID)
	}
	syncingSourcesMu.Unlock()
}

// IsCategorySyncing 判断采集站是否正在采集分类
func IsCategorySyncing(sourceID string) bool {
	syncingSourcesMu.RLock()
	defer syncingSourcesMu.RUnlock()
	return syncingSources[sourceID]
}

// BroadcastCategoryStatus 广播采集站分类同步状态变更
func BroadcastCategoryStatus(sourceID string, status string) {
	if status == "syncing" {
		SetCategorySyncing(sourceID, true)
	} else {
		SetCategorySyncing(sourceID, false)
	}

	GlobalHub.mu.RLock()
	defer GlobalHub.mu.RUnlock()
	evt := CategorySyncEvent{SourceID: sourceID, Status: status}
	for ch := range GlobalHub.categorySubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// BroadcastCategoryReady 兼容旧调用：广播就绪或失败
func BroadcastCategoryReady(sourceID string, ready bool) {
	if ready {
		BroadcastCategoryStatus(sourceID, "ready")
	} else {
		BroadcastCategoryStatus(sourceID, "failed")
	}
}

// SubscribeCategory 订阅采集站分类状态事件流
func SubscribeCategory(ctx context.Context) <-chan CategorySyncEvent {
	ch := make(chan CategorySyncEvent, 16)

	GlobalHub.mu.Lock()
	GlobalHub.categorySubscribers[ch] = struct{}{}
	GlobalHub.mu.Unlock()

	go func() {
		<-ctx.Done()
		GlobalHub.mu.Lock()
		delete(GlobalHub.categorySubscribers, ch)
		GlobalHub.mu.Unlock()
		close(ch)
	}()

	return ch
}
