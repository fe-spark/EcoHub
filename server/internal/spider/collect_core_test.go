package spider

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/spider/progress"
)

func TestFilterEnabledSources(t *testing.T) {
	sources := []model.FilmSource{
		{Id: "s1", Name: "Source 1", State: true},
		{Id: "s2", Name: "Source 2", State: false},
		{Id: "s3", Name: "Source 3", State: true},
	}

	enabled := filterEnabledSources(sources)
	if len(enabled) != 2 {
		t.Fatalf("expected 2 enabled sources, got %d", len(enabled))
	}
	if enabled[0].Id != "s1" || enabled[1].Id != "s3" {
		t.Fatalf("unexpected filtered sources: %+v", enabled)
	}

	// Test empty slice
	if len(filterEnabledSources(nil)) != 0 {
		t.Fatal("expected empty slice when input is nil")
	}
}

func TestResolveCategoryHintTarget(t *testing.T) {
	tests := []struct {
		classCount int
		expected   int
	}{
		{classCount: 0, expected: 0},
		{classCount: 1, expected: 1},
		{classCount: 10, expected: 7},  // 10 * 0.7 = 7
		{classCount: 20, expected: 14}, // 20 * 0.7 = 14
	}

	for _, tt := range tests {
		got := resolveCategoryHintTarget(tt.classCount)
		if got != tt.expected {
			t.Errorf("resolveCategoryHintTarget(%d) = %d; want %d", tt.classCount, got, tt.expected)
		}
	}
}

func TestNeedsCategoryParentInference(t *testing.T) {
	// 1) Empty classes -> false
	if needsCategoryParentInference(nil) {
		t.Error("expected false for empty classes")
	}

	// 2) Any item has Pid > 0 -> false (already has parent info)
	classesWithPid := []model.FilmClass{
		{ID: 1, Name: "电影", Pid: 0},
		{ID: 2, Name: "动作片", Pid: 1},
	}
	if needsCategoryParentInference(classesWithPid) {
		t.Error("expected false when at least one class has Pid > 0")
	}

	// 3) All items Pid == 0 -> true (needs parent inference)
	classesWithoutPid := []model.FilmClass{
		{ID: 1, Name: "电影", Pid: 0},
		{ID: 2, Name: "动作片", Pid: 0},
	}
	if !needsCategoryParentInference(classesWithoutPid) {
		t.Error("expected true when all classes have Pid == 0")
	}
}

func TestNormalizeAffectedMIDs(t *testing.T) {
	input := []int64{10, -1, 5, 0, 10, 20, 5, 3}
	expected := []int64{3, 5, 10, 20}

	result := normalizeAffectedMIDs(input)
	if !reflect.DeepEqual(result, expected) {
		t.Fatalf("normalizeAffectedMIDs(%v) = %v; want %v", input, result, expected)
	}

	// Test nil and empty slice
	if normalizeAffectedMIDs(nil) != nil {
		t.Error("expected nil for nil input")
	}
	if len(normalizeAffectedMIDs([]int64{-1, 0})) != 0 {
		t.Error("expected empty result for non-positive inputs")
	}
}

func TestShouldSkipCollectPublishOnError(t *testing.T) {
	masterSource := model.FilmSource{Grade: model.MasterCollect}
	slaveSource := model.FilmSource{Grade: model.SlaveCollect}

	// 主站全量采集 (h < 0) -> 跳过发布
	if !shouldSkipCollectPublishOnError(masterSource, -1) {
		t.Error("expected true for MasterCollect with h < 0")
	}

	// 主站增量采集 (h > 0) -> 不跳过
	if shouldSkipCollectPublishOnError(masterSource, 3) {
		t.Error("expected false for MasterCollect with h > 0")
	}

	// 附属站全量采集 (h < 0) -> 不跳过
	if shouldSkipCollectPublishOnError(slaveSource, -1) {
		t.Error("expected false for SlaveCollect with h < 0")
	}
}

func TestCollectLifecycleState_BeginAndEndSource(t *testing.T) {
	lc := newCollectLifecycle()
	const sourceID = "lc-test-source-1"

	// 1) Begin source -> success
	if err := lc.beginSource(sourceID); err != nil {
		t.Fatalf("unexpected beginSource error: %v", err)
	}

	// 2) Begin same source again -> expect error
	if err := lc.beginSource(sourceID); err == nil {
		t.Fatal("expected error when beginning active source again")
	}

	// 3) End source -> success
	lc.endSource(sourceID)

	// 4) Begin source again after end -> success
	if err := lc.beginSource(sourceID); err != nil {
		t.Fatalf("unexpected beginSource error after endSource: %v", err)
	}
	lc.endSource(sourceID)
}

