package snapshot

import (
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
)

func TestManageMetaCategory_SourceKey(t *testing.T) {
	detachGlobalDB(t)
	s := model.SearchVo{SourceId: "1208629981", Pid: 57}
	ctx, ok := newManageFilterContext(s)
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

	sCid := model.SearchVo{SourceId: "1208629981", Cid: 58}
	ctxCid, ok := newManageFilterContext(sCid)
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
	res, total, err := pageManageMetas(idx, s, page)
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

// detachGlobalDB 让用例在"未连接数据库"的前提下运行，结束后还原。
// 这些用例只测纯内存逻辑，但被测函数会读全局 db.Mdb；包内其他用例可能把 db.Mdb 留成已关闭/缺表的库，
// -count>1 时第二轮起就会读到它而失败。
func detachGlobalDB(t *testing.T) {
	t.Helper()
	prev := db.Mdb
	db.Mdb = nil
	t.Cleanup(func() { db.Mdb = prev })
}
