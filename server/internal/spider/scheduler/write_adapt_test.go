package scheduler

import (
	"errors"
	"testing"
	"time"

	"server/internal/config"
)

func TestWritePaceFullSpeedBacksOffThenReturns(t *testing.T) {
	var pace writePace
	limit := config.CollectWriteSlotMax

	for i := 0; i < writeAdaptWindow; i++ {
		next, reason, changed := pace.observe(limit, 100*time.Millisecond, false)
		if changed || next != limit || reason != "" {
			t.Fatalf("full speed should hold, next=%d reason=%s changed=%v", next, reason, changed)
		}
	}
	if pace.prev != 100*time.Millisecond {
		t.Fatalf("baseline=%s", pace.prev)
	}

	for i := 0; i < writeAdaptWindow; i++ {
		next, reason, changed := pace.observe(limit, 250*time.Millisecond, false)
		if i < writeAdaptWindow-1 {
			if changed {
				t.Fatal("shrunk before the window closed")
			}
			continue
		}
		if !changed || next != limit-1 || reason != "变慢" {
			t.Fatalf("slower window next=%d reason=%s changed=%v", next, reason, changed)
		}
		limit = next
	}

	next, reason, changed := pace.observe(limit, time.Millisecond, true)
	if !changed || next != limit-1 || reason != "锁等待" {
		t.Fatalf("lock shrink next=%d reason=%s changed=%v", next, reason, changed)
	}
	limit = next

	for i := 0; i < writeAdaptWindow; i++ {
		next, reason, changed = pace.observe(limit, 80*time.Millisecond, false)
		if changed {
			t.Fatalf("window after a lock should hold, next=%d reason=%s", next, reason)
		}
	}

	for i := 0; i < writeAdaptWindow; i++ {
		next, reason, changed = pace.observe(limit, 80*time.Millisecond, false)
		if i < writeAdaptWindow-1 {
			if changed {
				t.Fatal("grew before the window closed")
			}
			continue
		}
		if !changed || next != limit+1 || reason != "上探" {
			t.Fatalf("faster window next=%d reason=%s changed=%v", next, reason, changed)
		}
		limit = next
	}
	if limit > config.CollectWriteSlotMax {
		t.Fatalf("above cap %d", limit)
	}

	next, reason, changed = pace.observe(config.CollectWriteSlotMin, time.Millisecond, true)
	if changed || next != config.CollectWriteSlotMin || reason != "" {
		t.Fatalf("lock at floor next=%d reason=%s changed=%v", next, reason, changed)
	}
	if !isWriteLockErr(errors.New("Error 1213 (40001): Deadlock found when trying to get lock")) {
		t.Fatal("1213 should count as lock pressure")
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
	lane.followConfig = true
	lane.writeLimit = config.CollectWriteSlotMax
	lane.noteWrite(time.Millisecond, nil)
	if lane.writeLimit != config.CollectWriteSlotMax {
		t.Fatalf("success moved writeLimit to %d", lane.writeLimit)
	}

	lane.noteWrite(time.Millisecond, errors.New("Error 1213 (40001): Deadlock found when trying to get lock"))
	if lane.writeLimit != config.CollectWriteSlotMax-1 {
		t.Fatalf("writeLimit=%d", lane.writeLimit)
	}
	if config.CollectPageWorkers != lane.writeLimit || config.CollectWriteMaxInflight != lane.writeLimit {
		t.Fatalf("pages=%d inflight=%d limit=%d", config.CollectPageWorkers, config.CollectWriteMaxInflight, lane.writeLimit)
	}
	if lane.maxPerSource != config.CollectWriteMaxPendingPagesPerSource || lane.maxGlobal != config.CollectWriteMaxPendingPagesGlobal {
		t.Fatalf("queue per=%d global=%d", lane.maxPerSource, lane.maxGlobal)
	}
}
