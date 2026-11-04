package spider

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/utils"
)

const (
	// rateLimitCooldown 收到 429 后，该站下一次请求开始至少再等这么久。
	// 后来的 429 只把这个时刻刷新到现在加冷却，不叠加。
	rateLimitCooldown = 5 * time.Second
)

// requestGates 按站点维护限流冷却状态。
var requestGates sync.Map

type sourceRequestGate struct {
	mu            sync.Mutex
	nextAllowedAt time.Time
}

func ClearLimiter(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	requestGates.Delete(sourceID)
}

// DropSourceRuntime 采集站删除后丢掉运行时条目。
func DropSourceRuntime(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	ClearLimiter(sourceID)
	sourceExclusive.Delete(sourceID)
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
			gate.mu.Unlock()
			return func(requestErr error) {
				if !utils.IsRateLimitedErr(requestErr) {
					return
				}
				gate.mu.Lock()
				protectUntil := time.Now().Add(rateLimitCooldown)
				if gate.nextAllowedAt.Before(protectUntil) {
					gate.nextAllowedAt = protectUntil
				}
				nextAllowedAt := gate.nextAllowedAt
				gate.mu.Unlock()
				log.Printf("[Spider][RateLimit] 站点 %s %s触发限流，冷却 %s next_at=%d err=%v", s.Name, tag, rateLimitCooldown, nextAllowedAt.UnixMilli(), requestErr)
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
