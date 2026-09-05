package spider

import (
	"testing"
	"time"
)

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
