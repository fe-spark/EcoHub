package spider

import (
	"context"
	"testing"
	"time"
)

func TestCollectWriteLaneSelectsLargestReadyQueueEachTurn(t *testing.T) {
	lane := newCollectWriteLane("test")
	ctx := context.Background()

	for page := 1; page <= collectWriteMaxPagesPerTurn; page++ {
		if err := lane.submit(ctx, collectWriteJob{sourceID: "a", sourceName: "a", page: page}); err != nil {
			t.Fatalf("submit a page %d: %v", page, err)
		}
	}
	for page := 1; page <= collectWriteMaxPagesPerTurn+12; page++ {
		if err := lane.submit(ctx, collectWriteJob{sourceID: "b", sourceName: "b", page: page}); err != nil {
			t.Fatalf("submit b page %d: %v", page, err)
		}
	}
	for page := 1; page <= collectWriteMaxPagesPerTurn+3; page++ {
		if err := lane.submit(ctx, collectWriteJob{sourceID: "c", sourceName: "c", page: page}); err != nil {
			t.Fatalf("submit c page %d: %v", page, err)
		}
	}

	batch, _ := lane.nextBatch()
	if len(batch) != collectWriteMaxPagesPerTurn {
		t.Fatalf("expected first batch size %d, got %d", collectWriteMaxPagesPerTurn, len(batch))
	}
	for _, job := range batch {
		if job.sourceID != "b" {
			t.Fatalf("expected largest ready queue b to write first, got source %s", job.sourceID)
		}
	}

	for page := 1; page <= 25; page++ {
		if err := lane.submit(ctx, collectWriteJob{sourceID: "a", sourceName: "a", page: collectWriteMaxPagesPerTurn + page}); err != nil {
			t.Fatalf("submit extra a page %d: %v", page, err)
		}
	}

	batch, _ = lane.nextBatch()
	if len(batch) != collectWriteMaxPagesPerTurn {
		t.Fatalf("expected second batch size %d, got %d", collectWriteMaxPagesPerTurn, len(batch))
	}
	for _, job := range batch {
		if job.sourceID != "a" {
			t.Fatalf("expected scheduler to recount and pick a, got source %s", job.sourceID)
		}
	}
}

func TestCollectWriteLaneDoesNotWriteBeforeBatchSize(t *testing.T) {
	lane := newCollectWriteLane("test")
	ctx := context.Background()

	for page := 1; page < collectWriteMaxPagesPerTurn; page++ {
		if err := lane.submit(ctx, collectWriteJob{sourceID: "source", sourceName: "source", page: page}); err != nil {
			t.Fatalf("submit page %d: %v", page, err)
		}
	}

	selected := lane.selectQueueLocked()
	if selected != nil {
		t.Fatalf("expected no ready queue before %d pages, got %s", collectWriteMaxPagesPerTurn, selected.sourceID)
	}
}

func TestCollectWriteLaneFlushesTailWhenSourceFinished(t *testing.T) {
	lane := newCollectWriteLane("test")
	ctx := context.Background()

	for page := 1; page <= 5; page++ {
		if err := lane.submit(ctx, collectWriteJob{sourceID: "source", sourceName: "source", page: page}); err != nil {
			t.Fatalf("submit page %d: %v", page, err)
		}
	}
	lane.finishSource("source")

	batch, meta := lane.nextBatch()
	if len(batch) != 5 {
		t.Fatalf("expected tail batch size 5, got %d", len(batch))
	}
	if !meta.tail {
		t.Fatal("expected finished short batch to be marked as tail")
	}
}

