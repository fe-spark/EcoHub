package spider

import (
	"errors"
	"testing"
	"time"
)

func TestCollectLifecycle_WaitUntilIdle(t *testing.T) {
	state := newCollectLifecycle()
	if err := state.beginSource("s1"); err != nil {
		t.Fatal(err)
	}
	if err := state.beginSource("s2"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		state.waitUntilIdle()
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("returned while two sources are collecting")
	default:
	}
	state.endSource("s1")
	time.Sleep(20 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("returned while one source is still collecting")
	default:
	}
	state.endSource("s2")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("did not resume after the last source")
	}
}

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

func TestBeginMaintenanceRejectsCollectAndSecondMaintenance(t *testing.T) {
	s := newCollectLifecycle()
	if err := s.beginSource("in-flight"); err != nil {
		t.Fatal(err)
	}
	if err := s.beginMaintenance(); err != nil {
		t.Fatal(err)
	}
	if err := s.beginSource("late"); !errors.Is(err, errCollectDuringMaintenance) {
		t.Fatalf("new collect = %v", err)
	}
	if err := s.beginMaintenance(); !errors.Is(err, errMaintenanceBusy) {
		t.Fatalf("second maintenance = %v", err)
	}
	s.endDrain()
	s.endSource("in-flight")
	if err := s.beginSource("after"); err != nil {
		t.Fatalf("collect should resume after maintenance: %v", err)
	}
	s.endSource("after")
}

func TestRunMaintenanceRunsAfterInFlightExits(t *testing.T) {
	prev := maintenanceExitWait
	maintenanceExitWait = 2 * time.Second
	defer func() { maintenanceExitWait = prev }()

	s := newCollectLifecycle()
	if err := s.beginSource("busy"); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		s.endSource("busy")
	}()
	called := false
	if err := s.runMaintenance("测试维护，采集让行", func() error {
		called = true
		if err := s.beginSource("late"); !errors.Is(err, errCollectDuringMaintenance) {
			t.Errorf("new collect = %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("action was not executed")
	}
	if err := s.beginSource("after"); err != nil {
		t.Fatalf("collect should resume after maintenance: %v", err)
	}
	s.endSource("after")
}

func TestRunMaintenanceFailsWhenCollectDoesNotExit(t *testing.T) {
	prev := maintenanceExitWait
	maintenanceExitWait = 40 * time.Millisecond
	defer func() { maintenanceExitWait = prev }()

	s := newCollectLifecycle()
	if err := s.beginSource("stuck"); err != nil {
		t.Fatal(err)
	}
	defer s.endSource("stuck")
	called := false
	err := s.runMaintenance("测试维护，采集让行", func() error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	if err := s.beginSource("after"); err != nil {
		t.Fatalf("gate should release after failure: %v", err)
	}
	s.endSource("after")
}

func TestCollectLifecycle_BeginDrainTimeoutDoesNotReleaseOwner(t *testing.T) {
	s := newCollectLifecycle()
	if err := s.beginMaintenance(); err != nil {
		t.Fatal(err)
	}
	if err := s.beginDrain(40 * time.Millisecond); err == nil || !errors.Is(err, errDrainTimeout) {
		t.Fatalf("second drain should time out, got %v", err)
	}
	if err := s.beginSource("during"); err == nil {
		t.Fatal("owner gate must still reject collect after the other drain times out")
	}
	s.endDrain()
	if err := s.beginSource("after"); err != nil {
		t.Fatalf("collect should resume after maintenance: %v", err)
	}
	s.endSource("after")
}
