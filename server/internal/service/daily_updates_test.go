package service

import (
	"testing"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/notify"
)

func TestNormalizeDailyUpdateReqDefaults(t *testing.T) {
	got := normalizeDailyUpdateReq(DailyUpdateListReq{})
	if got.Page == nil {
		t.Fatal("page should not be nil")
	}
	if got.Page.Current != 1 {
		t.Fatalf("current: want 1 got %d", got.Page.Current)
	}
	if got.Page.PageSize != dailyUpdateDefaultPageSize {
		t.Fatalf("pageSize: want %d got %d", dailyUpdateDefaultPageSize, got.Page.PageSize)
	}
	if got.Exclude != nil {
		t.Fatalf("exclude should be cleared when not random, got %v", got.Exclude)
	}
}

func TestNormalizeDailyUpdateReqClampAndRandomExclude(t *testing.T) {
	got := normalizeDailyUpdateReq(DailyUpdateListReq{
		Page:    &dto.Page{Current: 0, PageSize: 500},
		Random:  true,
		Exclude: []int64{1, 2},
	})
	if got.Page.Current != 1 {
		t.Fatalf("current: want 1 got %d", got.Page.Current)
	}
	if got.Page.PageSize != dailyUpdateMaxPageSize {
		t.Fatalf("pageSize: want %d got %d", dailyUpdateMaxPageSize, got.Page.PageSize)
	}
	if len(got.Exclude) != 2 {
		t.Fatalf("random should keep exclude, got %v", got.Exclude)
	}

	over := make([]int64, dailyUpdateMaxExclude+10)
	for i := range over {
		over[i] = int64(i + 1)
	}
	got = normalizeDailyUpdateReq(DailyUpdateListReq{Random: true, Exclude: over})
	if len(got.Exclude) != dailyUpdateMaxExclude {
		t.Fatalf("exclude cap: want %d got %d", dailyUpdateMaxExclude, len(got.Exclude))
	}
}

func TestFillDailyUpdatePage(t *testing.T) {
	page := fillDailyUpdatePage(&dto.Page{Current: 2, PageSize: 21}, 50)
	if page.Total != 50 {
		t.Fatalf("total: want 50 got %d", page.Total)
	}
	if page.PageCount != 3 {
		t.Fatalf("pageCount: want 3 got %d", page.PageCount)
	}
	page = fillDailyUpdatePage(&dto.Page{Current: 1, PageSize: 21}, 0)
	if page.PageCount != 1 {
		t.Fatalf("empty pageCount: want 1 got %d", page.PageCount)
	}
}

func TestAssembleDailyUpdateCategories(t *testing.T) {
	nav := []model.Category{
		{Id: 1, Name: "电影"},
		{Id: 2, Name: "剧集"},
		{Id: 3, Name: "动漫"},
	}
	countByPid := map[int64]int{1: 10, 3: 4}
	got := AssembleDailyUpdateCategories(nav, countByPid, 3, 17)
	if len(got) != 4 {
		t.Fatalf("want 4 items (全部+电影+动漫+其他), got %+v", got)
	}
	if got[0].Pid != notify.DailyPidAll || got[0].Name != "全部" || got[0].Count != 17 {
		t.Fatalf("all: %+v", got[0])
	}
	if got[1].Pid != 1 || got[1].Count != 10 {
		t.Fatalf("movie: %+v", got[1])
	}
	if got[2].Pid != 3 || got[2].Name != "动漫" {
		t.Fatalf("anime: %+v", got[2])
	}
	if got[3].Pid != notify.DailyPidOther || got[3].Count != 3 {
		t.Fatalf("other: %+v", got[3])
	}
}

func TestAssembleDailyUpdateCategoriesEmpty(t *testing.T) {
	got := AssembleDailyUpdateCategories(nil, nil, 0, 0)
	if len(got) != 1 || got[0].Pid != 0 || got[0].Count != 0 {
		t.Fatalf("want only 全部 0, got %+v", got)
	}
}
