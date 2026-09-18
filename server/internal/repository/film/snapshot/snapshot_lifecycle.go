package snapshot

import (
	"log"
	"strings"
	"sync"
)

type SnapshotPublishedHook func(version string)

var (
	snapshotHooksMu sync.RWMutex
	snapshotHooks   []SnapshotPublishedHook
)

// RegisterSnapshotPublishedHook 注册快照发布完成后的监听回调（如异步预热缓存等）
func RegisterSnapshotPublishedHook(fn SnapshotPublishedHook) {
	if fn == nil {
		return
	}
	snapshotHooksMu.Lock()
	defer snapshotHooksMu.Unlock()
	snapshotHooks = append(snapshotHooks, fn)
}

// NotifySnapshotPublished 当新版本快照发布成功后广播通知所有注册的钩子
func NotifySnapshotPublished(version string) {
	version = strings.TrimSpace(version)
	if version == "" {
		return
	}
	snapshotHooksMu.RLock()
	hooks := make([]SnapshotPublishedHook, len(snapshotHooks))
	copy(hooks, snapshotHooks)
	snapshotHooksMu.RUnlock()

	for _, hook := range hooks {
		if hook != nil {
			go func(h SnapshotPublishedHook) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[Snapshot][Lifecycle] 执行快照发布钩子发生异常 version=%s: %v", version, r)
					}
				}()
				h(version)
			}(hook)
		}
	}
}
