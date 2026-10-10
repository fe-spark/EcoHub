package fetcher

import (
	"testing"

	"server/internal/config"
)

func TestGetSourcePageConcurrencySharesHostBudget(t *testing.T) {
	prevWorkers := config.CollectPageWorkers
	prevSolo := config.CollectPageWorkersSolo
	prevBudget := config.CollectFetchInFlight
	prevDeps := deps
	t.Cleanup(func() {
		config.CollectPageWorkers = prevWorkers
		config.CollectPageWorkersSolo = prevSolo
		config.CollectFetchInFlight = prevBudget
		deps = prevDeps
	})
	config.CollectPageWorkers = 16
	config.CollectPageWorkersSolo = 24
	config.CollectFetchInFlight = 48

	deps.LiveTaskCount = func() int { return 1 }
	if got := GetSourcePageConcurrency(nil); got != 24 {
		t.Fatalf("solo=%d", got)
	}
	deps.LiveTaskCount = func() int { return 12 }
	if got := GetSourcePageConcurrency(nil); got != 4 {
		t.Fatalf("12 stations=%d", got)
	}
	deps.LiveTaskCount = func() int { return 2 }
	if got := GetSourcePageConcurrency(nil); got != 16 {
		t.Fatalf("2 stations=%d", got)
	}
}
