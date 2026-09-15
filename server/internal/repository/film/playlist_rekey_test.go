package film

import (
	"encoding/json"
	"fmt"
	"testing"

	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func createTestFilmWithKeys(t *testing.T, gdb *gorm.DB, mid int64, name string, pid, dbId int64, keysOrder []string) {
	t.Helper()
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid, ContentKey: fmt.Sprintf("vod_%d", mid), SourceId: "master", DbId: dbId},
		FilmIndexCategory: model.FilmIndexCategory{Pid: pid, CName: model.BigCategoryAnimation},
		FilmIndexContent:  model.FilmIndexContent{Name: name},
	}).Error; err != nil {
		t.Fatalf("create film %d: %v", mid, err)
	}
	for _, key := range keysOrder {
		if err := gdb.Create(&model.MovieMatchKey{Mid: mid, MatchKey: key}).Error; err != nil {
			t.Fatalf("create match key mid=%d key=%s: %v", mid, key, err)
		}
	}
}

func playlistEpisodeJSON(t *testing.T, n int) string {
	t.Helper()
	links := make([]model.MovieUrlInfo, n)
	for i := 0; i < n; i++ {
		links[i] = model.MovieUrlInfo{
			Episode: fmt.Sprintf("第%d集", i+1),
			Link:    fmt.Sprintf("https://x/%d.m3u8", i+1),
		}
	}
	body, err := json.Marshal(links)
	if err != nil {
		t.Fatalf("marshal playlist: %v", err)
	}
	return string(body)
}

func TestSlavePlaylistRekeyTargets_OnlyUniqueMasterMatch(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{20: 0, 30: 0}, map[int64]string{20: model.BigCategoryAnimation, 30: model.BigCategoryAnimation})

	dbidKey := BuildMovieMatchKeys(111, "仙逆")[0]
	catKey20 := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	catKey30 := BuildMovieMatchKeysWithCategory(0, "仙逆", 30)[0]
	plainKey := BuildMovieMatchKeys(0, "仙逆")[0]
	onlyKey := BuildMovieMatchKeysWithCategory(0, "独苗", 20)[0]

	createTestFilmWithKeys(t, gdb, 901, "仙逆", 20, 111, []string{dbidKey, catKey20, plainKey})
	createTestFilmWithKeys(t, gdb, 902, "仙逆", 30, 0, []string{catKey30, plainKey})
	createTestFilmWithKeys(t, gdb, 903, "独苗", 20, 0, []string{onlyKey, BuildMovieMatchKeys(0, "独苗")[0]})

	targets, err := SlavePlaylistRekeyTargets([]string{catKey20, plainKey, onlyKey, "no-such-key"})
	if err != nil {
		t.Fatalf("rekey targets: %v", err)
	}
	if targets[catKey20] != dbidKey {
		t.Fatalf("unique hit must move to the film primary key, got %q", targets[catKey20])
	}
	if _, ok := targets[plainKey]; ok {
		t.Fatalf("key shared by two same-name films must be skipped, got %q", targets[plainKey])
	}
	if _, ok := targets[onlyKey]; ok {
		t.Fatalf("key already equal to the primary key must be skipped, got %q", targets[onlyKey])
	}
	if _, ok := targets["no-such-key"]; ok {
		t.Fatal("key without any master film must be skipped")
	}
}

func TestSlavePlaylistRekeyTargets_DoesNotMergeOntoSharedTitle(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{20: 0, 34: 0}, map[int64]string{20: model.BigCategoryAnimation, 34: model.BigCategoryShortFilm})

	cat20 := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	cat34 := BuildMovieMatchKeysWithCategory(0, "仙逆", 34)[0]
	plain := BuildMovieMatchKeys(0, "仙逆")[0]
	staleA := "stale-unique-anime"
	staleB := "stale-unique-short"

	createTestFilmWithKeys(t, gdb, 901, "仙逆", 20, 0, []string{plain, cat20, staleA})
	createTestFilmWithKeys(t, gdb, 902, "仙逆", 34, 0, []string{plain, cat34, staleB})

	targets, err := SlavePlaylistRekeyTargets([]string{staleA, staleB, cat20, cat34, plain})
	if err != nil {
		t.Fatalf("rekey targets: %v", err)
	}
	if targets[staleA] != cat20 {
		t.Fatalf("unique leftover must move onto category key, got %q", targets[staleA])
	}
	if targets[staleB] != cat34 {
		t.Fatalf("unique leftover must move onto its own category key, got %q", targets[staleB])
	}
	if targets[staleA] == plain || targets[staleB] == plain {
		t.Fatal("same-title films must not rekey onto the shared title key")
	}
	if _, ok := targets[plain]; ok {
		t.Fatal("shared title key must be skipped as a source")
	}
	if _, ok := targets[cat20]; ok {
		t.Fatal("category key already canonical must be skipped")
	}
	if _, ok := targets[cat34]; ok {
		t.Fatal("category key already canonical must be skipped")
	}
}

