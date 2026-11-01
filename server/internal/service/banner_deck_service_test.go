package service

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestOneClickGenerateDeckStream_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 预先取消

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_ = ManageSvc.OneClickGenerateDeckStream(ctx, OneClickGenerateReq{
			Quotas: []CategoryQuota{{CategoryID: 1, Count: 2}},
		}, func(evt OneClickProgressEvent) {})
	}()

	select {
	case <-doneCh:
		// 正常退出，未发生死锁
	case <-time.After(2 * time.Second):
		t.Fatalf("OneClickGenerateDeckStream deadlocked on canceled context")
	}
}

func TestOneClickGenerateDeckStream_EventFlow(t *testing.T) {
	var events []OneClickProgressEvent
	var mu sync.Mutex
	_ = ManageSvc.OneClickGenerateDeckStream(context.Background(), OneClickGenerateReq{
		Quotas: []CategoryQuota{{CategoryID: 1, Count: 2}},
	}, func(evt OneClickProgressEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, evt)
	})

	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 {
		t.Fatalf("expected at least one event (start or error)")
	}
	if events[0].Event != "start" {
		t.Fatalf("expected first event to be start, got: %s", events[0].Event)
	}
}