func TestCollectWriteLaneMarksFinalShortBatchAsTailAfterFullBatch(t *testing.T) {
	lane := newCollectWriteLane("test")
	ctx := context.Background()

	for page := 1; page <= collectWriteMaxPagesPerTurn+5; page++ {
		if err := lane.submit(ctx, collectWriteJob{sourceID: "source", sourceName: "source", page: page}); err != nil {
			t.Fatalf("submit page %d: %v", page, err)
		}
	}
	lane.finishSource("source")

	firstBatch, firstMeta := lane.nextBatch()
	if len(firstBatch) != collectWriteMaxPagesPerTurn {
		t.Fatalf("expected first batch size %d, got %d", collectWriteMaxPagesPerTurn, len(firstBatch))
	}
	if firstMeta.tail {
		t.Fatal("expected first full batch to not be marked as tail")
	}

	tailBatch, tailMeta := lane.nextBatch()
	if len(tailBatch) != 5 {
		t.Fatalf("expected final tail batch size 5, got %d", len(tailBatch))
	}
	if !tailMeta.tail {
		t.Fatal("expected final short batch after full batch to be marked as tail")
	}
}

func TestCollectWriteLaneLimitsBatchSize(t *testing.T) {
	lane := newCollectWriteLane("test")
	ctx := context.Background()

	for page := 1; page <= collectWriteMaxPagesPerTurn+5; page++ {
		if err := lane.submit(ctx, collectWriteJob{sourceID: "source", sourceName: "source", page: page}); err != nil {
			t.Fatalf("submit page %d: %v", page, err)
		}
	}

	batch, _ := lane.nextBatch()
	if len(batch) != collectWriteMaxPagesPerTurn {
		t.Fatalf("expected batch size %d, got %d", collectWriteMaxPagesPerTurn, len(batch))
	}

	lane.mu.Lock()
	remaining := len(lane.queues["source"].pending)
	lane.mu.Unlock()
	if remaining != 5 {
		t.Fatalf("expected 5 pending jobs after first batch, got %d", remaining)
	}
}

func TestCollectWriteLaneSubmitWaitsForPendingCapacity(t *testing.T) {
	lane := newCollectWriteLane("test")
	ctx := context.Background()

	for page := 1; page <= collectWriteMaxPendingPagesPerSource; page++ {
		if err := lane.submit(ctx, collectWriteJob{sourceID: "source", sourceName: "source", page: page}); err != nil {
			t.Fatalf("submit page %d: %v", page, err)
		}
	}

	finished := make(chan error, 1)
	go func() {
		finished <- lane.submit(ctx, collectWriteJob{sourceID: "source", sourceName: "source", page: collectWriteMaxPendingPagesPerSource + 1})
	}()

	select {
	case err := <-finished:
		t.Fatalf("submit should wait for capacity, returned %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	lane.nextBatch()

	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("submit after capacity freed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("submit did not finish after capacity was freed")
	}
}

func TestCollectWriteLaneSubmitReturnsWhenContextCanceled(t *testing.T) {
	lane := newCollectWriteLane("test")
	ctx := context.Background()

	for page := 1; page <= collectWriteMaxPendingPagesPerSource; page++ {
		if err := lane.submit(ctx, collectWriteJob{sourceID: "source", sourceName: "source", page: page}); err != nil {
			t.Fatalf("submit page %d: %v", page, err)
		}
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		finished <- lane.submit(cancelCtx, collectWriteJob{sourceID: "source", sourceName: "source", page: collectWriteMaxPendingPagesPerSource + 1})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("expected canceled context error, got nil")
		}
	case <-time.After(time.Second):
		t.Fatal("submit did not return after context cancellation")
	}
}

func TestCollectWriteLaneSubmitRejectsAlreadyCanceledContext(t *testing.T) {
	lane := newCollectWriteLane("test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := lane.submit(ctx, collectWriteJob{sourceID: "source", sourceName: "source", page: 1})
	if err == nil {
		t.Fatal("expected canceled context error, got nil")
	}

	lane.mu.Lock()
	queueCount := len(lane.queues)
	lane.mu.Unlock()
	if queueCount != 0 {
		t.Fatalf("expected no queued jobs after canceled submit, got %d queues", queueCount)
	}
}
