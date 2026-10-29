package snapshot

import (
	"sync/atomic"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/utils"
)

func TestPatchSearchMetasKeepsUntouchedAndDropsMissing(t *testing.T) {
	items := []FilmSearchMeta{{Mid: 1}, {Mid: 2}, {Mid: 3}}
	upsert := map[int64]FilmSearchMeta{
		2: {Mid: 2, FirstSourceId: "new"},
		4: {Mid: 4},
	}
	got := patchSearchMetas(items, upsert, map[int64]struct{}{3: {}})
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	if got[0].Mid != 1 || got[1].Mid != 2 || got[1].FirstSourceId != "new" || got[2].Mid != 4 {
		t.Fatalf("patched items = %+v", got)
	}
}

func TestResealSearchIndexMidsDoesNotFullScan(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	createTestFilm(t, gdb, 101, "旧名字", 1000, "默认主源")
	createTestFilm(t, gdb, 102, "另一部", 2000, "默认主源")
	if err := gdb.Model(&model.FilmIndex{}).Where("mid = ?", 101).Update("name", "新名字").Error; err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := SetActiveSnapshotVersion("v-reseal"); err != nil {
		t.Fatal(err)
	}

	old := FilmSearchMeta{Mid: 101, Item: utils.FilmSearchItem{Mid: 101, Name: "旧名字", UpdateStamp: 1000}}
	other := FilmSearchMeta{Mid: 102, Item: utils.FilmSearchItem{Mid: 102, Name: "另一部", UpdateStamp: 2000}}
	utils.FillSearchDerivedFields(&old.Item)
	utils.FillSearchDerivedFields(&other.Item)
	activeFilmSearchMetas.Store(sealSearchMetaIndex("v-reseal", []FilmSearchMeta{old, other}))

	var scanned atomic.Bool
	prevScan := scanActiveFilmSearchIndex
	prevDebounce := searchRebuildDebounce
	scanActiveFilmSearchIndex = func(uint64) error {
		scanned.Store(true)
		return nil
	}
	searchRebuildDebounce = 10 * time.Millisecond
	t.Cleanup(func() {
		searchRebuild.mu.Lock()
		if searchRebuild.timer != nil {
			searchRebuild.timer.Stop()
			searchRebuild.timer = nil
		}
		searchRebuild.gen = 0
		searchRebuild.running = false
		searchRebuild.mu.Unlock()
		scanActiveFilmSearchIndex = prevScan
		searchRebuildDebounce = prevDebounce
	})

	ResealSearchIndexMids([]int64{101})
	time.Sleep(40 * time.Millisecond)
	if scanned.Load() {
		t.Fatal("changed one film but still full-scanned the library")
	}

	idx := activeFilmSearchMetas.Load()
	if idx == nil || len(idx.Items) != 2 {
		t.Fatalf("index items=%v", idx)
	}
	var sawNew, sawOther bool
	for _, it := range idx.Items {
		switch it.Mid {
		case 101:
			sawNew = it.Item.Name == "新名字" && len(it.PlaySources) == 1 && it.PlaySources[0] == "source_master"
		case 102:
			sawOther = it.Item.Name == "另一部"
		}
	}
	if !sawNew || !sawOther {
		t.Fatalf("reseal result = %+v", idx.Items)
	}
}

func TestResealSearchIndexMids_TriggersRebuildWhenTooManyMids(t *testing.T) {
	setupSnapshotRepoTestDB(t)
	if err := SetActiveSnapshotVersion("v-too-many"); err != nil {
		t.Fatal(err)
	}
	activeFilmSearchMetas.Store(sealSearchMetaIndex("v-too-many", []FilmSearchMeta{{Mid: 1}}))

	var scanned atomic.Bool
	prevScan := scanActiveFilmSearchIndex
	prevDebounce := searchRebuildDebounce
	scanActiveFilmSearchIndex = func(uint64) error {
		scanned.Store(true)
		return nil
	}
	searchRebuildDebounce = 10 * time.Millisecond
	t.Cleanup(func() {
		searchRebuild.mu.Lock()
		if searchRebuild.timer != nil {
			searchRebuild.timer.Stop()
			searchRebuild.timer = nil
		}
		searchRebuild.gen = 0
		searchRebuild.running = false
		searchRebuild.mu.Unlock()
		scanActiveFilmSearchIndex = prevScan
		searchRebuildDebounce = prevDebounce
	})

	manyMids := make([]int64, 1005)
	for i := range manyMids {
		manyMids[i] = int64(i + 1)
	}
	ResealSearchIndexMids(manyMids)
	time.Sleep(40 * time.Millisecond)
	if !scanned.Load() {
		t.Fatal("expected full-scan rebuild triggered when mids > 1000")
	}
}

func TestLoadPlaySources_ChunksLargeMids(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	// 插入一个播放源测试行
	gdb.Create(&model.FilmSourcePlaylist{
		Mid:      12,
		SourceId: "src-chunk",
		LineKind: "play",
	})
	largeMids := make([]int64, 1200)
	for i := range largeMids {
		largeMids[i] = int64(i + 1)
	}
	plays, err := loadPlaySources(largeMids)
	if err != nil {
		t.Fatalf("loadPlaySources with 1200 mids failed: %v", err)
	}
	if len(plays[12]) != 1 || plays[12][0] != "src-chunk" {
		t.Fatalf("expected play source for mid 12, got: %+v", plays[12])
	}
}
