package spider

import (
	"context"
	"errors"
	"testing"
	"time"

	"server/internal/model"
)

func TestAdaptiveRateLimiting_SmoothBackoff(t *testing.T) {
	const sourceID = "test_source_rl"
	ClearLimiter(sourceID)
	defer ClearLimiter(sourceID)

	source := &model.FilmSource{
		Id:       sourceID,
		Name:     "Test RL",
		Interval: 50,
	}

	release1, err := waitSourceRequestTurn(context.Background(), source, "test-turn-1")
	if err != nil {
		t.Fatalf("unexpected wait turn error: %v", err)
	}
	release1(errors.New("HTTP 429 Too Many Requests"))

	gate := getSourceRequestGate(sourceID)
	gate.mu.Lock()
	hits := gate.rateLimitHits
	nextAllowed := gate.nextAllowedAt
	gate.mu.Unlock()

	if hits != 1 {
		t.Fatalf("expected rateLimitHits=1 after 429, got %d", hits)
	}
	if !nextAllowed.After(time.Now()) {
		t.Fatalf("expected nextAllowedAt to be in the future after 429 backoff")
	}

	// 恢复窗口内的成功不得清零，否则并发成功页会把退避打回全速。
	gate.mu.Lock()
	gate.nextAllowedAt = time.Now().Add(-100 * time.Millisecond)
	gate.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	release2, err := waitSourceRequestTurn(ctx, source, "test-turn-2")
	if err != nil {
		t.Fatalf("unexpected wait turn error: %v", err)
	}
	release2(nil)

	gate.mu.Lock()
	hitsAfterSuccess := gate.rateLimitHits
	gate.mu.Unlock()
	if hitsAfterSuccess != 1 {
		t.Fatalf("expected rateLimitHits=1 during recover window, got %d", hitsAfterSuccess)
	}
}

func TestRateLimitColdHitStillCounts(t *testing.T) {
	const sourceID = "test_source_rl_cold"
	ClearLimiter(sourceID)
	defer ClearLimiter(sourceID)

	source := &model.FilmSource{Id: sourceID, Name: "Test RL Cold", Interval: 50}
	gate := getSourceRequestGate(sourceID)
	gate.mu.Lock()
	gate.rateLimitHits = 0
	gate.lastRateLimitHit = time.Now()
	gate.nextAllowedAt = time.Now().Add(-time.Millisecond)
	gate.mu.Unlock()

	release, err := waitSourceRequestTurn(context.Background(), source, "cold-429")
	if err != nil {
		t.Fatalf("unexpected wait turn error: %v", err)
	}
	release(errors.New("Too Many Requests, status=429"))

	gate.mu.Lock()
	hits := gate.rateLimitHits
	nextAllowed := gate.nextAllowedAt
	gate.mu.Unlock()
	if hits != 1 {
		t.Fatalf("expected rateLimitHits=1 when counter was cold inside the merge window, got %d", hits)
	}
	if !nextAllowed.After(time.Now().Add(500 * time.Millisecond)) {
		t.Fatalf("expected cooldown of at least 1s, nextAllowed=%s", nextAllowed)
	}
}

func TestRateLimitBurstDoesNotStackInsideWindow(t *testing.T) {
	const sourceID = "test_source_rl_burst"
	ClearLimiter(sourceID)
	defer ClearLimiter(sourceID)

	source := &model.FilmSource{Id: sourceID, Name: "Test RL Burst", Interval: 50}
	release1, err := waitSourceRequestTurn(context.Background(), source, "burst-1")
	if err != nil {
		t.Fatalf("unexpected wait turn error: %v", err)
	}
	release1(errors.New("Too Many Requests, status=429"))

	gate := getSourceRequestGate(sourceID)
	gate.mu.Lock()
	gate.nextAllowedAt = time.Now().Add(-time.Millisecond)
	gate.mu.Unlock()

	release2, err := waitSourceRequestTurn(context.Background(), source, "burst-2")
	if err != nil {
		t.Fatalf("unexpected wait turn error: %v", err)
	}
	release2(errors.New("Too Many Requests, status=429"))

	gate.mu.Lock()
	hits := gate.rateLimitHits
	gate.mu.Unlock()
	if hits != 1 {
		t.Fatalf("expected burst inside 1s to stay at 1 hit, got %d", hits)
	}
}

