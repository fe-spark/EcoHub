package engine

import (
	"context"
	"log"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/utils"
)

const (
	// defaultThrottleWindow 单站初始并发窗口。无固定请求间隔，仅在 429 时通过二级 AIMD 收敛。
	defaultThrottleWindow = 8
	// aimdIncreaseStep 连续成功达到该次数后优先缩短动态间隔，归零后再逐步恢复并发窗口。
	aimdIncreaseStep = 10
	// rateLimitCooldown 上游未返回 Retry-After 时的默认 429 短冷却窗口。
	rateLimitCooldown = 1 * time.Second
	// minDynamicInterval 并发已降至 1 后再次命中 429 时启动的二级动态请求间隔。
	minDynamicInterval = 250 * time.Millisecond
	// maxDynamicInterval 二级动态请求间隔上限，避免退避无限放大。
	maxDynamicInterval = 5 * time.Second
	// intervalRecoverFloor 动态间隔逐步减半至低于该阈值时直接归零。
	intervalRecoverFloor = 100 * time.Millisecond
)

type concurrencyGate struct {
	mu       sync.Mutex
	cond     *sync.Cond
	inflight int
	limit    func() int
}

func newConcurrencyGate(limit func() int) *concurrencyGate {
	g := &concurrencyGate{limit: limit}
	g.cond = sync.NewCond(&g.mu)
	return g
}

func (g *concurrencyGate) acquire(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() {
		g.mu.Lock()
		g.cond.Broadcast()
		g.mu.Unlock()
	})
	defer stop()

	g.mu.Lock()
	defer g.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		limit := g.limit()
		if limit < 1 {
			limit = 1
		}
		if g.inflight < limit {
			g.inflight++
			return nil
		}
		timer := time.AfterFunc(20*time.Millisecond, func() {
			g.mu.Lock()
			g.cond.Broadcast()
			g.mu.Unlock()
		})
		g.cond.Wait()
		timer.Stop()
	}
}

func (g *concurrencyGate) release() {
	g.mu.Lock()
	if g.inflight > 0 {
		g.inflight--
	}
	g.cond.Broadcast()
	g.mu.Unlock()
}

type sourceWindow struct {
	mu              sync.Mutex
	cond            *sync.Cond
	sourceID        string
	inFlight        int
	limit           int
	epoch           uint64
	successStreak   int
	cooldownUntil   time.Time
	dynamicInterval time.Duration
	nextLaunchAt    time.Time
	nowFn           func() time.Time
	maxWindowFn     func() int
}

func newSourceWindow(sourceID string, maxWindowFn func() int, nowFn func() time.Time) *sourceWindow {
	initWin := defaultThrottleWindow
	if maxWin := maxWindowFn(); maxWin > 0 && initWin > maxWin {
		initWin = maxWin
	}
	if initWin < 1 {
		initWin = 1
	}
	w := &sourceWindow{
		sourceID:    sourceID,
		limit:       initWin,
		nowFn:       nowFn,
		maxWindowFn: maxWindowFn,
	}
	w.cond = sync.NewCond(&w.mu)
	return w
}

func (w *sourceWindow) acquire(ctx context.Context) (uint64, error) {
	stop := context.AfterFunc(ctx, func() {
		w.mu.Lock()
		w.cond.Broadcast()
		w.mu.Unlock()
	})
	defer stop()

	w.mu.Lock()
	defer w.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		now := w.nowFn()
		waitDur := w.cooldownUntil.Sub(now)
		if w.dynamicInterval > 0 {
			if paceWait := w.nextLaunchAt.Sub(now); paceWait > waitDur {
				waitDur = paceWait
			}
		}
		if waitDur > 0 {
			timer := time.AfterFunc(waitDur, func() {
				w.mu.Lock()
				w.cond.Broadcast()
				w.mu.Unlock()
			})
			w.cond.Wait()
			timer.Stop()
			continue
		}
		capLimit := w.currentLimitLocked()
		if w.inFlight < capLimit {
			w.inFlight++
			if w.dynamicInterval > 0 {
				w.nextLaunchAt = now.Add(w.dynamicInterval)
			}
			return w.epoch, nil
		}
		w.cond.Wait()
	}
}

func (w *sourceWindow) currentLimitLocked() int {
	maxWin := w.maxWindowFn()
	if maxWin < 1 {
		maxWin = 1
	}
	if w.limit > maxWin {
		return maxWin
	}
	if w.limit < 1 {
		return 1
	}
	return w.limit
}

