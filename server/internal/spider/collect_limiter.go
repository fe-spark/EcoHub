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

// requestGates 按站点控制外部采集接口请求间隔：同一站点两次请求之间至少等待站点 Interval。
var requestGates sync.Map

type sourceRequestGate struct {
	mu            sync.Mutex
	nextAllowedAt time.Time
	rateLimitHits int
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
	sourceID = strings.TrimSpace(sourceID)
	if sourceID != "" {
		if latest := repository.FindCollectSourceById(sourceID); latest != nil && latest.Interval > 0 {
			return time.Duration(latest.Interval) * time.Millisecond
		}
	}
	if fallback != nil && fallback.Interval > 0 {
		return time.Duration(fallback.Interval) * time.Millisecond
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
				if adaptiveInterval > 1500*time.Millisecond {
					adaptiveInterval = 1500 * time.Millisecond
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
					if gate.rateLimitHits > 0 {
						gate.rateLimitHits--
					}
					gate.mu.Unlock()
					return
				}
				if utils.IsRateLimitedErr(requestErr) {
					gate.mu.Lock()
					gate.rateLimitHits++
					hits := gate.rateLimitHits
					if hits > 5 {
						hits = 5
					}
					backoffFactor := time.Duration(1 << uint(hits-1))
					cooldown := interval * backoffFactor
					if cooldown < 1*time.Second {
						cooldown = 1 * time.Second
					}
					if cooldown > 15*time.Second {
						cooldown = 15 * time.Second
					}
					protectUntil := time.Now().Add(cooldown)
					if gate.nextAllowedAt.Before(protectUntil) {
						gate.nextAllowedAt = protectUntil
					}
					nextAllowedAt := gate.nextAllowedAt
					gate.mu.Unlock()
					log.Printf("[Spider][RateLimit] 站点 %s %s触发限流，指数退避冷却 cooldown_ms=%d (hits=%d) next_at=%d err=%v", s.Name, tag, cooldown.Milliseconds(), hits, nextAllowedAt.UnixMilli(), requestErr)
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
