package snapshot

import (
	"reflect"
	"testing"
)

func resetPlaySummaryPending(t *testing.T, pending map[int64]struct{}) {
	t.Helper()
	playSummaryRefresh.mu.Lock()
	playSummaryRefresh.pending = pending
	playSummaryRefresh.flushing = false
	playSummaryRefresh.waiters = nil
	playSummaryRefresh.mu.Unlock()
	t.Cleanup(func() {
		playSummaryRefresh.mu.Lock()
		playSummaryRefresh.pending = make(map[int64]struct{})
		playSummaryRefresh.flushing = false
		playSummaryRefresh.waiters = nil
		playSummaryRefresh.mu.Unlock()
	})
}

func TestTakePendingPlaySummaryMids_DoesNotDrainOtherBatch(t *testing.T) {
	resetPlaySummaryPending(t, map[int64]struct{}{
		101: {},
		202: {},
		303: {},
	})

	taken := takePendingPlaySummaryMids([]int64{101})
	if !reflect.DeepEqual(taken, map[int64]struct{}{101: {}}) {
		t.Fatalf("this batch should only take its own mids, got %v", taken)
	}

	playSummaryRefresh.mu.Lock()
	remaining := make(map[int64]struct{}, len(playSummaryRefresh.pending))
	for mid := range playSummaryRefresh.pending {
		remaining[mid] = struct{}{}
	}
	playSummaryRefresh.mu.Unlock()
	if !reflect.DeepEqual(remaining, map[int64]struct{}{202: {}, 303: {}}) {
		t.Fatalf("other batch pending should stay, got %v", remaining)
	}
}

func TestFlushPlaySummaryRefreshByMids_EmptyDoesNotTouchPending(t *testing.T) {
	resetPlaySummaryPending(t, map[int64]struct{}{
		11: {},
		22: {},
	})

	mids, err := FlushPlaySummaryRefreshByMids(nil)
	if err != nil {
		t.Fatalf("empty flush: %v", err)
	}
	if len(mids) != 0 {
		t.Fatalf("expected no mids, got %v", mids)
	}

	playSummaryRefresh.mu.Lock()
	pendingCount := len(playSummaryRefresh.pending)
	playSummaryRefresh.mu.Unlock()
	if pendingCount != 2 {
		t.Fatalf("empty flush must not drain other pending, got %d", pendingCount)
	}
}
