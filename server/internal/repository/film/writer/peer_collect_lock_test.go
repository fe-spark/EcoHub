package writer

import (
	"testing"

	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMatchKeyLockIndexes_AscendingUnique(t *testing.T) {
	keys := []string{"斗破苍穹", "仙逆", "斗破苍穹", "", "海贼王", "仙逆"}
	idxs := matchKeyLockIndexes(keys)
	if len(idxs) == 0 {
		t.Fatal("expected lock indexes")
	}
	seen := map[int]struct{}{}
	for i, idx := range idxs {
		if idx < 0 || idx >= len(filmMatchKeyLocks) {
			t.Fatalf("index out of range: %d", idx)
		}
		if _, ok := seen[idx]; ok {
			t.Fatalf("duplicate lock index %d", idx)
		}
		seen[idx] = struct{}{}
		if i > 0 && idxs[i-1] > idxs[i] {
			t.Fatalf("lock indexes not ascending: %v", idxs)
		}
	}
}

func TestSaveStationPlaylists_UpsertByPrimaryKey(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:playlist_pk?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmSourcePlaylist{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	first := []model.FilmSourcePlaylist{
		{Mid: 7, SourceId: "src", LineKind: "play", GroupIndex: 0, GroupName: "线1", Content: "a", ContentHash: "h1", EpisodeCount: 1},
		{Mid: 7, SourceId: "src", LineKind: "play", GroupIndex: 1, GroupName: "线2", Content: "b", ContentHash: "h2", EpisodeCount: 2},
	}
	changed, playChanged, err := saveStationPlaylistsTx(gdb, 7, "src", first)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !changed || !playChanged {
		t.Fatalf("changed=%v playChanged=%v", changed, playChanged)
	}

	var stored model.FilmSourcePlaylist
	if err := gdb.Where("mid = ? AND group_index = ?", 7, 0).First(&stored).Error; err != nil {
		t.Fatalf("group 0 missing: %v", err)
	}
	if stored.Content != "a" {
		t.Fatalf("group 0 content = %q", stored.Content)
	}

	unchanged, unchangedPlay, err := saveStationPlaylistsTx(gdb, 7, "src", first)
	if err != nil {
		t.Fatalf("identical: %v", err)
	}
	if unchanged || unchangedPlay {
		t.Fatalf("identical write should be a no-op, changed=%v play=%v", unchanged, unchangedPlay)
	}

	updated := []model.FilmSourcePlaylist{
		{Mid: 7, SourceId: "src", LineKind: "play", GroupIndex: 0, GroupName: "线1", Content: "a2", ContentHash: "h3", EpisodeCount: 3},
	}
	changed, playChanged, err = saveStationPlaylistsTx(gdb, 7, "src", updated)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !changed || !playChanged {
		t.Fatalf("upsert changed=%v playChanged=%v", changed, playChanged)
	}
	var n int64
	if err := gdb.Model(&model.FilmSourcePlaylist{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("vanished group should be deleted, count=%d", n)
	}
	if err := gdb.Where("group_index = ?", 0).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Content != "a2" || stored.EpisodeCount != 3 {
		t.Fatalf("upsert content=%q episodes=%d", stored.Content, stored.EpisodeCount)
	}
}