func TestRateLimitSuccessAfterRecoverDecrements(t *testing.T) {
	const sourceID = "test_source_rl_recover"
	ClearLimiter(sourceID)
	defer ClearLimiter(sourceID)

	source := &model.FilmSource{Id: sourceID, Name: "Test RL Recover", Interval: 50}
	release1, err := waitSourceRequestTurn(context.Background(), source, "recover-429")
	if err != nil {
		t.Fatalf("unexpected wait turn error: %v", err)
	}
	release1(errors.New("Too Many Requests, status=429"))

	gate := getSourceRequestGate(sourceID)
	gate.mu.Lock()
	gate.lastRateLimitHit = time.Now().Add(-rateLimitRecoverAfter - time.Second)
	gate.nextAllowedAt = time.Now().Add(-time.Millisecond)
	gate.mu.Unlock()

	release2, err := waitSourceRequestTurn(context.Background(), source, "recover-ok")
	if err != nil {
		t.Fatalf("unexpected wait turn error: %v", err)
	}
	release2(nil)

	gate.mu.Lock()
	hits := gate.rateLimitHits
	last := gate.lastRateLimitHit
	gate.mu.Unlock()
	if hits != 0 || !last.IsZero() {
		t.Fatalf("expected hits=0 and cleared lastRateLimitHit after recover, hits=%d last=%s", hits, last)
	}
}

func TestRateLimitCooldownFloor(t *testing.T) {
	if got := rateLimitCooldown(0, 0, 50*time.Millisecond); got != time.Second {
		t.Fatalf("hits=0 cooldown = %s, want 1s", got)
	}
	if got := rateLimitCooldown(3, 0, 2*time.Second); got != rateLimitCooldownBaseMax {
		t.Fatalf("hits=3 streak=0 cooldown = %s, want base cap %s", got, rateLimitCooldownBaseMax)
	}
	if got := rateLimitCooldown(3, rateLimitStreakMax, 2*time.Second); got != rateLimitCooldownMax {
		t.Fatalf("streak max cooldown = %s, want cap %s", got, rateLimitCooldownMax)
	}
}

func TestRateLimitStreakExtendsCooldown(t *testing.T) {
	const sourceID = "test_source_rl_streak"
	ClearLimiter(sourceID)
	defer ClearLimiter(sourceID)

	source := &model.FilmSource{Id: sourceID, Name: "Test RL Streak", Interval: 1000}
	gate := getSourceRequestGate(sourceID)
	gate.mu.Lock()
	gate.rateLimitHits = rateLimitMaxHits
	gate.lastRateLimitHit = time.Now().Add(-2 * time.Second)
	gate.nextAllowedAt = time.Now().Add(-time.Millisecond)
	gate.mu.Unlock()

	release, err := waitSourceRequestTurn(context.Background(), source, "streak-429")
	if err != nil {
		t.Fatalf("unexpected wait turn error: %v", err)
	}
	release(errors.New("Too Many Requests, status=429"))

	gate.mu.Lock()
	streak := gate.rateLimitStreak
	nextAllowed := gate.nextAllowedAt
	gate.lastRateLimitHit = time.Now().Add(-rateLimitRecoverAfter - time.Second)
	gate.nextAllowedAt = time.Now().Add(-time.Millisecond)
	gate.mu.Unlock()
	if streak != 1 {
		t.Fatalf("expected streak=1 after a fresh 429 at max hits, got %d", streak)
	}
	if !nextAllowed.After(time.Now().Add(5 * time.Second)) {
		t.Fatalf("expected cooldown beyond 5s, nextAllowed=%s", nextAllowed)
	}

	releaseOK, err := waitSourceRequestTurn(context.Background(), source, "streak-ok")
	if err != nil {
		t.Fatalf("unexpected wait turn error: %v", err)
	}
	releaseOK(nil)

	gate.mu.Lock()
	streak = gate.rateLimitStreak
	hits := gate.rateLimitHits
	gate.mu.Unlock()
	if streak != 0 || hits != rateLimitMaxHits-1 {
		t.Fatalf("expected streak cleared and hits decremented, streak=%d hits=%d", streak, hits)
	}
}
