package snapshot

import (
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"
)

func TestManageMetaCategory_SourceKey(t *testing.T) {
	detachGlobalDB(t)
	s := model.SearchVo{SourceId: "other", Pid: 57}
	ctx, ok := newManageFilterContext(s, "1208629981")
	if !ok {
		t.Fatal("expected filter context to be created")
	}

	meta := FilmSearchMeta{Pid: 57, CategoryKey: "source:1208629981:57", PlaySources: []string{"1208629981"}}
	if !ctx.pass(&meta) {
		t.Fatal("source type key should match category_key")
	}
	byPidOnly := FilmSearchMeta{Pid: 57, CategoryKey: "source:other:57", PlaySources: []string{"1208629981"}}
	if ctx.pass(&byPidOnly) {
		t.Fatal("local pid must not match another source key")
	}

	sCid := model.SearchVo{SourceId: "other", Cid: 58}
	ctxCid, ok := newManageFilterContext(sCid, "1208629981")
	if !ok {
		t.Fatal("expected cid filter context to be created")
	}
	cid := FilmSearchMeta{Cid: 58, CategoryKey: "source:1208629981:58", PlaySources: []string{"1208629981"}}
	if !ctxCid.pass(&cid) {
		t.Fatal("cid source key should match")
	}
}

func TestPageManageMetas_ScalePerformance(t *testing.T) {
	detachGlobalDB(t)
	const count = 50000
	items := make([]FilmSearchMeta, count)
	byUpdate := make([]int, count)
	for i := 0; i < count; i++ {
		byUpdate[i] = i
		items[i] = FilmSearchMeta{
			Mid:         int64(i + 1),
			Pid:         int64(i%5 + 1),
			Cid:         int64(i%20 + 1),
			PlaySources: []string{"3370810636"},
			CategoryKey: "source:3370810636:2",
		}
	}
	ptrs := make([]*FilmSearchMeta, len(items))
	for i := range items {
		ptrs[i] = &items[i]
	}
	idx := &filmSearchMetaIndex{
		Version:  "v_bench",
		Items:    ptrs,
		ByUpdate: byUpdate,
	}

	page := &dto.Page{Current: 1, PageSize: 10}
	s := model.SearchVo{
		SourceId: "3370810636",
		Pid:      2,
		Cid:      0,
	}

	// 50,000 条数据内存切页应在 50ms 内完成，绝对不能出现 18s 级卡顿
	start := time.Now()
	res, total, err := pageManageMetas(idx, s, page, "3370810636")
	cost := time.Since(start)
	if err != nil {
		t.Fatalf("pageManageMetas error: %v", err)
	}
	if cost > 100*time.Millisecond {
		t.Fatalf("pageManageMetas took too long: %s (must be < 100ms)", cost)
	}
	if len(res) != 10 {
		t.Fatalf("expected 10 items in window, got %d", len(res))
	}
	if total <= 0 {
		t.Fatalf("expected positive total, got %d", total)
	}
}

func TestOtherSourceUpsertDoesNotResealPrimaryIndex(t *testing.T) {
	prevDB := db.Mdb
	prevIdx := activeFilmSearchMetas.Load()
	t.Cleanup(func() {
		db.Mdb = prevDB
		activeFilmSearchMetas.Store(prevIdx)
	})
	gdb := setupSearchSourceTestDB(t)
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 9, FirstSourceId: "hd"},
		FilmIndexContent:  model.FilmIndexContent{Name: "其他站影片"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	item := FilmSearchMeta{Mid: 1, FirstSourceId: "uku", Item: utils.FilmSearchItem{Mid: 1, Name: "主站片"}}
	idx := sealSearchMetaIndex("v-keep", []FilmSearchMeta{item})
	idx.PrimaryID = "uku"
	activeFilmSearchMetas.Store(idx)

	UpsertMidsToActiveFilmSearchIndex("v-keep", []int64{9})
	if got := activeFilmSearchMetas.Load(); got != idx {
		t.Fatal("non-primary save resealed the resident index")
	}

	prevModel := activeFilmReadModel.Load()
	activeFilmReadModel.Store(&FilmReadModel{Version: "v-keep"})
	t.Cleanup(func() { activeFilmReadModel.Store(prevModel) })
	if _, _, err := UpsertActiveSnapshotsByMids(9); err != nil {
		t.Fatal(err)
	}
	if got := activeFilmSearchMetas.Load(); got != idx {
		t.Fatal("non-primary snapshot save wrote the search index")
	}
}

func detachGlobalDB(t *testing.T) {
	t.Helper()
	prev := db.Mdb
	db.Mdb = nil
	t.Cleanup(func() { db.Mdb = prev })
}

func TestCountFilmsByCategoryKeys(t *testing.T) {
	prevIdx := activeFilmSearchMetas.Load()
	t.Cleanup(func() { activeFilmSearchMetas.Store(prevIdx) })

	activeFilmSearchMetas.Store(nil)
	if _, ok := CountFilmsByCategoryKeys("src_a"); ok {
		t.Fatal("nil search index must report ok=false")
	}

	emptyIdx := sealSearchMetaIndex("v-empty", nil)
	emptyIdx.PrimaryID = "src_a"
	activeFilmSearchMetas.Store(emptyIdx)
	if _, ok := CountFilmsByCategoryKeys("src_a"); ok {
		t.Fatal("empty search index must report ok=false so live DB counts are used")
	}

	idx := sealSearchMetaIndex("v-cat-count", []FilmSearchMeta{
		{Mid: 1, FirstSourceId: "src_a", CategoryKey: "source:src_a:6"},
		{Mid: 2, FirstSourceId: "src_a", CategoryKey: "source:src_a:6"},
		{Mid: 3, FirstSourceId: "src_a", CategoryKey: "source:src_a:7"},
	})
	idx.PrimaryID = "src_a"
	activeFilmSearchMetas.Store(idx)

	if _, ok := CountFilmsByCategoryKeys("src_b"); ok {
		t.Fatal("non-primary source must report ok=false")
	}
	counts, ok := CountFilmsByCategoryKeys("src_a")
	if !ok {
		t.Fatal("primary source should hit in-memory index")
	}
	if counts["source:src_a:6"] != 2 || counts["source:src_a:7"] != 1 || len(counts) != 2 {
		t.Fatalf("unexpected category counts: %#v", counts)
	}
}
