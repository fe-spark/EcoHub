package spider

import (
	"context"
	"testing"
	"time"

	"server/internal/model"
)

func TestShouldCatchUpCollectHoursOnlyCronIncremental(t *testing.T) {
	if !shouldCatchUpCollectHours(model.NotifyTriggerCron, 3) {
		t.Fatal("定时增量应补窗")
	}
	if shouldCatchUpCollectHours(model.NotifyTriggerManual, 3) {
		t.Fatal("手动采集不应补窗")
	}
	if shouldCatchUpCollectHours(model.NotifyTriggerCron, -1) {
		t.Fatal("全量不应补窗")
	}
	if shouldCatchUpCollectHours("", 24) {
		t.Fatal("无 trigger 视为手动，不应补窗")
	}
}

func TestResolveCollectHoursKeepsConfiguredWhenRecentSuccess(t *testing.T) {
	now := time.Date(2026, 8, 24, 20, 0, 0, 0, time.Local)
	last := now.Add(-30 * time.Minute)
	got := resolveCollectHours(3, &last, now)
	if got != 3 {
		t.Fatalf("want 3, got %d", got)
	}
}

func TestResolveCollectHoursExpandsAfterOutage(t *testing.T) {
	now := time.Date(2026, 8, 24, 20, 0, 0, 0, time.Local)
	last := now.Add(-12 * time.Hour)
	got := resolveCollectHours(3, &last, now)
	if got != 13 {
		t.Fatalf("want 13, got %d", got)
	}
}

func TestResolveCollectHoursCapsAtMax(t *testing.T) {
	now := time.Date(2026, 8, 24, 20, 0, 0, 0, time.Local)
	last := now.Add(-400 * time.Hour)
	got := resolveCollectHours(3, &last, now)
	if got != collectCatchUpMaxHours {
		t.Fatalf("want %d, got %d", collectCatchUpMaxHours, got)
	}
}

func TestResolveCollectHoursIgnoresFullCollectAndZero(t *testing.T) {
	now := time.Now()
	last := now.Add(-12 * time.Hour)
	if got := resolveCollectHours(-1, &last, now); got != -1 {
		t.Fatalf("full collect should stay -1, got %d", got)
	}
	if got := resolveCollectHours(0, &last, now); got != 0 {
		t.Fatalf("zero should stay 0, got %d", got)
	}
}

func TestResolveCollectHoursNilLastKeepsRequested(t *testing.T) {
	if got := resolveCollectHours(3, nil, time.Now()); got != 3 {
		t.Fatalf("want 3, got %d", got)
	}
}

func TestShouldNoteCronCollectSuccess(t *testing.T) {
	if !shouldNoteCronCollectSuccess(nil, context.Background(), "missing") {
		t.Fatal("无错误且未停止应记成功")
	}
	if shouldNoteCronCollectSuccess(context.Canceled, context.Background(), "missing") {
		t.Fatal("有错误不应记成功")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if shouldNoteCronCollectSuccess(nil, ctx, "missing") {
		t.Fatal("ctx 已取消不应记成功")
	}
}