func TestOccupyAndMarkCollectSources_RetryIsNewQueue(t *testing.T) {
	ik := model.FilmSource{Id: "retry-queue-ik", Name: "HD(IK)"}
	wrap := model.FilmSource{Id: "retry-queue-wrap", Name: "默认"}
	defer releaseCollectSources([]model.FilmSource{ik, wrap})

	batch := occupyAndMarkCollectSources([]model.FilmSource{ik, wrap}, "Batch-Collect")
	if len(batch) != 2 {
		t.Fatalf("batch occupy got %d, want 2", len(batch))
	}
	ikSnap, ok := progress.Snapshot(ik.Id)
	wrapSnap, wrapOK := progress.Snapshot(wrap.Id)
	if !ok || !wrapOK || ikSnap.QueueId == "" {
		t.Fatal("batch must tag both sources with queueId")
	}
	if wrapSnap.QueueId != ikSnap.QueueId {
		t.Fatalf("batch queueId mismatch ik=%q wrap=%q", ikSnap.QueueId, wrapSnap.QueueId)
	}
	batchQueue := ikSnap.QueueId

	releaseCollectSourceIDs(ik.Id)
	progress.Update(ik.Id, func(p *model.CollectProgress) {
		p.Status = progress.StatusFailed
	})

	retry := occupyAndMarkCollectSources([]model.FilmSource{ik}, "Single-Collect")
	if len(retry) != 1 {
		t.Fatalf("retry occupy got %d, want 1", len(retry))
	}
	retrySnap, ok := progress.Snapshot(ik.Id)
	if !ok {
		t.Fatal("expected retry progress")
	}
	if retrySnap.QueueId == "" || retrySnap.QueueId == batchQueue {
		t.Fatalf("IK retry must be a new queue, got %q (batch %q)", retrySnap.QueueId, batchQueue)
	}
	if retrySnap.Status != progress.StatusStarting {
		t.Fatalf("retry status = %q, want starting", retrySnap.Status)
	}
	wrapSnap, _ = progress.Snapshot(wrap.Id)
	if wrapSnap.QueueId != batchQueue {
		t.Fatalf("wrapping source must keep batch queueId %q, got %q", batchQueue, wrapSnap.QueueId)
	}
}

func TestBatchCollectErrorReleasesOccupyForRetry(t *testing.T) {
	id := "batch-fail-release-occupy"
	source := model.FilmSource{Id: id, Name: "HD(IK)"}
	if len(occupyAndMarkCollectSources([]model.FilmSource{source}, "Batch-Collect")) != 1 {
		t.Fatal("occupy failed")
	}
	defer releaseCollectSourceIDs(id)

	ver := uint64(0)
	if err := handleCollectWithStopVersion(id, -1, &ver, false, false, nil); err == nil {
		t.Fatal("expected missing source error")
	}
	if isOccupiedCollectSource(id) {
		t.Fatal("batch occupy must be released on collect failure so retry can start")
	}
	snap, ok := progress.Snapshot(id)
	if !ok || snap.Status != progress.StatusFailed {
		t.Fatalf("early fail must not leave starting, ok=%v status=%q", ok, snap.Status)
	}
	if progress.IsAlreadyQueuedOrRunning(id) {
		t.Fatal("failed leftover must not block retry")
	}

	if len(occupyAndMarkCollectSources([]model.FilmSource{source}, "Single-Collect")) != 1 {
		t.Fatal("retry should occupy after failed batch source released")
	}
}

func TestBatchCloseDoesNotDropRetryOccupy(t *testing.T) {
	ik := model.FilmSource{Id: "batch-close-retry-ik", Name: "HD(IK)"}
	wrap := model.FilmSource{Id: "batch-close-retry-wrap", Name: "默认"}
	claimed := occupyCollectSources([]model.FilmSource{ik, wrap}, "Batch-Collect")
	if len(claimed) != 2 {
		t.Fatalf("batch occupy got %d", len(claimed))
	}
	defer releaseCollectSources([]model.FilmSource{ik, wrap})

	batch := newCollectBatchContext(model.NotifyTriggerManual, "Batch-Collect", claimed, nil, time.Now())
	abortUnstartedCollect(ik.Id, batch)

	if len(occupyCollectSources([]model.FilmSource{ik}, "Single-Collect")) != 1 {
		t.Fatal("retry occupy failed")
	}
	batch.close()
	if !isOccupiedCollectSource(ik.Id) {
		t.Fatal("retry occupy must survive parent batch close")
	}
	if isOccupiedCollectSource(wrap.Id) {
		t.Fatal("original batch close should still release remaining occupy")
	}
}