func TestMigrateSlavePlaylistKeys_DryRunThenApply(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{20: 0}, map[int64]string{20: model.BigCategoryAnimation})

	staleKey := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[1]
	orphanKey := BuildMovieMatchKeys(0, "查无此片")[0]
	for _, key := range []string{staleKey, orphanKey} {
		if err := gdb.Create(&model.SlaveMoviePlaylist{
			SourceId: "subo", MovieKey: key, GroupIndex: 0, GroupName: "速博", Content: `[]`,
		}).Error; err != nil {
			t.Fatalf("create slave row %s: %v", key, err)
		}
	}
	dbKey := BuildMovieMatchKeys(12345, "仙逆")[0]
	catKey := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	createTestFilmWithKeys(t, gdb, 901, "仙逆", 20, 12345, []string{dbKey, catKey, staleKey})

	result, err := MigrateSlavePlaylistKeys(true, 1, nil)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if result.Scanned != 2 || result.Migrated != 1 || result.Skipped != 1 {
		t.Fatalf("unexpected dry-run stats: %+v", result)
	}
	if written := loadPlaylistKeys(t, gdb, "subo"); !written[staleKey] || !written[orphanKey] {
		t.Fatalf("dry run must not touch the database, got %v", written)
	}

	result, err = MigrateSlavePlaylistKeys(false, 1, nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Migrated != 1 {
		t.Fatalf("expected 1 migrated row, got %+v", result)
	}
	written := loadPlaylistKeys(t, gdb, "subo")
	if !written[dbKey] || !written[orphanKey] {
		t.Fatalf("stale row must move onto the primary key and orphan stay, got %v", written)
	}
	if written[staleKey] {
		t.Fatalf("stale key row must be deleted after migration, got %v", written)
	}
}

func TestMigrateSlavePlaylistKeys_SameTitleCrossCategoryKeepsIsolation(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{20: 0, 34: 0}, map[int64]string{
		20: model.BigCategoryAnimation,
		34: model.BigCategoryShortFilm,
	})

	cat20 := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	cat34 := BuildMovieMatchKeysWithCategory(0, "仙逆", 34)[0]
	plain := BuildMovieMatchKeys(0, "仙逆")[0]
	createTestFilmWithKeys(t, gdb, 901, "仙逆", 20, 0, []string{plain, cat20})
	createTestFilmWithKeys(t, gdb, 902, "仙逆", 34, 0, []string{plain, cat34})

	if err := gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "subo", MovieKey: cat20, GroupIndex: 0, GroupName: "速博", Content: playlistEpisodeJSON(t, 22),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "subo", MovieKey: cat34, GroupIndex: 0, GroupName: "速博", Content: playlistEpisodeJSON(t, 80),
	}).Error; err != nil {
		t.Fatal(err)
	}

	result, err := MigrateSlavePlaylistKeys(false, 2, nil)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result.Migrated != 0 {
		t.Fatalf("category playlists must stay isolated, got migrated=%d", result.Migrated)
	}

	written := loadPlaylistKeys(t, gdb, "subo")
	if !written[cat20] || !written[cat34] {
		t.Fatalf("both category playlists must remain, got %v", written)
	}
	if written[plain] {
		t.Fatalf("migration must not merge both 仙逆 playlists onto the shared title key, got %v", written)
	}

	keysByMid := loadMovieMatchKeysByMids([]int64{901, 902})
	if len(keysByMid[901]) == 0 || keysByMid[901][0] != cat20 {
		t.Fatalf("anime match keys[0] must be category key after rewrite, got %v", keysByMid[901])
	}
	if len(keysByMid[902]) == 0 || keysByMid[902][0] != cat34 {
		t.Fatalf("short-drama match keys[0] must be category key after rewrite, got %v", keysByMid[902])
	}
}

