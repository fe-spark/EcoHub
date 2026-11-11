package snapshot

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/utils"
)

func TestPatchSearchMetasKeepsUntouchedAndDropsMissing(t *testing.T) {
	items := []FilmSearchMeta{{Mid: 1}, {Mid: 2}, {Mid: 3}}
	ptrs := make([]*FilmSearchMeta, len(items))
	for i := range items {
		ptrs[i] = &items[i]
	}
	upsert := map[int64]FilmSearchMeta{
		2: {Mid: 2, FirstSourceId: "new"},
		4: {Mid: 4},
	}
	got := patchSearchMetas(ptrs, upsert, map[int64]struct{}{3: {}})
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
	scanActiveFilmSearchIndex = func() (int64, error) {
		scanned.Store(true)
		return 0, nil
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

func TestResealSearchIndexMids_DoesNotRebuildWhenLargeMids(t *testing.T) {
	setupSnapshotRepoTestDB(t)
	if err := SetActiveSnapshotVersion("v-large-mids"); err != nil {
		t.Fatal(err)
	}
	activeFilmSearchMetas.Store(sealSearchMetaIndex("v-large-mids", []FilmSearchMeta{{Mid: 1}}))

	var scanned atomic.Bool
	prevScan := scanActiveFilmSearchIndex
	prevDebounce := searchRebuildDebounce
	scanActiveFilmSearchIndex = func() (int64, error) {
		scanned.Store(true)
		return 0, nil
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
	if scanned.Load() {
		t.Fatal("unexpected full-scan rebuild triggered when large mids should be resealed incrementally")
	}
}

func TestResealSearchIndexMids_LargeBatchStaysIncremental(t *testing.T) {
	setupSnapshotRepoTestDB(t)
	if err := SetActiveSnapshotVersion("v-large"); err != nil {
		t.Fatal(err)
	}
	activeFilmSearchMetas.Store(sealSearchMetaIndex("v-large", []FilmSearchMeta{{Mid: 1}}))

	var scanned atomic.Bool
	stopRebuild := stubSearchRebuildScan(t, &scanned)
	defer stopRebuild()

	hugeMids := make([]int64, 55000)
	for i := range hugeMids {
		hugeMids[i] = int64(i + 1)
	}
	ResealSearchIndexMids(hugeMids)
	time.Sleep(40 * time.Millisecond)
	if scanned.Load() {
		t.Fatal("unexpected full-scan rebuild triggered for large batch without drops")
	}
}

func TestResealRereadsWhenIndexPointerChanges(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	createTestFilm(t, gdb, 101, "将被删除", 1000, "默认主源")
	createTestFilm(t, gdb, 102, "留下", 2000, "默认主源")
	if err := SetActiveSnapshotVersion("v-stale"); err != nil {
		t.Fatal(err)
	}
	activeFilmSearchMetas.Store(sealSearchMetaIndex("v-stale", []FilmSearchMeta{
		{Mid: 101, Item: utils.FilmSearchItem{Mid: 101, Name: "将被删除"}},
		{Mid: 102, Item: utils.FilmSearchItem{Mid: 102, Name: "留下"}},
	}))

	var scanned atomic.Bool
	stopRebuild := stubSearchRebuildScan(t, &scanned)
	defer stopRebuild()

	resealAfterRead = func() {
		if err := gdb.Where("mid = ?", 101).Delete(&model.FilmIndex{}).Error; err != nil {
			t.Errorf("delete film: %v", err)
		}
		activeFilmSearchMetas.Store(sealSearchMetaIndex("v-stale", []FilmSearchMeta{
			{Mid: 102, Item: utils.FilmSearchItem{Mid: 102, Name: "已剔除"}},
		}))
	}
	t.Cleanup(func() { resealAfterRead = nil })

	if !ResealSearchIndexMids([]int64{101, 102}) {
		t.Fatal("pointer change should reseal from a fresh read")
	}
	time.Sleep(40 * time.Millisecond)
	if scanned.Load() {
		t.Fatal("pointer change should reread, not full-scan")
	}
	idx := activeFilmSearchMetas.Load()
	if idx == nil {
		t.Fatal("index missing")
	}
	for _, it := range idx.Items {
		if it.Mid == 101 {
			t.Fatal("deleted film was written back from the stale read")
		}
	}
}

func TestResealEmptyIndexRequestsFullScan(t *testing.T) {
	setupSnapshotRepoTestDB(t)
	if err := SetActiveSnapshotVersion("v-empty"); err != nil {
		t.Fatal(err)
	}
	activeFilmSearchMetas.Store(sealSearchMetaIndex("v-empty", nil))

	var scanned atomic.Bool
	stopRebuild := stubSearchRebuildScan(t, &scanned)
	defer stopRebuild()

	prevRead := readSearchMetaUpserts
	readSearchMetaUpserts = func([]int64, func(int)) (map[int64]FilmSearchMeta, map[int64]struct{}, error) {
		t.Error("empty index must not point-query films by mid")
		return nil, nil, errors.New("should not read by mid")
	}
	t.Cleanup(func() { readSearchMetaUpserts = prevRead })

	if ResealSearchIndexMids([]int64{1, 2, 3}) {
		t.Fatal("empty index should hand off to a sequential rebuild")
	}
	time.Sleep(40 * time.Millisecond)
	if !scanned.Load() {
		t.Fatal("empty index did not request a sequential rebuild")
	}
}

func TestResealReadFailureRequestsRebuild(t *testing.T) {
	setupSnapshotRepoTestDB(t)
	if err := SetActiveSnapshotVersion("v-read-fail"); err != nil {
		t.Fatal(err)
	}
	activeFilmSearchMetas.Store(sealSearchMetaIndex("v-read-fail", []FilmSearchMeta{{Mid: 1}}))

	var scanned atomic.Bool
	stopRebuild := stubSearchRebuildScan(t, &scanned)
	defer stopRebuild()

	prevRead := readSearchMetaUpserts
	readSearchMetaUpserts = func([]int64, func(int)) (map[int64]FilmSearchMeta, map[int64]struct{}, error) {
		return nil, nil, errors.New("db down")
	}
	t.Cleanup(func() { readSearchMetaUpserts = prevRead })

	if ResealSearchIndexMids([]int64{1}) {
		t.Fatal("read failure must not report applied")
	}
	time.Sleep(40 * time.Millisecond)
	if !scanned.Load() {
		t.Fatal("read failure did not request a full rebuild")
	}
}

func stubSearchRebuildScan(t *testing.T, scanned *atomic.Bool) func() {
	t.Helper()
	prevScan := scanActiveFilmSearchIndex
	prevDebounce := searchRebuildDebounce
	scanActiveFilmSearchIndex = func() (int64, error) {
		scanned.Store(true)
		return 0, nil
	}
	searchRebuildDebounce = 10 * time.Millisecond
	return func() {
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
