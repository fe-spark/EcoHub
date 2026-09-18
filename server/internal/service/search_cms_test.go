package service

import (
	"testing"

	"server/internal/model"
	"server/internal/model/dto"
)

func TestCoerceCMSInt(t *testing.T) {
	if got := coerceCMSInt("3", 1); got != 3 {
		t.Fatalf("string page, got %d", got)
	}
	if got := coerceCMSInt(float64(2), 1); got != 2 {
		t.Fatalf("float page, got %d", got)
	}
	if got := coerceCMSInt("", 4); got != 4 {
		t.Fatalf("fallback, got %d", got)
	}
}

func TestApplyCMSPage(t *testing.T) {
	page := &dto.Page{Current: 1, PageSize: 12}
	applyCMSPage(page, model.FilmListPage{Page: "2", PageCount: 5, Limit: "20", Total: 80}, 20)
	if page.Current != 2 || page.PageSize != 20 || page.PageCount != 5 || page.Total != 80 {
		t.Fatalf("unexpected page: %+v", page)
	}
}

func TestResolveCMSMediaURL(t *testing.T) {
	base := "https://api.example.com/api.php/provide/vod/"
	if got := resolveCMSMediaURL("/upload/a.jpg", base); got != "https://api.example.com/upload/a.jpg" {
		t.Fatalf("relative path: %s", got)
	}
	if got := resolveCMSMediaURL("//cdn.example.com/a.jpg", base); got != "https://cdn.example.com/a.jpg" {
		t.Fatalf("protocol-relative: %s", got)
	}
	if got := resolveCMSMediaURL("https://cdn.example.com/a.jpg", base); got != "https://cdn.example.com/a.jpg" {
		t.Fatalf("absolute: %s", got)
	}
}

func TestApplyLocalSnapshotToCMSCard(t *testing.T) {
	card := model.MovieBasicInfo{Name: "仙逆", Picture: ""}
	applyLocalSnapshotToCMSCard(&card, model.FilmListSnapshot{Mid: 9, Picture: "https://local/p.jpg", Year: 2024})
	if card.Picture != "https://local/p.jpg" || card.Year != "2024" {
		t.Fatalf("snapshot overlay failed: %+v", card)
	}
}

func TestSetSearchSourceCount(t *testing.T) {
	tabs := []model.SearchSourceTab{{Id: "", Name: "聚合"}, {Id: "s1", Name: "源1"}}
	setSearchSourceCount(tabs, "", 9)
	if tabs[0].Count != 9 {
		t.Fatalf("聚合 count=%d", tabs[0].Count)
	}
	setSearchSourceCount(tabs, "s1", 4)
	if tabs[1].Count != 4 {
		t.Fatalf("源 count=%d", tabs[1].Count)
	}
}

func TestFillMissingCMSSearchPicturesSkipsSnapshots(t *testing.T) {
	list := []model.FilmList{
		{VodID: 10, VodName: "仙逆", VodPic: ""},
	}
	localBySourceMid := map[int64]int64{10: 99}
	snapByMid := map[int64]model.FilmListSnapshot{
		99: {Mid: 99, Picture: "http://local/pic.jpg"},
	}
	// uri 指向无效地址，若触发详情拉取会报错或失败；命中本地快照则应直接跳过，保留空 VodPic 由后续 snapshot 覆盖
	fillMissingCMSSearchPictures("http://invalid.local.domain", list, localBySourceMid, snapByMid)
	if list[0].VodPic != "" {
		t.Fatalf("expected untouched VodPic, got %s", list[0].VodPic)
	}
}

func TestSearchFilmResult_SingleSourcePreservesSources(t *testing.T) {
	svc := &IndexService{}
	page := &dto.Page{Current: 1, PageSize: 12}
	res := svc.SearchFilm("test", "non_existent_source", "", page)
	if len(res.Sources) == 0 {
		t.Fatalf("expected sources to be preserved for single source search, got 0")
	}
}
