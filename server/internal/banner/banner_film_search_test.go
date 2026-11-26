package banner

import (
	"context"
	"testing"

	"server/internal/film/snapshot"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/sourcecatalog"
)

func TestSearchBannerFilmOptions_MemoryAndFallbackPaths(t *testing.T) {
	gdb := setupBannerSvcDB(t)
	if err := sourcecatalog.RefreshCollectSourceCatalog(); err != nil {
		t.Fatalf("refresh source catalog: %v", err)
	}
	t.Cleanup(func() {
		snapshot.ClearActiveFilmReadModel()
	})

	films := []model.FilmIndex{
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101, FirstSourceId: "src1"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 11, CName: "电影"},
			FilmIndexContent:  model.FilmIndexContent{Name: "甲片", Hits: 100, Score: 8.1, Year: 2023, UpdateStamp: 200, Actor: "张三"},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 102, FirstSourceId: "src1"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 11, CName: "电影"},
			FilmIndexContent:  model.FilmIndexContent{Name: "乙片", Hits: 300, Score: 9.2, Year: 2025, UpdateStamp: 100, Director: "李四"},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 103, FirstSourceId: "src1"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 11, CName: "电影"},
			FilmIndexContent:  model.FilmIndexContent{Name: "丙片", Hits: 200, Score: 7.5, Year: 2024, UpdateStamp: 300},
		},
	}
	for _, f := range films {
		if err := gdb.Create(&f).Error; err != nil {
			t.Fatalf("seed film %d: %v", f.Mid, err)
		}
		if err := gdb.Create(&model.FilmSourcePlaylist{
			Mid: f.Mid, SourceId: "src1", LineKind: model.LineKindPlay, GroupIndex: 0, GroupName: "线路",
		}).Error; err != nil {
			t.Fatalf("seed line %d: %v", f.Mid, err)
		}
	}

	// 1. SQL 延迟关联回退路径（未加载内存索引时）
	snapshot.ClearActiveFilmReadModel()
	page := &dto.Page{Current: 1, PageSize: 2}
	list, err := SearchBannerFilmOptions(context.Background(), "src1", "", "hits", page)
	if err != nil {
		t.Fatalf("sql hits search: %v", err)
	}
	if page.Total != 3 || page.PageCount != 2 || len(list) != 2 || list[0].Mid != 102 || list[1].Mid != 103 {
		t.Fatalf("sql hits mismatch: page=%+v list=%+v", page, list)
	}

	// 关键词跨字段（导演）检索走 SQL 回退
	kwPage := &dto.Page{Current: 1, PageSize: 10}
	kwList, err := SearchBannerFilmOptions(context.Background(), "src1", "李四", "", kwPage)
	if err != nil {
		t.Fatalf("keyword search: %v", err)
	}
	if kwPage.Total != 1 || len(kwList) != 1 || kwList[0].Mid != 102 {
		t.Fatalf("keyword mismatch: page=%+v list=%+v", kwPage, kwList)
	}

	// 2. 加载主站内存索引后走内存排序/分页 + 主键回表路径
	if err := snapshot.LoadActiveFilmReadModel(snapshot.LiveReadVersion); err != nil {
		t.Fatalf("load read model: %v", err)
	}

	for _, tc := range []struct {
		sortField string
		wantFirst int64
	}{
		{sortField: "latest", wantFirst: 103},
		{sortField: "unknown", wantFirst: 103},
		{sortField: "hits", wantFirst: 102},
		{sortField: "score", wantFirst: 102},
		{sortField: "year", wantFirst: 102},
	} {
		p := &dto.Page{Current: 1, PageSize: 1}
		got, err := SearchBannerFilmOptions(context.Background(), "src1", "", tc.sortField, p)
		if err != nil {
			t.Fatalf("memory sort %s: %v", tc.sortField, err)
		}
		if p.Total != 3 || p.PageCount != 3 || len(got) != 1 || got[0].Mid != tc.wantFirst {
			t.Fatalf("memory sort %s mismatch: page=%+v got=%+v", tc.sortField, p, got)
		}
	}

	pHits2 := &dto.Page{Current: 2, PageSize: 1}
	gotHits2, err := SearchBannerFilmOptions(context.Background(), "src1", "", "hits", pHits2)
	if err != nil || pHits2.Total != 3 || len(gotHits2) != 1 || gotHits2[0].Mid != 103 {
		t.Fatalf("memory hits page 2 mismatch: err=%v page=%+v got=%+v", err, pHits2, gotHits2)
	}

	p2 := &dto.Page{Current: 2, PageSize: 2}
	got2, err := SearchBannerFilmOptions(context.Background(), "src1", "", "latest", p2)
	if err != nil {
		t.Fatalf("memory page 2: %v", err)
	}
	if p2.Total != 3 || len(got2) != 1 || got2[0].Mid != 102 {
		t.Fatalf("memory page 2 mismatch: page=%+v got=%+v", p2, got2)
	}

	// 超大页码不得导致 offset + limit 溢出并触发 makeslice panic
	for _, sortField := range []string{"hits", "latest"} {
		huge := &dto.Page{Current: 184467440737095517, PageSize: 50}
		gotHuge, err := SearchBannerFilmOptions(context.Background(), "src1", "", sortField, huge)
		if err != nil {
			t.Fatalf("huge page %s: %v", sortField, err)
		}
		if huge.Total != 3 || len(gotHuge) != 0 {
			t.Fatalf("huge page %s mismatch: page=%+v got=%+v", sortField, huge, gotHuge)
		}
	}
}
