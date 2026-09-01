package film

import (
	"testing"
	"time"

	"server/internal/infra/db"

	"github.com/redis/go-redis/v9"
)

func TestDecideClusterSnapshotSync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		rmVersion, activeVer string
		curRev, lastRevision string
		want                 clusterSnapshotSyncAction
	}{
		{
			name:         "in sync does nothing",
			rmVersion:    "v1", activeVer: "v1",
			curRev: "5", lastRevision: "5",
			want: clusterSnapshotSyncNone,
		},
		{
			name:         "version change reloads",
			rmVersion:    "v1", activeVer: "v2",
			curRev: "5", lastRevision: "5",
			want: clusterSnapshotSyncReload,
		},
		{
			name:         "empty local read model with active snapshot reloads",
			rmVersion:    "", activeVer: "v2",
			curRev: "5", lastRevision: "5",
			want: clusterSnapshotSyncReload,
		},
		{
			name:         "version change wins over revision refresh",
			rmVersion:    "v1", activeVer: "v2",
			curRev: "6", lastRevision: "5",
			want: clusterSnapshotSyncReload,
		},
		{
			name:         "first start only records revision",
			rmVersion:    "v1", activeVer: "v1",
			curRev: "5", lastRevision: "",
			want: clusterSnapshotSyncNone,
		},
		{
			name:         "revision bump refreshes index",
			rmVersion:    "v1", activeVer: "v1",
			curRev: "6", lastRevision: "5",
			want: clusterSnapshotSyncRefreshIndex,
		},
		{
			name:         "empty revision does nothing",
			rmVersion:    "v1", activeVer: "v1",
			curRev: "", lastRevision: "5",
			want: clusterSnapshotSyncNone,
		},
		{
			name:         "version change without revision still reloads",
			rmVersion:    "v1", activeVer: "v2",
			curRev: "", lastRevision: "5",
			want: clusterSnapshotSyncReload,
		},
		{
			name:         "empty local read model without revision still reloads",
			rmVersion:    "", activeVer: "v2",
			curRev: "", lastRevision: "",
			want: clusterSnapshotSyncReload,
		},
		{
			name:         "revision without active snapshot does nothing",
			rmVersion:    "", activeVer: "",
			curRev: "6", lastRevision: "5",
			want: clusterSnapshotSyncNone,
		},
		{
			name:         "empty active version does not reload",
			rmVersion:    "v1", activeVer: "",
			curRev: "5", lastRevision: "5",
			want: clusterSnapshotSyncNone,
		},
		{
			name:         "empty revisions baseline not seeded does nothing",
			rmVersion:    "v1", activeVer: "v1",
			curRev: "", lastRevision: "",
			want: clusterSnapshotSyncNone,
		},
		{
			name:         "version change with unseeded baseline still reloads",
			rmVersion:    "v1", activeVer: "v2",
			curRev: "", lastRevision: "",
			want: clusterSnapshotSyncReload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decideClusterSnapshotSync(tt.rmVersion, tt.activeVer, tt.curRev, tt.lastRevision)
			if got != tt.want {
				t.Fatalf("decideClusterSnapshotSync(%q, %q, %q, %q) = %v, want %v",
					tt.rmVersion, tt.activeVer, tt.curRev, tt.lastRevision, got, tt.want)
			}
		})
	}
}

func TestSyncClusterSnapshotStateNoRedisNoop(t *testing.T) {
	origRdb := db.Rdb
	defer func() { db.Rdb = origRdb }()
	db.Rdb = nil

	state := &clusterSnapshotSyncState{lastRevision: "5"}
	syncClusterSnapshotState(state)
	if state.lastRevision != "5" {
		t.Fatalf("syncClusterSnapshotState mutated lastRevision without Redis, got %q", state.lastRevision)
	}
}

func TestCurrentSnapshotRevisionNoRedis(t *testing.T) {
	origRdb := db.Rdb
	defer func() { db.Rdb = origRdb }()
	db.Rdb = nil

	if rev, ok := currentSnapshotRevision(); ok || rev != "" {
		t.Fatalf("currentSnapshotRevision() = (%q, %v), want (\"\", false)", rev, ok)
	}
}

func TestCurrentSnapshotRevisionRedisUnavailable(t *testing.T) {
	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond})
	db.Rdb = client
	defer func() {
		client.Close()
		db.Rdb = origRdb
	}()

	if rev, ok := currentSnapshotRevision(); ok || rev != "" {
		t.Fatalf("currentSnapshotRevision() = (%q, %v), want (\"\", false) when Redis unreachable", rev, ok)
	}
}

func TestSeedClusterSnapshotBaselineNoRedisNoop(t *testing.T) {
	origRdb := db.Rdb
	origState := clusterWatcherState
	defer func() {
		db.Rdb = origRdb
		clusterWatcherState = origState
	}()
	db.Rdb = nil
	clusterWatcherState = nil

	SeedClusterSnapshotBaseline()
	if clusterWatcherState != nil {
		t.Fatal("SeedClusterSnapshotBaseline must not create state without Redis")
	}
}

func TestSeedClusterSnapshotBaselineRedisUnavailableKeepsWatermark(t *testing.T) {
	origRdb := db.Rdb
	origState := clusterWatcherState
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond})
	db.Rdb = client
	defer func() {
		client.Close()
		db.Rdb = origRdb
		clusterWatcherState = origState
	}()
	clusterWatcherState = &clusterSnapshotSyncState{lastRevision: "7"}

	SeedClusterSnapshotBaseline()
	if clusterWatcherState.lastRevision != "7" {
		t.Fatalf("SeedClusterSnapshotBaseline must not overwrite watermark on Redis failure, got %q", clusterWatcherState.lastRevision)
	}
}
