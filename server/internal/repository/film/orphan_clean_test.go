package film

import (
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

func TestCleanOrphanRows_MultiBatchKeepsLiveFilms(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	origScan, origDelete := orphanScanBatchSize, orphanDeleteBatchSize
	orphanScanBatchSize, orphanDeleteBatchSize = 3, 2
	defer func() {
		db.Rdb = origRdb
		orphanScanBatchSize, orphanDeleteBatchSize = origScan, origDelete
	}()

	gdb := setupFilmZeroTestDB(t)

	// 存活影片：偶数 mid 2..20；mid 21 为软删除，仍视为存在（与原 NOT EXISTS 语义一致）。
	live := map[int64]bool{}
	for mid := int64(2); mid <= 20; mid += 2 {
		live[mid] = true
		gdb.Create(&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid, FirstSourceId: "src1"},
			FilmIndexContent:  model.FilmIndexContent{Name: "live"},
		})
	}
	live[21] = true
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 21, FirstSourceId: "src1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "soft-deleted"},
		DeletedAt:         gorm.DeletedAt{Time: time.Now(), Valid: true},
	})

	// mid 1..21 全部写入三张表，每个 mid 两行，奇数（除 21）是孤儿。
	for mid := int64(1); mid <= 21; mid++ {
		for _, src := range []string{"src1", "src2"} {
			gdb.Create(&model.FilmSourcePlaylist{Mid: mid, SourceId: src, LineKind: "play", Content: "[]"})
			gdb.Create(&model.MovieSourceMapping{SourceId: src, SourceMid: mid * 100, GlobalMid: mid})
		}
		gdb.Create(&model.MovieMatchKey{Mid: mid, MatchKey: "a"})
		gdb.Create(&model.MovieMatchKey{Mid: mid, MatchKey: "b"})
	}
	orphanCount := int64(10) // 1,3,...,19

	n, err := CleanOrphanPlaylists()
	if err != nil {
		t.Fatalf("CleanOrphanPlaylists: %v", err)
	}
	if n != orphanCount*2 {
		t.Fatalf("playlists cleaned = %d, want %d", n, orphanCount*2)
	}
	k, err := CleanOrphanMatchKeysAndMappings()
	if err != nil {
		t.Fatalf("CleanOrphanMatchKeysAndMappings: %v", err)
	}
	if k != orphanCount*4 {
		t.Fatalf("keys+mappings cleaned = %d, want %d", k, orphanCount*4)
	}

	check := func(name string, mids []int64) {
		t.Helper()
		seen := map[int64]int{}
		for _, mid := range mids {
			if !live[mid] {
				t.Fatalf("%s: orphan mid %d survived", name, mid)
			}
			seen[mid]++
		}
		for mid := range live {
			if seen[mid] != 2 {
				t.Fatalf("%s: live mid %d has %d rows, want 2", name, mid, seen[mid])
			}
		}
	}
	var mids []int64
	gdb.Model(&model.FilmSourcePlaylist{}).Pluck("mid", &mids)
	check("playlists", mids)
	mids = nil
	gdb.Model(&model.MovieMatchKey{}).Pluck("mid", &mids)
	check("match_keys", mids)
	mids = nil
	gdb.Model(&model.MovieSourceMapping{}).Pluck("global_mid", &mids)
	check("mappings", mids)

	// 再跑一次应无可清理数据。
	if n, err := CleanOrphanPlaylists(); err != nil || n != 0 {
		t.Fatalf("second run playlists = %d, %v", n, err)
	}
	if k, err := CleanOrphanMatchKeysAndMappings(); err != nil || k != 0 {
		t.Fatalf("second run keys = %d, %v", k, err)
	}
}

func TestCleanOrphanRows_ReturnsError(t *testing.T) {
	gdb := setupFilmZeroTestDB(t)
	if err := gdb.Migrator().DropTable(&model.MovieSourceMapping{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := CleanOrphanMatchKeysAndMappings(); err == nil {
		t.Fatalf("expected error when movie_source_mapping table is missing")
	}
}