func TestMigrateSlavePlaylistKeys_KeepsNewerTargetPlaylist(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{20: 0}, map[int64]string{20: model.BigCategoryAnimation})

	dbKey := BuildMovieMatchKeys(12345, "仙逆")[0]
	catKey := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	plain := BuildMovieMatchKeys(0, "仙逆")[0]
	createTestFilmWithKeys(t, gdb, 901, "仙逆", 20, 12345, []string{dbKey, catKey, plain})

	if err := gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "subo", MovieKey: dbKey, GroupIndex: 0, GroupName: "速博", Content: playlistEpisodeJSON(t, 22),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "subo", MovieKey: catKey, GroupIndex: 0, GroupName: "速博", Content: playlistEpisodeJSON(t, 12),
	}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateSlavePlaylistKeys(false, 10, nil); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	written := loadPlaylistKeys(t, gdb, "subo")
	if !written[dbKey] {
		t.Fatal("primary playlist row must remain")
	}
	if written[catKey] {
		t.Fatal("stale category row must be deleted after merge")
	}

	var row model.SlaveMoviePlaylist
	if err := gdb.Where("source_id = ? AND movie_key = ?", "subo", dbKey).First(&row).Error; err != nil {
		t.Fatalf("load primary row: %v", err)
	}
	var links []model.MovieUrlInfo
	if err := json.Unmarshal([]byte(row.Content), &links); err != nil {
		t.Fatalf("decode playlist: %v", err)
	}
	if got := episodeCount(links); got != 22 {
		t.Fatalf("migration must keep the newer 22-episode playlist, got %d", got)
	}
}

// 影片行未把分类落到 pid/cid 上时，采集写入侧会靠详情 CName 兜底生成「片名#大类」键；
// 迁移的规范键集必须用同一套解析结果，否则会连同正在生效的播放列表源一起删掉。
func TestMigrateSlavePlaylistKeys_KeepsCategoryKeyWhenFilmPidUnresolved(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{20: 0}, map[int64]string{20: model.BigCategoryAnimation})

	catKey := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	plain := BuildMovieMatchKeys(0, "仙逆")[0]
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 901, ContentKey: "vod_901", SourceId: "master"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 0, CName: model.BigCategoryAnimation},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙逆"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{catKey, plain} {
		if err := gdb.Create(&model.MovieMatchKey{Mid: 901, MatchKey: key}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "subo", MovieKey: catKey, GroupIndex: 0, GroupName: "速博", Content: playlistEpisodeJSON(t, 12),
	}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateSlavePlaylistKeys(false, 10, nil); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	keys := loadMovieMatchKeysByMids([]int64{901})[901]
	if len(keys) == 0 || keys[0] != catKey {
		t.Fatalf("category key must survive for unresolved pid, got %v", keys)
	}
	groups := GetMultiplePlayGroupsBySourcesAndKeys([]model.FilmSource{{Id: "subo", Name: "速博"}}, keys)
	if got := groups["subo"]; len(got) != 1 || len(got[0].LinkList) != 12 {
		t.Fatalf("detail page must keep the slave source after migration, got %+v", got)
	}
}

// 旧键（此处模拟改名遗留键）仍被播放列表引用时不能直接删：先保留，交给第二阶段归并到规范主键。
func TestMigrateSlavePlaylistKeys_KeepsKeyStillReferencedByPlaylist(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)
	support.SetCategoryTreeForTest(map[int64]int64{20: 0}, map[int64]string{20: model.BigCategoryAnimation})

	catKey := BuildMovieMatchKeysWithCategory(0, "仙逆", 20)[0]
	plain := BuildMovieMatchKeys(0, "仙逆")[0]
	legacyCat := BuildMovieMatchKeysWithCategory(0, "旧名", 20)[0]
	legacyPlain := BuildMovieMatchKeys(0, "旧名")[0]
	createTestFilmWithKeys(t, gdb, 901, "仙逆", 20, 0, []string{legacyCat, legacyPlain, catKey, plain})

	if err := gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "subo", MovieKey: legacyCat, GroupIndex: 0, GroupName: "速博", Content: playlistEpisodeJSON(t, 12),
	}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateSlavePlaylistKeys(false, 10, nil); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	written := loadPlaylistKeys(t, gdb, "subo")
	if !written[catKey] {
		t.Fatalf("row on the leftover key must be merged onto the canonical key, got %v", written)
	}
	keys := loadMovieMatchKeysByMids([]int64{901})[901]
	if len(keys) == 0 || keys[0] != catKey {
		t.Fatalf("canonical key must stay first after keeping the leftover key, got %v", keys)
	}
	groups := GetMultiplePlayGroupsBySourcesAndKeys([]model.FilmSource{{Id: "subo", Name: "速博"}}, keys)
	if got := groups["subo"]; len(got) != 1 || len(got[0].LinkList) != 12 {
		t.Fatalf("detail page must keep the slave source after migration, got %+v", got)
	}
}
