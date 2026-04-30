package spider

import (
	"context"
	"testing"
	"time"

	"server/internal/model"
)

func submitCollectWriteJobs(t *testing.T, lane *collectWriteLane, sourceID string, count int) {
	t.Helper()
	for page := 1; page <= count; page++ {
		if err := lane.submit(context.Background(), collectWriteJob{sourceID: sourceID, sourceName: sourceID, page: page}); err != nil {
			t.Fatalf("submit %s page %d: %v", sourceID, page, err)
		}
	}
}

func assertCollectWriteJobsSource(t *testing.T, jobs []collectWriteJob, sourceID string) {
	t.Helper()
	for _, job := range jobs {
		if job.sourceID != sourceID {
			t.Fatalf("expected source %s, got %s", sourceID, job.sourceID)
		}
	}
}

func countCollectWriteJobsBySource(jobs []collectWriteJob) map[string]int {
	counts := make(map[string]int)
	for _, job := range jobs {
		counts[job.sourceID]++
	}
	return counts
}

func TestCollectWriteLanePicksPendingBeforeSourceFinished(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "source", 1)

	selected := lane.selectQueueLocked()
	if selected == nil {
		t.Fatal("expected pending source to be ready immediately")
	}
	if selected.sourceID != "source" {
		t.Fatalf("expected source to be selected, got %s", selected.sourceID)
	}
}

func TestCollectWriteLaneSelectsPendingBeforeSourceFinished(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "source", 1)

	jobs, meta, finish := lane.nextJobs()
	defer finish()
	if len(jobs) != 1 {
		t.Fatalf("expected pending count 1, got %d", len(jobs))
	}
	if meta.tail {
		t.Fatal("expected write before source finished to be non-tail")
	}
	assertCollectWriteJobsSource(t, jobs, "source")

	lane.mu.Lock()
	queue, exists := lane.queues["source"]
	lane.mu.Unlock()
	if !exists {
		t.Fatal("expected unfinished source queue to remain after write pick")
	}
	if len(queue.pending) != 0 {
		t.Fatalf("expected pending jobs to be cleared after write pick, got %d", len(queue.pending))
	}
}

func TestCollectWriteLaneFollowupPendingReadyAfterFirstWrite(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "source", 1)

	_, _, firstFinish := lane.nextJobs()
	firstFinish()
	submitCollectWriteJobs(t, lane, "source", 1)

	selected := lane.selectQueueLocked()
	if selected == nil {
		t.Fatal("expected followup batch to be ready immediately")
	}
	if selected.sourceID != "source" {
		t.Fatalf("expected source to be selected, got %s", selected.sourceID)
	}
}

func TestCollectWriteLaneFinishedFollowupTailWritesPending(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "source", 1)

	_, _, firstFinish := lane.nextJobs()
	firstFinish()
	submitCollectWriteJobs(t, lane, "source", 3)
	lane.finishSource("source")

	jobs, meta, finish := lane.nextJobs()
	defer finish()
	if len(jobs) != 3 {
		t.Fatalf("expected followup tail count 3, got %d", len(jobs))
	}
	if !meta.tail {
		t.Fatal("expected finished followup batch to be marked as tail")
	}
	assertCollectWriteJobsSource(t, jobs, "source")
}

func TestCollectWriteLaneFinishedSmallSourceWritesTail(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "source", 1)
	lane.finishSource("source")

	jobs, meta, finish := lane.nextJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected small source tail count 1, got %d", len(jobs))
	}
	if !meta.tail {
		t.Fatal("expected finished small source write to be marked as tail")
	}
	assertCollectWriteJobsSource(t, jobs, "source")
	finish()

	lane.mu.Lock()
	_, exists := lane.queues["source"]
	lane.mu.Unlock()
	if exists {
		t.Fatal("expected finished source queue to be removed after tail write")
	}
}

func TestCollectWriteLaneManualSingleSourceTailWritesAfterFinish(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "manual", 1)

	selected := lane.selectQueueLocked()
	if selected == nil {
		t.Fatal("expected manual single source to be ready before finish")
	}
	if selected.sourceID != "manual" {
		t.Fatalf("expected manual source to be selected, got %s", selected.sourceID)
	}

	lane.finishSource("manual")
	jobs, meta, finish := lane.nextJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected one tail job, got %d", len(jobs))
	}
	if !meta.tail {
		t.Fatal("expected manual single source tail to be marked as tail")
	}
	assertCollectWriteJobsSource(t, jobs, "manual")
	finish()

	lane.mu.Lock()
	_, exists := lane.queues["manual"]
	lane.mu.Unlock()
	if exists {
		t.Fatal("expected manual single source queue to be removed after tail write")
	}
}

func TestCollectWriteLaneFinishRemovesEmptyQueue(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "source", 1)

	jobs, _, finish := lane.nextJobs()
	finish()
	if len(jobs) != 1 {
		t.Fatalf("expected pending count 1, got %d", len(jobs))
	}
	lane.finishSource("source")

	lane.mu.Lock()
	_, exists := lane.queues["source"]
	lane.mu.Unlock()
	if exists {
		t.Fatal("expected empty queue to be removed when source finishes")
	}
}

