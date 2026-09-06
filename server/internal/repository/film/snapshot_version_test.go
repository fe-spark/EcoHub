package film

import (
	"testing"
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
	defer func() {
		if origRm != nil {
			activeFilmReadModel.Store(origRm)
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
	defer func() {
		if origRm != nil {
			activeFilmReadModel.Store(origRm)
		}
	}()

	const testVer = "v-test-preserve-123"
	_ = LoadActiveFilmReadModel(testVer)

	InvalidateActiveFilmSearchIndex(testVer)

	rm := GetActiveFilmReadModel()
	if rm == nil || rm.Version != testVer {
		t.Fatalf("InvalidateActiveFilmSearchIndex cleared version, got %+v, want version=%q", rm, testVer)
	}
}

func TestResolveActiveReadModelVersion(t *testing.T) {
	origRm := GetActiveFilmReadModel()
	defer func() {
		if origRm != nil {
			activeFilmReadModel.Store(origRm)
		}
	}()

	t.Run("in sync uses snapshot version", func(t *testing.T) {
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
	t.Run("version mismatch switches to snapshot", func(t *testing.T) {
		if got := resolveActiveReadModelVersion("v1", "v2"); got != "v2" {
			t.Fatalf("got %q, want v2", got)
		}
	})
}
