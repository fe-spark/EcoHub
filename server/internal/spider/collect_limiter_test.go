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

func TestWaitTurnZeroIntervalDoesNotBlock(t *testing.T) {
	const sourceID = "test_collect_interval_zero"
	ClearLimiter(sourceID)
	defer ClearLimiter(sourceID)

	source := &model.FilmSource{Id: sourceID, Name: "间隔零"}
	started := time.Now()
	for i := 0; i < 3; i++ {
		release, err := waitSourceRequestTurn(context.Background(), source, "page")
		if err != nil {
			t.Fatal(err)
		}
		release(nil)
	}
	if waited := time.Since(started); waited > 50*time.Millisecond {
		t.Fatalf("zero interval blocked, waited %s", waited)
	}
}

func TestWaitTurnCollectIntervalSpacesStarts(t *testing.T) {
	const sourceID = "test_collect_interval"
	ClearLimiter(sourceID)
	ClearLimiter(sourceID + "_other")
	defer ClearLimiter(sourceID)
	defer ClearLimiter(sourceID + "_other")

	source := &model.FilmSource{Id: sourceID, Name: "间隔", CollectIntervalMs: 120}
	other := &model.FilmSource{Id: sourceID + "_other", Name: "另一站", CollectIntervalMs: 120}
	started := time.Now()
	release, err := waitSourceRequestTurn(context.Background(), source, "page")
	if err != nil {
		t.Fatal(err)
	}
	if !claimSourceRequestTurn(source) {
		t.Fatal("first claim")
	}
	release(nil)
	otherRelease, err := waitSourceRequestTurn(context.Background(), other, "page")
	if err != nil {
		t.Fatal(err)
	}
	otherRelease(nil)
	if waited := time.Since(started); waited > 50*time.Millisecond {
		t.Fatalf("another source waited for this source, waited %s", waited)
	}
	for i := 0; i < 2; i++ {
		release, err = waitSourceRequestTurn(context.Background(), source, "page")
		if err != nil {
			t.Fatal(err)
		}
		if !claimSourceRequestTurn(source) {
			t.Fatal("claim")
		}
		release(nil)
	}
	waited := time.Since(started)
	if waited < 200*time.Millisecond || waited > 500*time.Millisecond {
		t.Fatalf("three starts should keep two gaps, waited %s", waited)
	}
}

func TestWaitTurnIntervalDoesNotShortenRateLimit(t *testing.T) {
	const sourceID = "test_collect_interval_429"
	ClearLimiter(sourceID)
	defer ClearLimiter(sourceID)

	source := &model.FilmSource{Id: sourceID, Name: "间隔加限流", CollectIntervalMs: 80}
	release, err := waitSourceRequestTurn(context.Background(), source, "page")
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	release(errors.New("HTTP 429 Too Many Requests"))

	gate := getSourceRequestGate(sourceID)
	gate.mu.Lock()
	next := gate.nextAllowedAt
	gate.mu.Unlock()
	wait := next.Sub(before)
	if wait < 4*time.Second || wait > rateLimitCooldown+time.Second {
		t.Fatalf("429 should keep the cooldown, wait=%s", wait)
	}
}

func TestClaimSourceRequestTurnOneWinner(t *testing.T) {
	const sourceID = "test_claim_one"
	ClearLimiter(sourceID)
	defer ClearLimiter(sourceID)

	source := &model.FilmSource{Id: sourceID, Name: "占许可", CollectIntervalMs: 80}
	if !claimSourceRequestTurn(source) {
		t.Fatal("first claim")
	}
	if claimSourceRequestTurn(source) {
		t.Fatal("second claim should wait for the interval")
	}
	time.Sleep(90 * time.Millisecond)
	if !claimSourceRequestTurn(source) {
		t.Fatal("claim after the interval")
	}
}