func TestCollectWriteLaneTakesAllReadyQueuesInOneBatch(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "a", 1)
	submitCollectWriteJobs(t, lane, "b", 13)
	submitCollectWriteJobs(t, lane, "c", 9)

	jobs, meta, finish := lane.nextJobs()
	if len(jobs) != 23 {
		t.Fatalf("expected all ready pending count 23, got %d", len(jobs))
	}
	counts := countCollectWriteJobsBySource(jobs)
	if counts["a"] != 1 || counts["b"] != 13 || counts["c"] != 9 {
		t.Fatalf("expected a/b/c jobs 1/13/9, got %#v", counts)
	}
	if meta.tail {
		t.Fatal("expected write before source finished to be non-tail")
	}

	lane.mu.Lock()
	queueCount := len(lane.queues)
	for sourceID, queue := range lane.queues {
		if len(queue.pending) != 0 {
			t.Fatalf("expected source %s pending to be cleared after write pick, got %d", sourceID, len(queue.pending))
		}
	}
	lane.mu.Unlock()
	if queueCount != 3 {
		t.Fatalf("expected unfinished source queues to remain after write pick, got %d", queueCount)
	}

	finish()
}

func TestCollectWriteLaneWaitsForAllActiveSourcesBeforeBatch(t *testing.T) {
	lane := newCollectWriteLane("test")
	lane.beginSources([]model.FilmSource{{Id: "a"}, {Id: "b"}, {Id: "c"}})
	defer lane.endSources([]model.FilmSource{{Id: "a"}, {Id: "b"}, {Id: "c"}})

	submitCollectWriteJobs(t, lane, "a", 1)
	lane.mu.Lock()
	selected := lane.selectQueuesLocked()
	lane.mu.Unlock()
	if len(selected) != 0 {
		t.Fatalf("expected active batch to wait for all sources, got %d selected", len(selected))
	}

	submitCollectWriteJobs(t, lane, "b", 1)
	lane.mu.Lock()
	selected = lane.selectQueuesLocked()
	lane.mu.Unlock()
	if len(selected) != 0 {
		t.Fatalf("expected active batch to wait for missing source, got %d selected", len(selected))
	}

	submitCollectWriteJobs(t, lane, "c", 1)
	jobs, _, finish := lane.nextJobs()
	defer finish()
	counts := countCollectWriteJobsBySource(jobs)
	if counts["a"] != 1 || counts["b"] != 1 || counts["c"] != 1 {
		t.Fatalf("expected all active sources in one batch, got %#v", counts)
	}
}

func TestCollectWriteLaneActiveSourceFinishAllowsPartialBatch(t *testing.T) {
	lane := newCollectWriteLane("test")
	lane.beginSources([]model.FilmSource{{Id: "a"}, {Id: "b"}, {Id: "c"}})
	defer lane.endSources([]model.FilmSource{{Id: "a"}, {Id: "b"}, {Id: "c"}})

	submitCollectWriteJobs(t, lane, "a", 1)
	submitCollectWriteJobs(t, lane, "b", 1)
	lane.finishSource("c")

	jobs, _, finish := lane.nextJobs()
	defer finish()
	counts := countCollectWriteJobsBySource(jobs)
	if counts["a"] != 1 || counts["b"] != 1 || counts["c"] != 0 {
		t.Fatalf("expected pending active sources after c finished, got %#v", counts)
	}
}

func TestCollectWriteLaneSkippedActiveSourcesDoNotBlockBatch(t *testing.T) {
	lane := newCollectWriteLane("test")
	lane.beginSources([]model.FilmSource{{Id: "a"}, {Id: "b"}, {Id: "c"}})
	defer lane.endSources([]model.FilmSource{{Id: "a"}, {Id: "b"}, {Id: "c"}})

	submitCollectWriteJobs(t, lane, "a", 1)
	lane.finishSource("b")
	lane.finishSource("c")

	jobs, _, finish := lane.nextJobs()
	defer finish()
	if len(jobs) != 1 {
		t.Fatalf("expected only submitted active source to write, got %d jobs", len(jobs))
	}
	assertCollectWriteJobsSource(t, jobs, "a")
}

func TestCollectWriteLaneRechecksAllReadyAfterBatchFinish(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "a", 1)
	submitCollectWriteJobs(t, lane, "b", 1)
	submitCollectWriteJobs(t, lane, "c", 1)

	firstJobs, _, firstFinish := lane.nextJobs()
	if len(firstJobs) != 3 {
		t.Fatalf("expected first batch to include all initial sources, got %d", len(firstJobs))
	}
	firstFinish()
	submitCollectWriteJobs(t, lane, "a", 2)
	submitCollectWriteJobs(t, lane, "b", 1)
	submitCollectWriteJobs(t, lane, "c", 3)

	secondJobs, _, secondFinish := lane.nextJobs()
	defer secondFinish()
	if len(secondJobs) != 6 {
		t.Fatalf("expected second batch pending count 6, got %d", len(secondJobs))
	}
	counts := countCollectWriteJobsBySource(secondJobs)
	if counts["a"] != 2 || counts["b"] != 1 || counts["c"] != 3 {
		t.Fatalf("expected all followup pending to be ready, got %#v", counts)
	}
}

