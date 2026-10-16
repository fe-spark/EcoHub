package spider

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/utils"
)

const (
	// rateLimitHitWindow 同一秒内的连续 429 只记一次，避免一簇并发响应把命中打满。
	rateLimitHitWindow = time.Second
	// rateLimitRecoverAfter 上次 429 过后这段时间内，成功请求不得降低命中。
	// 全量采集里成功页远多于 429，立刻清零会让站点间隔回到全速，重试仍打在限流窗口里。
	rateLimitRecoverAfter = 5 * time.Second
	rateLimitMaxHits      = 3
	// rateLimitStreakMax 命中已顶满后，每一波新的 429 再把冷却翻倍，最多 8 倍。
	rateLimitStreakMax       = 3
	rateLimitCooldownMin     = time.Second
	rateLimitCooldownBaseMax = 5 * time.Second
	rateLimitCooldownMax     = 30 * time.Second
)

// requestGates 按站点控制外部采集接口请求间隔：同一站点两次请求之间至少等待站点 Interval。
var requestGates sync.Map

type sourceRequestGate struct {
	mu               sync.Mutex
	nextAllowedAt    time.Time
	rateLimitHits    int
	rateLimitStreak  int
	lastRateLimitHit time.Time
}

// noteRateLimitLocked 记录一次 429。计数已清空时即使落在合并窗口内也至少记 1 次，
// 避免「成功清零但 lastRateLimitHit 仍在 1 秒内」导致后续 429 的 hits 停在 0。
func (g *sourceRequestGate) noteRateLimitLocked(now time.Time) (int, int) {
	fresh := g.rateLimitHits <= 0 || g.lastRateLimitHit.IsZero() || now.Sub(g.lastRateLimitHit) > rateLimitHitWindow
	if fresh {
		if g.rateLimitHits < rateLimitMaxHits {
			g.rateLimitHits++
		} else if g.rateLimitStreak < rateLimitStreakMax {
			// 命中已是 3 仍收到新一波 429。只加冷却，不再把同一秒里的并发响应当成多次。
			g.rateLimitStreak++
		}
	}
	// 每次 429 都刷新时间戳。命中按秒合并，但恢复窗口必须跟着最近一次限流走。
	g.lastRateLimitHit = now
	return rateLimitHitsForBackoff(g.rateLimitHits), g.rateLimitStreak
}

// noteSuccessLocked 限流恢复窗口内不降命中。窗口结束后每次成功递减 1，降到 0 时清掉时间戳。
func (g *sourceRequestGate) noteSuccessLocked(now time.Time) {
	if g.rateLimitHits <= 0 {
		return
	}
	if !g.lastRateLimitHit.IsZero() && now.Sub(g.lastRateLimitHit) < rateLimitRecoverAfter {
		return
	}
	g.rateLimitHits--
	g.rateLimitStreak = 0
	if g.rateLimitHits == 0 {
		g.lastRateLimitHit = time.Time{}
	}
}

func rateLimitHitsForBackoff(hits int) int {
	if hits < 1 {
		return 1
	}
	if hits > rateLimitMaxHits {
		return rateLimitMaxHits
	}
	return hits
}

func rateLimitCooldown(hits, streak int, interval time.Duration) time.Duration {
	hits = rateLimitHitsForBackoff(hits)
	cooldown := interval * time.Duration(uint(1)<<uint(hits-1))
	if cooldown < rateLimitCooldownMin {
		cooldown = rateLimitCooldownMin
	}
	if cooldown > rateLimitCooldownBaseMax {
		cooldown = rateLimitCooldownBaseMax
	}
	if streak > 0 {
		if streak > rateLimitStreakMax {
			streak = rateLimitStreakMax
		}
		cooldown *= time.Duration(uint(1) << uint(streak))
		if cooldown > rateLimitCooldownMax {
			cooldown = rateLimitCooldownMax
		}
	}
	return cooldown
}

func ClearLimiter(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	if val, ok := requestGates.Load(sourceID); ok {
		gate := val.(*sourceRequestGate)
		gate.mu.Lock()
		gate.nextAllowedAt = time.Time{}
		gate.rateLimitHits = 0
		gate.rateLimitStreak = 0
		gate.lastRateLimitHit = time.Time{}
		gate.mu.Unlock()
	}
}

func getSourceRequestGate(sourceID string) *sourceRequestGate {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return &sourceRequestGate{}
	}
	if val, ok := requestGates.Load(sourceID); ok {
		return val.(*sourceRequestGate)
	}
	gate := &sourceRequestGate{}
	actual, _ := requestGates.LoadOrStore(sourceID, gate)
	return actual.(*sourceRequestGate)
}

func getSourceInterval(sourceID string, fallback *model.FilmSource) time.Duration {
	if fallback != nil && fallback.Interval > 0 {
		return time.Duration(fallback.Interval) * time.Millisecond
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID != "" {
		if latest := repository.FindCollectSourceById(sourceID); latest != nil && latest.Interval > 0 {
			return time.Duration(latest.Interval) * time.Millisecond
		}
	}
	return config.DefaultSpiderInterval * time.Millisecond
}

func waitSourceRequestTurn(ctx context.Context, s *model.FilmSource, tag string) (func(error), error) {
	if s == nil {
		return func(error) {}, errors.New("采集站信息不存在")
	}

	gate := getSourceRequestGate(s.Id)

	for {
		gate.mu.Lock()
		waitUntil := gate.nextAllowedAt
		now := time.Now()
		if waitUntil.IsZero() || !waitUntil.After(now) {
			grantedAt := now
			interval := getSourceInterval(s.Id, s)
			// 若当前站点处于限流敏感状态(rateLimitHits > 0)，平滑拉长单次请求放行间隔
			if gate.rateLimitHits > 0 {
				adaptiveInterval := interval * time.Duration(1+gate.rateLimitHits)
				if adaptiveInterval > 1000*time.Millisecond {
					adaptiveInterval = 1000 * time.Millisecond
				}
				if adaptiveInterval > interval {
					interval = adaptiveInterval
				}
			}
			gate.nextAllowedAt = grantedAt.Add(interval)
			gate.mu.Unlock()
			return func(requestErr error) {
				if requestErr == nil {
					gate.mu.Lock()
					gate.noteSuccessLocked(time.Now())
					gate.mu.Unlock()
					return
				}
				if utils.IsRateLimitedErr(requestErr) {
					gate.mu.Lock()
					now := time.Now()
					hits, streak := gate.noteRateLimitLocked(now)
					cooldown := rateLimitCooldown(hits, streak, interval)
					protectUntil := now.Add(cooldown)
					if gate.nextAllowedAt.Before(protectUntil) {
						gate.nextAllowedAt = protectUntil
					}
					nextAllowedAt := gate.nextAllowedAt
					gate.mu.Unlock()
					log.Printf("[Spider][RateLimit] 站点 %s %s触发限流，指数退避冷却 cooldown_ms=%d (hits=%d streak=%d) next_at=%d err=%v", s.Name, tag, cooldown.Milliseconds(), hits, streak, nextAllowedAt.UnixMilli(), requestErr)
					return
				}
			}, nil
		}

		cooldown := waitUntil.Sub(now)
		gate.mu.Unlock()
		timer := time.NewTimer(cooldown)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
