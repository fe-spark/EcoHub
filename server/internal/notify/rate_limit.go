package notify

import (
	"sync"
	"time"
)

type rateLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
}

var globalRate = &rateLimiter{last: make(map[string]time.Time)}

// allow 同 key 在 minInterval 内只放行一次；minInterval<=0 表示不限流。
func (r *rateLimiter) allow(key string, minInterval time.Duration) bool {
	if minInterval <= 0 || key == "" {
		return true
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.last[key]; ok && now.Sub(t) < minInterval {
		return false
	}
	r.last[key] = now
	// 简单清理：map 过大时整表重置，避免无限增长
	if len(r.last) > 4096 {
		r.last = map[string]time.Time{key: now}
	}
	return true
}
