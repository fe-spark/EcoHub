package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/repository"
)

func TestBatchScrapeBannersStream_EmptyBanners(t *testing.T) {
	// 保存旧轮播
	oldBanners := repository.GetBanners()
	defer func() {
		_ = repository.SaveBanners(oldBanners)
	}()

	_ = repository.SaveBanners(model.Banners{})

	var events []BatchScrapeProgressEvent
	var mu sync.Mutex
	err := ManageSvc.BatchScrapeBannersStream(context.Background(), func(evt BatchScrapeProgressEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, evt)
	})

	if err != nil {
		t.Fatalf("expected nil error on empty banners, got: %v", err)
	}

	if len(events) != 1 || events[0].Event != "done" {
		t.Fatalf("expected 1 done event, got %+v", events)
	}
}

func TestBatchScrapeBannersStream_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 预先取消

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_ = ManageSvc.BatchScrapeBannersStream(ctx, func(evt BatchScrapeProgressEvent) {})
	}()

	select {
	case <-doneCh:
		// 正常退出，未发生死锁
	case <-time.After(2 * time.Second):
		t.Fatalf("BatchScrapeBannersStream deadlocked on canceled context")
	}
}

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
