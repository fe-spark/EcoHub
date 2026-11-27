package utils

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseRetryAfterHeader(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

	if got := parseRetryAfterHeader("5", now); got != 5*time.Second {
		t.Fatalf("seconds Retry-After=%s, want 5s", got)
	}
	if got := parseRetryAfterHeader("9999", now); got != maxRetryAfterCooldown {
		t.Fatalf("clamped Retry-After=%s, want %s", got, maxRetryAfterCooldown)
	}
	if got := parseRetryAfterHeader("0", now); got != 0 {
		t.Fatalf("zero Retry-After=%s, want 0", got)
	}
	httpDate := now.Add(12 * time.Second).Format(http.TimeFormat)
	if got := parseRetryAfterHeader(httpDate, now); got != 12*time.Second {
		t.Fatalf("http-date Retry-After=%s, want 12s", got)
	}
	if got := parseRetryAfterHeader("invalid", now); got != 0 {
		t.Fatalf("invalid Retry-After=%s, want 0", got)
	}
}

func TestApiGet_429PropagatesRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	req := RequestInfo{Uri: srv.URL}
	ApiGet(&req)
	err := errors.New(req.Err)
	if !IsRateLimitedErr(err) {
		t.Fatalf("expected rate limit error, got %q", req.Err)
	}
	if got := RateLimitRetryAfter(err); got != 7*time.Second {
		t.Fatalf("RateLimitRetryAfter=%s, want 7s (err=%q)", got, req.Err)
	}
}