func TestCollectWriteLaneFinishedFollowupTailIncludedWithReadyBatch(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "a", 1)
	submitCollectWriteJobs(t, lane, "b", 1)

	_, _, firstFinish := lane.nextJobs()
	firstFinish()
	submitCollectWriteJobs(t, lane, "a", 2)
	submitCollectWriteJobs(t, lane, "b", 1)
	lane.finishSource("b")

	jobs, meta, finish := lane.nextJobs()
	defer finish()
	if len(jobs) != 3 {
		t.Fatalf("expected active pending plus finished tail count 3, got %d", len(jobs))
	}
	if meta.tail {
		t.Fatal("expected mixed active and tail batch to be non-tail")
	}
	counts := countCollectWriteJobsBySource(jobs)
	if counts["a"] != 2 || counts["b"] != 1 {
		t.Fatalf("expected a ready and b tail jobs, got %#v", counts)
	}
}

func TestCollectWriteLaneDoesNotPickSameSourceWhileWriting(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "a", 1)

	firstJobs, _, firstFinish := lane.nextJobs()
	if len(firstJobs) != 1 {
		t.Fatalf("expected first pending count 1, got %d", len(firstJobs))
	}
	submitCollectWriteJobs(t, lane, "a", 1)

	lane.mu.Lock()
	selected := lane.selectQueueLocked()
	lane.mu.Unlock()
	if selected != nil {
		t.Fatalf("expected no ready queue for same source while writing, got %s", selected.sourceID)
	}

	firstFinish()

	lane.mu.Lock()
	selected = lane.selectQueueLocked()
	lane.mu.Unlock()
	if selected == nil {
		t.Fatal("expected followup same source to be ready after current batch finishes")
	}
	secondJobs, _, secondFinish := lane.nextJobs()
	defer secondFinish()
	if len(secondJobs) != 1 {
		t.Fatalf("expected second pending count 1, got %d", len(secondJobs))
	}
	assertCollectWriteJobsSource(t, secondJobs, "a")
}

func TestCollectWriteLaneWaitsForBatchFinishBeforePickingNextSource(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "a", 1)
	submitCollectWriteJobs(t, lane, "b", 1)

	firstJobs, _, firstFinish := lane.nextJobs()
	if len(firstJobs) != 2 {
		t.Fatalf("expected first batch to include both ready sources, got %d", len(firstJobs))
	}

	lane.mu.Lock()
	selected := lane.selectQueueLocked()
	lane.mu.Unlock()
	if selected != nil {
		t.Fatalf("expected no next source before current batch finishes, got %s", selected.sourceID)
	}

	firstFinish()
	submitCollectWriteJobs(t, lane, "c", 1)
	secondJobs, _, secondFinish := lane.nextJobs()
	defer secondFinish()
	if len(secondJobs) != 1 {
		t.Fatalf("expected next batch pending count 1, got %d", len(secondJobs))
	}
	assertCollectWriteJobsSource(t, secondJobs, "c")
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

func TestCollectWriteLaneSubmitWaitsWhenSourcePendingFull(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "source", collectWriteMaxPendingPages)

	submitted := make(chan error, 1)
	go func() {
		submitted <- lane.submit(context.Background(), collectWriteJob{sourceID: "source", sourceName: "source", page: collectWriteMaxPendingPages + 1})
	}()

	select {
	case err := <-submitted:
		t.Fatalf("expected submit to wait while pending is full, got %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	jobs, _, finish := lane.nextJobs()
	if len(jobs) != collectWriteMaxPendingPages {
		t.Fatalf("expected full pending batch count %d, got %d", collectWriteMaxPendingPages, len(jobs))
	}

	select {
	case err := <-submitted:
		if err != nil {
			t.Fatalf("expected submit after batch pick to succeed, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected submit to resume after pending is picked")
	}
	finish()

	lane.mu.Lock()
	queue := lane.queues["source"]
	pending := len(queue.pending)
	lane.mu.Unlock()
	if pending != 1 {
		t.Fatalf("expected one pending job after resumed submit, got %d", pending)
	}
}

func TestCollectWriteLaneSubmitFullQueueReturnsOnContextCancel(t *testing.T) {
	lane := newCollectWriteLane("test")
	submitCollectWriteJobs(t, lane, "source", collectWriteMaxPendingPages)

	ctx, cancel := context.WithCancel(context.Background())
	submitted := make(chan error, 1)
	go func() {
		submitted <- lane.submit(ctx, collectWriteJob{sourceID: "source", sourceName: "source", page: collectWriteMaxPendingPages + 1})
	}()

	select {
	case err := <-submitted:
		t.Fatalf("expected submit to wait while pending is full, got %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-submitted:
		if err != context.Canceled {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected submit to return after context cancel")
	}

	lane.mu.Lock()
	pending := len(lane.queues["source"].pending)
	lane.mu.Unlock()
	if pending != collectWriteMaxPendingPages {
		t.Fatalf("expected canceled submit not to enqueue extra job, got %d", pending)
	}
}
