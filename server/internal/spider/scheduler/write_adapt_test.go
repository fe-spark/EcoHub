package scheduler

import (
	"errors"
	"testing"
	"time"

	"server/internal/config"
)

func TestWritePaceAdjustsOnLatencyAndLocks(t *testing.T) {
	var pace writePace
	limit := config.CollectWriteSlotMin

	for i := 0; i < writeAdaptWindow; i++ {
		next, reason, changed := pace.observe(limit, 100*time.Millisecond, false)
		if i < writeAdaptWindow-1 {
			if changed {
				t.Fatal("grew before the window closed")
			}
			continue
		}
		if !changed || next != limit+1 || reason != "上探" {
			t.Fatalf("flat window should probe up, next=%d reason=%s changed=%v", next, reason, changed)
		}
		limit = next
	}
	if pace.prev != 100*time.Millisecond {
		t.Fatalf("baseline=%s", pace.prev)
	}

	for i := 0; i < writeAdaptWindow; i++ {
		next, reason, changed := pace.observe(limit, 100*time.Millisecond, false)
		if i < writeAdaptWindow-1 {
			if changed {
				t.Fatal("grew before the window closed")
			}
			continue
		}
		if !changed || next != limit+1 || reason != "上探" {
			t.Fatalf("steady window should keep probing, next=%d reason=%s changed=%v", next, reason, changed)
		}
		limit = next
	}

	for i := 0; i < writeAdaptWindow; i++ {
		next, reason, changed := pace.observe(limit, 250*time.Millisecond, false)
		if i < writeAdaptWindow-1 {
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
	next, reason, changed = pace.observe(limit, time.Millisecond, true)
	if changed || next != config.CollectWriteSlotMin || reason != "" {
		t.Fatalf("lock at floor next=%d reason=%s changed=%v", next, reason, changed)
	}
	next, reason, changed = pace.observe(4, time.Millisecond, true)
	if !changed || next != 3 || reason != "锁等待" {
		t.Fatalf("lock shrink next=%d reason=%s changed=%v", next, reason, changed)
	}
	if !isWriteLockErr(errors.New("Error 1213 (40001): Deadlock found when trying to get lock")) {
		t.Fatal("1213 should count as lock pressure")
	}
	if isWriteLockErr(errors.New("Error 1406 (22001): Data too long")) {
		t.Fatal("1406 is not lock pressure")
	}
}