func (w *sourceWindow) release(reqEpoch uint64, err error) {
	w.mu.Lock()
	if w.inFlight > 0 {
		w.inFlight--
	}
	if utils.IsRateLimitedErr(err) {
		prevWin := w.limit
		prevInterval := w.dynamicInterval
		shouldStep := reqEpoch == w.epoch
		if shouldStep {
			// 同一个并发波次在降窗/拉间隔前已发出的在途请求，最多只触发一次退避，防止级联打穿。
			w.epoch++
			w.successStreak = 0
			if w.limit > 1 {
				next := w.limit / 2
				if next < 1 {
					next = 1
				}
				w.limit = next
			} else {
				// 并发已降到 1 仍触发 429：进入二级退避，逐步拉大动态请求间隔。
				if w.dynamicInterval <= 0 {
					w.dynamicInterval = minDynamicInterval
				} else {
					w.dynamicInterval *= 2
					if w.dynamicInterval > maxDynamicInterval {
						w.dynamicInterval = maxDynamicInterval
					}
				}
			}
		}
		cooldown := utils.RateLimitRetryAfter(err)
		if cooldown <= 0 {
			cooldown = rateLimitCooldown
		}
		if w.dynamicInterval > cooldown {
			cooldown = w.dynamicInterval
		}
		nextCool := w.nowFn().Add(cooldown)
		if nextCool.After(w.cooldownUntil) {
			w.cooldownUntil = nextCool
		}
		if shouldStep {
			log.Printf("[Spider] 站点 %s 命中 429 限流，并发窗口 %d -> %d，动态间隔 %s -> %s，冷却 %s",
				w.sourceID, prevWin, w.limit, prevInterval, w.dynamicInterval, cooldown)
		}
	} else if err == nil {
		w.successStreak++
		if w.successStreak >= aimdIncreaseStep {
			w.successStreak = 0
			if w.dynamicInterval > 0 {
				next := w.dynamicInterval / 2
				if next < intervalRecoverFloor {
					w.dynamicInterval = 0
					w.nextLaunchAt = time.Time{}
				} else {
					w.dynamicInterval = next
				}
			} else {
				maxWin := w.maxWindowFn()
				if maxWin < 1 {
					maxWin = 1
				}
				if w.limit < maxWin {
					w.limit++
				}
			}
		}
	}
	w.cond.Broadcast()
	w.mu.Unlock()
}

func (w *sourceWindow) cancelRelease() {
	w.mu.Lock()
	if w.inFlight > 0 {
		w.inFlight--
	}
	w.cond.Broadcast()
	w.mu.Unlock()
}

// ThrottleStore 管理全局抓取并发与各站点独立的 AIMD 自适应窗口。
type ThrottleStore struct {
	global      *concurrencyGate
	mu          sync.Mutex
	windows     map[string]*sourceWindow
	maxWindowFn func() int
	nowFn       func() time.Time
}

// NewThrottleStore 创建独立的节流管理器。
func NewThrottleStore(globalLimitFn, maxWindowFn func() int) *ThrottleStore {
	if globalLimitFn == nil {
		globalLimitFn = config.FetchInFlightLimit
	}
	if maxWindowFn == nil {
		maxWindowFn = config.PageWorkerLimit
	}
	return &ThrottleStore{
		global:      newConcurrencyGate(globalLimitFn),
		windows:     make(map[string]*sourceWindow),
		maxWindowFn: maxWindowFn,
		nowFn:       time.Now,
	}
}

var defaultThrottle = NewThrottleStore(nil, nil)

// DefaultThrottle 返回全局默认节流器。
func DefaultThrottle() *ThrottleStore {
	return defaultThrottle
}

// ResetSourceThrottle 清除指定站点的运行期节流窗口状态。
func ResetSourceThrottle(sourceID string) {
	defaultThrottle.Reset(sourceID)
}

// ResetAllThrottles 清除全部站点的运行期节流窗口状态。
func ResetAllThrottles() {
	defaultThrottle.ResetAll()
}

func (ts *ThrottleStore) window(sourceID string) *sourceWindow {
	if sourceID == "" {
		sourceID = "_default"
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	w := ts.windows[sourceID]
	if w == nil {
		w = newSourceWindow(sourceID, ts.maxWindowFn, ts.nowFn)
		ts.windows[sourceID] = w
	}
	return w
}

// Reset 重置单站并发窗口。
func (ts *ThrottleStore) Reset(sourceID string) {
	if sourceID == "" {
		return
	}
	ts.mu.Lock()
	delete(ts.windows, sourceID)
	ts.mu.Unlock()
}

// ResetAll 清空所有站点的并发窗口状态。
func (ts *ThrottleStore) ResetAll() {
	ts.mu.Lock()
	clear(ts.windows)
	ts.mu.Unlock()
}

// WindowLimit 返回单站当前并发窗口上限（供观测与单测使用）。
func (ts *ThrottleStore) WindowLimit(sourceID string) int {
	w := ts.window(sourceID)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentLimitLocked()
}

// DynamicInterval 返回单站当前二级自适应请求间隔（供观测与单测使用）。
func (ts *ThrottleStore) DynamicInterval(sourceID string) time.Duration {
	w := ts.window(sourceID)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dynamicInterval
}

// Acquire 先占用单站 AIMD 窗口，再占用全局抓取通道，返回结果回调以驱动窗口自调。
func (ts *ThrottleStore) Acquire(ctx context.Context, sourceID string) (func(error), error) {
	w := ts.window(sourceID)
	reqEpoch, err := w.acquire(ctx)
	if err != nil {
		return nil, err
	}
	if err := ts.global.acquire(ctx); err != nil {
		w.cancelRelease()
		return nil, err
	}
	var once sync.Once
	return func(err error) {
		once.Do(func() {
			ts.global.release()
			w.release(reqEpoch, err)
		})
	}, nil
}
