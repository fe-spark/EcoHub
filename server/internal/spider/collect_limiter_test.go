package spider

import (
	"context"
	"errors"
	"testing"
	"time"

	"server/internal/model"
)

func TestWaitTurnRateLimitCooldown(t *testing.T) {
	const sourceID = "test_rl_cooldown"
	ClearLimiter(sourceID)
	defer ClearLimiter(sourceID)

	source := &model.FilmSource{Id: sourceID, Name: "限流测试"}
	release, err := waitSourceRequestTurn(context.Background(), source, "turn")
	if err != nil {
		t.Fatal(err)
	}
	release(errors.New("HTTP 429 Too Many Requests"))

	gate := getSourceRequestGate(sourceID)
	gate.mu.Lock()
	next := gate.nextAllowedAt
	gate.mu.Unlock()
	wait := time.Until(next)
	if wait < 4*time.Second || wait > rateLimitCooldown+time.Second {
		t.Fatalf("cooldown wait = %s", wait)
	}

	release, err = waitSourceRequestTurn(context.Background(), source, "turn")
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	release(errors.New("HTTP 429 Too Many Requests"))
	gate.mu.Lock()
	refreshed := gate.nextAllowedAt
	gate.mu.Unlock()
	if refreshed.Before(before.Add(4 * time.Second)) {
		t.Fatal("a later 429 should refresh the cooldown from now")
	}
	if refreshed.After(before.Add(rateLimitCooldown + time.Second)) {
		t.Fatal("cooldown should not stack")
	}
}
