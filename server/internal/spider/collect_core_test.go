package spider

import (
	"reflect"
	"testing"

	"server/internal/model"
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
		{classCount: 10, expected: 7}, // 10 * 0.7 = 7
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

func TestCollectLifecycleState_AffectedMIDsDrain(t *testing.T) {
	lc := newCollectLifecycle()

	lc.addAffectedMIDs([]int64{100, 200, 100, -5})
	lc.addMasterAffectedMIDs([]int64{300, 200})

	lc.mu.Lock()
	affected := lc.drainAffectedMIDsLocked()
	master := lc.drainMasterAffectedMIDsLocked()
	lc.mu.Unlock()

	expectedAffected := []int64{100, 200, 300}
	expectedMaster := []int64{200, 300}

	if !reflect.DeepEqual(affected, expectedAffected) {
		t.Errorf("affected MIDs = %v; want %v", affected, expectedAffected)
	}
	if !reflect.DeepEqual(master, expectedMaster) {
		t.Errorf("master MIDs = %v; want %v", master, expectedMaster)
	}
}
