package access

import "sync/atomic"

var isAnalyticsEnabled atomic.Bool

// IsEnabled 纳秒级单指令读取，供中间件和埋点高频调用
func IsEnabled() bool {
	return isAnalyticsEnabled.Load()
}

// SetEnabled 热更新内存状态
func SetEnabled(val bool) {
	isAnalyticsEnabled.Store(val)
}
