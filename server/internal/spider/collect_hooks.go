package spider

import (
	"log"
	"sync"
)

var (
	collectDoneHooksMu sync.RWMutex
	collectDoneHooks   []func()
)

// RegisterCollectDoneHook 注册采集批次全部完成后的全局回调
func RegisterCollectDoneHook(hook func()) {
	if hook == nil {
		return
	}
	collectDoneHooksMu.Lock()
	defer collectDoneHooksMu.Unlock()
	collectDoneHooks = append(collectDoneHooks, hook)
}

func triggerCollectDoneHooks() {
	collectDoneHooksMu.RLock()
	hooks := make([]func(), len(collectDoneHooks))
	copy(hooks, collectDoneHooks)
	collectDoneHooksMu.RUnlock()

	for _, fn := range hooks {
		if fn != nil {
			go func(f func()) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[Spider] collectDoneHook panic: %v", r)
					}
				}()
				f()
			}(fn)
		}
	}
}
