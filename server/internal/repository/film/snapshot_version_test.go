package film

import (
	"context"
	"testing"
	"time"
)

func TestActiveReadModelVersionFallsBackWhenEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		readModel       *FilmReadModel
		snapshotVersion string
		want            string
	}{
		{
			name:            "nil read model uses snapshot version",
			readModel:       nil,
			snapshotVersion: "v-snap",
			want:            "v-snap",
		},
		{
			name:            "empty version pointer uses snapshot version",
			readModel:       &FilmReadModel{Version: ""},
			snapshotVersion: "v-snap",
			want:            "v-snap",
		},
		{
			name:            "whitespace version uses snapshot version",
			readModel:       &FilmReadModel{Version: "  "},
			snapshotVersion: "v-snap",
			want:            "v-snap",
		},
		{
			name:            "loaded version wins",
			readModel:       &FilmReadModel{Version: " v-mem "},
			snapshotVersion: "v-snap",
			want:            "v-mem",
		},
		{
			name:            "empty snapshot when read model empty",
			readModel:       &FilmReadModel{Version: ""},
			snapshotVersion: "",
			want:            "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := activeReadModelVersion(tt.readModel, tt.snapshotVersion)
			if got != tt.want {
				t.Fatalf("activeReadModelVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClearActiveFilmReadModelLeavesEmptyNonNilPointer(t *testing.T) {
	origRm := GetActiveFilmReadModel()
	origIdx := activeFilmSearchIndex.Load()
	defer func() {
		if origRm != nil {
			activeFilmReadModel.Store(origRm)
		}
		if origIdx != nil {
			activeFilmSearchIndex.Store(origIdx)
		}
	}()

	ClearActiveFilmReadModel()
	rm := GetActiveFilmReadModel()
	if rm == nil {
		t.Fatal("ClearActiveFilmReadModel stored nil; GetActiveReadModelVersion nil-check would pass and hide empty Version")
	}
	if rm.Version != "" {
		t.Fatalf("ClearActiveFilmReadModel Version = %q, want empty", rm.Version)
	}
}

func TestInvalidateActiveFilmSearchIndexPreservesReadModelVersion(t *testing.T) {
	origRm := GetActiveFilmReadModel()
	origIdx := activeFilmSearchIndex.Load()
	defer func() {
		if origRm != nil {
			activeFilmReadModel.Store(origRm)
		}
		if origIdx != nil {
			activeFilmSearchIndex.Store(origIdx)
		}
	}()

	const testVer = "v-test-preserve-123"
	_ = LoadActiveFilmReadModel(testVer)

	InvalidateActiveFilmSearchIndex(testVer)

	rm := GetActiveFilmReadModel()
	if rm == nil || rm.Version != testVer {
		t.Fatalf("InvalidateActiveFilmSearchIndex cleared version, got %+v, want version=%q", rm, testVer)
	}

	searchIdx := activeFilmSearchIndex.Load()
	if searchIdx != nil && searchIdx.Version != "" && len(searchIdx.Items) > 0 {
		t.Fatalf("InvalidateActiveFilmSearchIndex failed to mark memory search index stale, got %+v", searchIdx)
	}
}

func TestStartClusterSnapshotWatcherIdempotent(t *testing.T) {
	before := clusterWatcherRunCount.Load()
	StartClusterSnapshotWatcher()
	StartClusterSnapshotWatcher()
	StartClusterSnapshotWatcher()
	if got := clusterWatcherRunCount.Load(); got != before+1 {
		t.Fatalf("watcher goroutine started %d times, want once (before=%d)", got, before)
	}
	StopClusterSnapshotWatcher()
}

func TestRunClusterSnapshotWatcherStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runClusterSnapshotWatcher(ctx, new(clusterSnapshotSyncState))
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runClusterSnapshotWatcher did not stop after context cancel")
	}
}

func TestResolveActiveReadModelVersion(t *testing.T) {
	origRm := GetActiveFilmReadModel()
	origIdx := activeFilmSearchIndex.Load()
	defer func() {
		if origRm != nil {
			activeFilmReadModel.Store(origRm)
		}
		if origIdx != nil {
			activeFilmSearchIndex.Store(origIdx)
		}
	}()

	t.Run("in sync uses memory version", func(t *testing.T) {
		if got := resolveActiveReadModelVersion("v1", "v1"); got != "v1" {
			t.Fatalf("got %q, want v1", got)
		}
	})
	t.Run("no active snapshot falls back to memory", func(t *testing.T) {
		if got := resolveActiveReadModelVersion("v1", ""); got != "v1" {
			t.Fatalf("got %q, want v1", got)
		}
	})
	t.Run("no memory version falls back to snapshot", func(t *testing.T) {
		if got := resolveActiveReadModelVersion("", "v2"); got != "v2" {
			t.Fatalf("got %q, want v2", got)
		}
	})
	t.Run("both empty returns empty", func(t *testing.T) {
		if got := resolveActiveReadModelVersion("", ""); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
	t.Run("version mismatch with stale index keeps memory version", func(t *testing.T) {
		activeFilmSearchIndex.Store(&filmSearchMemoryIndex{Version: "v1"})
		if got := resolveActiveReadModelVersion("v1", "v2"); got != "v1" {
			t.Fatalf("got %q, want v1 (index not ready)", got)
		}
	})
	t.Run("version mismatch with ready index switches to snapshot", func(t *testing.T) {
		activeFilmSearchIndex.Store(&filmSearchMemoryIndex{Version: "v2", Items: make([]filmSearchMemoryItem, 1)})
		if got := resolveActiveReadModelVersion("v1", "v2"); got != "v2" {
			t.Fatalf("got %q, want v2 (index ready)", got)
		}
	})
}