func TestDispatchSkipStoppedReleasesOccupy(t *testing.T) {
	source := model.FilmSource{Id: "batch-skip-stopped-occupy", Name: "HD(IK)", Grade: 1}
	if len(occupyCollectSources([]model.FilmSource{source}, "Batch-Collect")) != 1 {
		t.Fatal("occupy failed")
	}
	defer releaseCollectSourceIDs(source.Id)

	progress.Ensure(source.Id, source.Name)
	progress.MarkStopped(source.Id)

	runSourcesGroupWithLimit([]model.FilmSource{source}, 24, "Batch-Collect", 0, 0, nil)
	if isOccupiedCollectSource(source.Id) {
		t.Fatal("stopped queued source must release occupy so retry can start")
	}
}

func TestHandlePreparedCollect_MissingSourceReleasesOccupy(t *testing.T) {
	id := "prepared-missing-source"
	occupied := occupyCollectSources([]model.FilmSource{{Id: id, Name: "gone"}}, "prepared")
	if len(occupied) != 1 {
		t.Fatalf("expected occupy, got %d", len(occupied))
	}
	defer releaseCollectSourceIDs(id)

	if err := HandlePreparedCollect(id, 24); err == nil {
		t.Fatal("expected missing source error")
	}
	if isOccupiedCollectSource(id) {
		t.Fatal("prepare occupy must be released when collect never created a batch")
	}
}

func TestStandaloneCloseDoesNotLeaveOccupyForSecondRelease(t *testing.T) {
	source := model.FilmSource{Id: "prepared-double-release", Name: "A"}
	if len(occupyCollectSources([]model.FilmSource{source}, "first")) != 1 {
		t.Fatal("first occupy failed")
	}
	batch := newCollectBatchContext(model.NotifyTriggerManual, "单站采集", []model.FilmSource{source}, nil, time.Now(), true)
	batch.close()
	if isOccupiedCollectSource(source.Id) {
		t.Fatal("close should release occupy")
	}

	if len(occupyCollectSources([]model.FilmSource{source}, "second")) != 1 {
		t.Fatal("second queue should occupy after first closed")
	}
	defer releaseCollectSourceIDs(source.Id)
	if !isOccupiedCollectSource(source.Id) {
		t.Fatal("second occupy should hold")
	}
}

func TestOccupyCollectSources_SkipAlreadyCollecting(t *testing.T) {
	sourceA := model.FilmSource{Id: "occupy-source-a", Name: "A"}
	sourceB := model.FilmSource{Id: "occupy-source-b", Name: "B"}
	sourceC := model.FilmSource{Id: "occupy-source-c", Name: "C"}
	defer releaseCollectSources([]model.FilmSource{sourceA, sourceB, sourceC})

	first := occupyCollectSources([]model.FilmSource{sourceA, sourceB}, "Queue-1")
	if len(first) != 2 {
		t.Fatalf("first queue should occupy A and B, got %d", len(first))
	}
	if !isOccupiedCollectSource(sourceA.Id) || !isOccupiedCollectSource(sourceB.Id) {
		t.Fatal("A and B should be in the collecting queue")
	}

	second := occupyCollectSources([]model.FilmSource{sourceA, sourceC}, "Queue-2")
	if len(second) != 1 || second[0].Id != sourceC.Id {
		t.Fatalf("second queue should skip A and occupy C, got %+v", second)
	}
	if !isOccupiedCollectSource(sourceA.Id) {
		t.Fatal("A should remain in the first collecting queue")
	}

	releaseCollectSources(first)
	if isOccupiedCollectSource(sourceA.Id) || isOccupiedCollectSource(sourceB.Id) {
		t.Fatal("released first queue should no longer occupy A/B")
	}
	if !isOccupiedCollectSource(sourceC.Id) {
		t.Fatal("C should still belong to the second queue")
	}
}

