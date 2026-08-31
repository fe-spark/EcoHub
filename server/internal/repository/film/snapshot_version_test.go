package film

import "testing"

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
	ClearActiveFilmReadModel()
	rm := GetActiveFilmReadModel()
	if rm == nil {
		t.Fatal("ClearActiveFilmReadModel stored nil; GetActiveReadModelVersion nil-check would pass and hide empty Version")
	}
	if rm.Version != "" {
		t.Fatalf("ClearActiveFilmReadModel Version = %q, want empty", rm.Version)
	}
}
