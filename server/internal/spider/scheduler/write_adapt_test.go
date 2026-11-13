package scheduler

import (
	"errors"
	"testing"
	"time"

	"server/internal/config"
)

func TestWritePaceIgnoresCostAndReturnsAfterQuiet(t *testing.T) {
	pace := &writePace{}
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	pace.now = func() time.Time { return now }
	limit := config.WriteSlotMax()

	for _, cost := range []time.Duration{time.Millisecond, 100 * time.Millisecond, 2 * time.Second} {
		next, reason, changed := pace.observe(limit, false)
		if changed || next != limit || reason != "" {
			t.Fatalf("cost %s moved slot next=%d reason=%s", cost, next, reason)
		}
	}

	next, reason, changed := pace.observe(limit, true)
	if !changed || next != limit-1 || reason != "锁等待" {
		t.Fatalf("lock next=%d reason=%s changed=%v", next, reason, changed)
	}
	limit = next

	next, reason, changed = pace.observe(limit, false)
	if changed {
		t.Fatalf("inside quiet next=%d reason=%s", next, reason)
	}

	now = now.Add(writeAdaptHold)
	next, reason, changed = pace.observe(limit, false)
	if !changed || next != limit+1 || reason != "回升" {
		t.Fatalf("after quiet next=%d reason=%s changed=%v", next, reason, changed)
	}
	if next > config.WriteSlotMax() {
		t.Fatalf("above cap %d", next)
	}
	limit = next

	next, reason, changed = pace.observe(limit, true)
	if !changed || next != limit-1 || reason != "锁等待" {
		t.Fatalf("lock after raise next=%d reason=%s changed=%v", next, reason, changed)
	}

	next, reason, changed = pace.observe(config.CollectWriteSlotMin, true)
	if changed || next != config.CollectWriteSlotMin || reason != "" {
		t.Fatalf("lock at floor next=%d reason=%s changed=%v", next, reason, changed)
	}
	if !isWriteLockErr(errors.New("Error 1213 (40001): Deadlock found when trying to get lock")) {
		t.Fatal("1213 should count as lock pressure")
	}
	if !isWriteLockErr(errors.New("Error 1205 (HY000): Lock wait timeout exceeded")) {
		t.Fatal("1205 should count as lock pressure")
	}
	if isWriteLockErr(errors.New("Error 1406 (22001): Data too long")) {
		t.Fatal("1406 is not lock pressure")
	}
}

func TestNoteWrite_FinalLockErrorAppliesLowerSlot(t *testing.T) {
	prevInflight := config.CollectWriteMaxInflight
	prevPages := config.CollectPageWorkers
	prevFetch := config.CollectFetchInFlight
	prevPer := config.CollectWriteMaxPendingPagesPerSource
	prevGlobal := config.CollectWriteMaxPendingPagesGlobal
	t.Cleanup(func() {
		config.CollectWriteMaxInflight = prevInflight
		config.CollectPageWorkers = prevPages
		config.CollectFetchInFlight = prevFetch
		config.CollectWriteMaxPendingPagesPerSource = prevPer
		config.CollectWriteMaxPendingPagesGlobal = prevGlobal
	})

	lane := newWriteLane("full")
	lane.adapt = true
	lane.writeLimit = config.WriteSlotMax()
	lane.noteWrite(2*time.Second, nil)
	if lane.writeLimit != config.WriteSlotMax() {
		t.Fatalf("slow success moved writeLimit to %d", lane.writeLimit)
	}

	lane.noteWrite(time.Millisecond, errors.New("Error 1213 (40001): Deadlock found when trying to get lock"))
	if lane.writeLimit != config.WriteSlotMax()-1 {
		t.Fatalf("writeLimit=%d", lane.writeLimit)
	}
	if config.CollectPageWorkers != prevPages || config.CollectWriteMaxInflight != prevInflight {
		t.Fatalf("pages=%d inflight=%d", config.CollectPageWorkers, config.CollectWriteMaxInflight)
	}
	if config.CollectFetchInFlight != prevFetch || config.CollectWriteMaxPendingPagesPerSource != prevPer || config.CollectWriteMaxPendingPagesGlobal != prevGlobal {
		t.Fatalf("fetch=%d per=%d global=%d", config.CollectFetchInFlight, config.CollectWriteMaxPendingPagesPerSource, config.CollectWriteMaxPendingPagesGlobal)
	}
	if lane.maxPerSource != prevPer || lane.maxGlobal != prevGlobal {
		t.Fatalf("lane queue per=%d global=%d", lane.maxPerSource, lane.maxGlobal)
	}
}
