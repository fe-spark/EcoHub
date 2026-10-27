package spider

import (
	"errors"
	"testing"
	"time"
)

func TestWaitSourceReturnsAfterEnd(t *testing.T) {
	state := newCollectLifecycle()
	if err := state.beginSource("s1"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- state.waitSource("s1", time.Second)
	}()
	time.Sleep(20 * time.Millisecond)
	state.endSource("s1")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCollectLifecycle_IsBusy(t *testing.T) {
	s := newCollectLifecycle()
	if s.isBusy() {
		t.Fatal("expected isBusy to be false initially")
	}

	// 1. 测试 source 活跃
	if err := s.beginSource("source-1"); err != nil {
		t.Fatalf("beginSource failed: %v", err)
	}
	if !s.isBusy() {
		t.Fatal("expected isBusy to be true after beginSource")
	}
	s.endSource("source-1")
	if s.isBusy() {
		t.Fatal("expected isBusy to be false after endSource")
	}

	// 2. 测试 publishing 活跃
	s.beginPublish()
	if !s.isBusy() {
		t.Fatal("expected isBusy to be true after beginPublish")
	}
	s.beginPublish()
	s.endPublish()
	if !s.isBusy() {
		t.Fatal("expected isBusy to remain true when publishing count > 0")
	}
	s.endPublish()
	if s.isBusy() {
		t.Fatal("expected isBusy to be false after publishing count returns to 0")
	}
}

func TestCollectLifecycle_WaitNotBusy(t *testing.T) {
	s := newCollectLifecycle()
	if err := s.waitNotBusy(0); err != nil {
		t.Fatalf("idle waitNotBusy: %v", err)
	}

	if err := s.beginSource("s1"); err != nil {
		t.Fatalf("beginSource: %v", err)
	}
	if err := s.waitNotBusy(0); err == nil {
		t.Fatal("expected timeout while source active")
	}

	done := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.endSource("s1")
		close(done)
	}()
	if err := s.waitNotBusy(time.Second); err != nil {
		t.Fatalf("waitNotBusy while source ending: %v", err)
	}
	<-done

	s.beginPublish()
	if err := s.waitNotBusy(0); err == nil {
		t.Fatal("expected timeout while publishing")
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.endPublish()
	}()
	if err := s.waitNotBusy(time.Second); err != nil {
		t.Fatalf("waitNotBusy while publish ending: %v", err)
	}

	s.beginPublish()
	start := time.Now()
	if err := s.waitNotBusy(40 * time.Millisecond); err == nil {
		t.Fatal("expected timeout while publish stays active")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("timeout waited %s", time.Since(start))
	}
	s.endPublish()
}

func TestCollectLifecycle_WaitSourceTimeout(t *testing.T) {
	s := newCollectLifecycle()
	if err := s.beginSource("s1"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err := s.waitSource("s1", 40*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("timeout waited %s", time.Since(start))
	}
	if err := s.beginSource("s2"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.beginDrain(time.Second)
	}()
	time.Sleep(20 * time.Millisecond)
	if err := s.beginSource("s3"); err == nil {
		t.Fatal("expected drain to reject new collect")
	}
	s.endSource("s1")
	s.endSource("s2")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s.endDrain()
}

func TestCollectLifecycle_RunExclusiveIsBusy(t *testing.T) {
	s := newCollectLifecycle()
	ran := false
	err := s.runExclusive(func() error {
		ran = true
		if !s.isBusy() {
			t.Error("expected isBusy to be true inside runExclusive")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("runExclusive failed: %v", err)
	}
	if !ran {
		t.Fatal("action inside runExclusive was not executed")
	}
	if s.isBusy() {
		t.Fatal("expected isBusy to be false after runExclusive finishes")
	}
}

func TestCollectLifecycle_ReserveMaintenanceBlocksNewCollect(t *testing.T) {
	s := newCollectLifecycle()
	if err := s.beginSource("in-flight"); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.reserveMaintenance(time.Second, func() error {
			close(entered)
			if err := s.beginSource("late"); err == nil {
				return errors.New("maintenance should reject new collect")
			}
			return nil
		})
	}()
	select {
	case <-entered:
		t.Fatal("maintenance entered before in-flight source finished")
	case <-time.After(30 * time.Millisecond):
	}
	s.endSource("in-flight")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.beginSource("after"); err != nil {
		t.Fatalf("collect should resume after maintenance: %v", err)
	}
	s.endSource("after")
}

func TestCollectLifecycle_BeginDrainTimeoutDoesNotReleaseOwner(t *testing.T) {
	s := newCollectLifecycle()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.reserveMaintenance(time.Second, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not start")
	}
	if err := s.beginDrain(40 * time.Millisecond); err == nil || !errors.Is(err, errDrainTimeout) {
		t.Fatalf("second drain should time out, got %v", err)
	}
	if err := s.beginSource("during"); err == nil {
		t.Fatal("owner gate must still reject collect after the other drain times out")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