func TestOccupyCollectSources_ConcurrentSameSourceOnlyOneWins(t *testing.T) {
	source := model.FilmSource{Id: "occupy-source-race", Name: "Race"}
	defer releaseCollectSourceIDs(source.Id)

	const n = 16
	results := make(chan []model.FilmSource, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			results <- occupyCollectSources([]model.FilmSource{source}, "Race")
		}()
	}
	wg.Wait()
	close(results)

	won := 0
	for claimed := range results {
		if len(claimed) == 1 {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("expected exactly one occupier, got %d", won)
	}
}

func TestCollectBatchContext_Isolation(t *testing.T) {
	// 模拟两个独立批次：Batch A（全量采集）与 Batch B（定时任务）
	sourceA := model.FilmSource{Id: "source-a", Name: "Source A", Grade: model.SlaveCollect}
	sourceB := model.FilmSource{Id: "source-b", Name: "Source B", Grade: model.SlaveCollect}

	batchA := newCollectBatchContext(model.NotifyTriggerManual, "全量", []model.FilmSource{sourceA}, nil, time.Now(), true)
	batchB := newCollectBatchContext(model.NotifyTriggerCron, "定时", []model.FilmSource{sourceB}, nil, time.Now(), false)

	if !batchA.isStandalone {
		t.Errorf("expected batch A isStandalone=true")
	}
	if batchB.isStandalone {
		t.Errorf("expected batch B isStandalone=false")
	}

	// Batch A 产生 MIDs: 100, 200
	batchA.addAffectedMIDs(&sourceA, 24, []int64{100, 200})

	// Batch B 产生 MIDs: 300, 400
	batchB.addAffectedMIDs(&sourceB, 24, []int64{300, 400})

	// 验证两批次各自独立持有自身 MIDs，互不泄露
	batchA.mu.Lock()
	midsA := make([]int64, 0, len(batchA.affectedMIDs))
	for mid := range batchA.affectedMIDs {
		midsA = append(midsA, mid)
	}
	sort.Slice(midsA, func(i, j int) bool { return midsA[i] < midsA[j] })
	batchA.mu.Unlock()

	batchB.mu.Lock()
	midsB := make([]int64, 0, len(batchB.affectedMIDs))
	for mid := range batchB.affectedMIDs {
		midsB = append(midsB, mid)
	}
	sort.Slice(midsB, func(i, j int) bool { return midsB[i] < midsB[j] })
	batchB.mu.Unlock()

	expectedA := []int64{100, 200}
	expectedB := []int64{300, 400}

	if !reflect.DeepEqual(midsA, expectedA) {
		t.Errorf("batch A MIDs = %v; want %v", midsA, expectedA)
	}
	if !reflect.DeepEqual(midsB, expectedB) {
		t.Errorf("batch B MIDs = %v; want %v", midsB, expectedB)
	}
}

func TestAdaptiveRateLimiting_SmoothBackoff(t *testing.T) {
	const sourceID = "test_source_rl"
	ClearLimiter(sourceID)
	defer ClearLimiter(sourceID)

	source := &model.FilmSource{
		Id:       sourceID,
		Name:     "Test RL",
		Interval: 50,
	}

	// 1) 首次请求放行，并模拟上游返回 429 限流错误
	release1, err := waitSourceRequestTurn(context.Background(), source, "test-turn-1")
	if err != nil {
		t.Fatalf("unexpected wait turn error: %v", err)
	}
	release1(errors.New("HTTP 429 Too Many Requests"))

	gate := getSourceRequestGate(sourceID)
	gate.mu.Lock()
	hits := gate.rateLimitHits
	nextAllowed := gate.nextAllowedAt
	gate.mu.Unlock()

	if hits != 1 {
		t.Fatalf("expected rateLimitHits=1 after 429, got %d", hits)
	}
	if !nextAllowed.After(time.Now()) {
		t.Fatalf("expected nextAllowedAt to be in the future after 429 backoff")
	}

	// 2) 再次请求放行（模拟冷却后），并模拟请求成功，hits 应递减
	gate.mu.Lock()
	gate.nextAllowedAt = time.Now().Add(-100 * time.Millisecond) // 模拟冷却结束
	gate.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	release2, err := waitSourceRequestTurn(ctx, source, "test-turn-2")
	if err != nil {
		t.Fatalf("unexpected wait turn error: %v", err)
	}
	release2(nil) // 成功放行释放

	gate.mu.Lock()
	hitsAfterSuccess := gate.rateLimitHits
	gate.mu.Unlock()

	if hitsAfterSuccess != 0 {
		t.Fatalf("expected rateLimitHits=0 after success, got %d", hitsAfterSuccess)
	}
}
