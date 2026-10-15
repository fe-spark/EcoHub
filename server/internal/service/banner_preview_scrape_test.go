package service

import (
	"context"
	"testing"
	"time"
)

func TestPreviewScrapeDeckStream_Empty(t *testing.T) {
	var events []PreviewScrapeEvent
	err := ManageSvc.PreviewScrapeDeckStream(context.Background(), nil, func(evt PreviewScrapeEvent) {
		events = append(events, evt)
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(events) != 1 || events[0].Event != "done" || events[0].SuccessCount != 0 {
		t.Fatalf("expected one done event, got %+v", events)
	}
}

func TestPreviewScrapeDeckStream_Canceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	doneCh := make(chan error, 1)
	sawDone := false
	go func() {
		doneCh <- ManageSvc.PreviewScrapeDeckStream(ctx, []int64{1}, func(evt PreviewScrapeEvent) {
			if evt.Event == "done" {
				sawDone = true
			}
		})
	}()

	select {
	case err := <-doneCh:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if sawDone {
			t.Fatal("canceled preview must not emit done")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PreviewScrapeDeckStream deadlocked on canceled context")
	}
}
