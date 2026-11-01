package progress

import (
	"context"
	"sync"
	"time"

	"server/internal/model"
)

// CategorySyncEvent 采集站分类同步状态事件
type CategorySyncEvent struct {
	SourceID    string                     `json:"sourceId"`
	Status      string                     `json:"status"` // "syncing" | "ready" | "failed" | "confirm" | "idle"
	Name        string                     `json:"name,omitempty"`
	Changes     []model.CategoryTreeChange `json:"changes,omitempty"`
	Message     string                     `json:"message,omitempty"`
	DataCleared bool                       `json:"dataCleared,omitempty"`
	SitePageTotal int                        `json:"sitePageTotal,omitempty"`
}

// SourceDeleteEvent 采集站异步删除事件
type SourceDeleteEvent struct {
	SourceID     string `json:"sourceId"`
	SourceName   string `json:"sourceName,omitempty"`
	Status       string `json:"status"` // "deleting" | "deleted" | "failed"
	Message      string `json:"message,omitempty"`
	DeletedFilms int    `json:"deletedFilms,omitempty"`
}

// Hub 采集进度全局广播中心
type Hub struct {
	mu                  sync.RWMutex
	subscribers            map[chan []model.CollectProgress]struct{}
	categorySubscribers    map[chan CategorySyncEvent]struct{}
	deleteSubscribers      map[chan SourceDeleteEvent]struct{}
	searchIndexSubscribers map[chan SearchIndexProgressEvent]struct{}
	notifyCh               chan struct{}
}

// GlobalHub 全局单例
var GlobalHub = &Hub{
	subscribers:            make(map[chan []model.CollectProgress]struct{}),
	categorySubscribers:    make(map[chan CategorySyncEvent]struct{}),
	deleteSubscribers:      make(map[chan SourceDeleteEvent]struct{}),
	searchIndexSubscribers: make(map[chan SearchIndexProgressEvent]struct{}),
	notifyCh:               make(chan struct{}, 1),
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

// BroadcastCategoryReady 广播就绪或失败
func BroadcastCategoryReady(sourceID string, ready bool) {
	if ready {
		BroadcastCategoryStatus(sourceID, "ready")
	} else {
		BroadcastCategoryStatus(sourceID, "failed")
	}
}

// BroadcastCategoryConfirm 广播分类树变更确认事件
func BroadcastCategoryConfirm(sourceID string, name string, changes []model.CategoryTreeChange) {
	SetCategorySyncing(sourceID, false)

	GlobalHub.mu.RLock()
	defer GlobalHub.mu.RUnlock()
	evt := CategorySyncEvent{
		SourceID: sourceID,
		Status:   "confirm",
		Name:     name,
		Changes:  changes,
	}
	for ch := range GlobalHub.categorySubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// BroadcastCategoryReadyMessage 广播分类就绪事件及执行结果文案与最新采集站总页
func BroadcastCategoryReadyMessage(sourceID string, message string, dataCleared bool, sitePageTotal int) {
	SetCategorySyncing(sourceID, false)

	GlobalHub.mu.RLock()
	defer GlobalHub.mu.RUnlock()
	evt := CategorySyncEvent{
		SourceID:      sourceID,
		Status:        "ready",
		Message:       message,
		DataCleared:   dataCleared,
		SitePageTotal: sitePageTotal,
	}
	for ch := range GlobalHub.categorySubscribers {
		select {
		case ch <- evt:
		default:
		}
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

var (
	deletingSourcesMu sync.RWMutex
	deletingSources   = make(map[string]bool)
)

// SetSourceDeleting 设置采集站异步删除运行态
func SetSourceDeleting(sourceID string, deleting bool) {
	deletingSourcesMu.Lock()
	if deleting {
		deletingSources[sourceID] = true
	} else {
		delete(deletingSources, sourceID)
	}
	deletingSourcesMu.Unlock()
}

// IsSourceDeleting 判断采集站是否正在执行异步删除
func IsSourceDeleting(sourceID string) bool {
	deletingSourcesMu.RLock()
	defer deletingSourcesMu.RUnlock()
	return deletingSources[sourceID]
}

// GetDeletingSourceIDs 返回当前处于异步删除中的所有站点 ID 列表
func GetDeletingSourceIDs() []string {
	deletingSourcesMu.RLock()
	defer deletingSourcesMu.RUnlock()
	ids := make([]string, 0, len(deletingSources))
	for id := range deletingSources {
		ids = append(ids, id)
	}
	return ids
}

// BroadcastSourceDeleteStatus 广播采集站删除状态变更
func BroadcastSourceDeleteStatus(sourceID, sourceName, status, message string, deletedFilms int) {
	if status == "deleting" {
		SetSourceDeleting(sourceID, true)
	} else {
		SetSourceDeleting(sourceID, false)
	}

	GlobalHub.mu.RLock()
	defer GlobalHub.mu.RUnlock()
	evt := SourceDeleteEvent{
		SourceID:     sourceID,
		SourceName:   sourceName,
		Status:       status,
		Message:      message,
		DeletedFilms: deletedFilms,
	}
	for ch := range GlobalHub.deleteSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// SubscribeSourceDelete 订阅采集站删除事件流
func SubscribeSourceDelete(ctx context.Context) <-chan SourceDeleteEvent {
	ch := make(chan SourceDeleteEvent, 16)

	GlobalHub.mu.Lock()
	GlobalHub.deleteSubscribers[ch] = struct{}{}
	GlobalHub.mu.Unlock()

	go func() {
		<-ctx.Done()
		GlobalHub.mu.Lock()
		delete(GlobalHub.deleteSubscribers, ch)
		GlobalHub.mu.Unlock()
		close(ch)
	}()

	return ch
}
// SearchIndexProgressEvent 检索索引重建数字进度事件
type SearchIndexProgressEvent struct {
	Total   int64  `json:"total"`
	Current int64  `json:"current"`
	Percent int    `json:"percent"`
	Status  string `json:"status"` // "indexing" | "done" | "idle"
}

// BroadcastSearchIndexProgress 广播检索索引重建数字进度
func BroadcastSearchIndexProgress(evt SearchIndexProgressEvent) {
	GlobalHub.mu.RLock()
	defer GlobalHub.mu.RUnlock()
	for ch := range GlobalHub.searchIndexSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// SubscribeSearchIndex 订阅检索索引重建进度事件流
func SubscribeSearchIndex(ctx context.Context) <-chan SearchIndexProgressEvent {
	ch := make(chan SearchIndexProgressEvent, 8)
	GlobalHub.mu.Lock()
	GlobalHub.searchIndexSubscribers[ch] = struct{}{}
	GlobalHub.mu.Unlock()

	go func() {
		<-ctx.Done()
		GlobalHub.mu.Lock()
		delete(GlobalHub.searchIndexSubscribers, ch)
		GlobalHub.mu.Unlock()
		close(ch)
	}()

	return ch
}
