package service

import (
	"errors"
	"fmt"
	"testing"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/spider"
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

func TestFormatCMSSearchError(t *testing.T) {
	if got := formatCMSSearchError(spider.ErrCMSSearchUnsupported); got != "暂不支持搜索" {
		t.Fatalf("unsupported: %q", got)
	}
	wrapped := fmt.Errorf("%w: %s", spider.ErrCMSSearchUnsupported, "暂不支持搜索")
	if got := formatCMSSearchError(wrapped); got != "暂不支持搜索" {
		t.Fatalf("wrapped unsupported: %q", got)
	}
	if got := formatCMSSearchError(errors.New("context deadline exceeded")); got != "源站超时" {
		t.Fatalf("timeout: %q", got)
	}
}

func TestSearchCollectSourceCMSReportsUnsupported(t *testing.T) {
	origFind := findCollectSourceById
	origList := searchSourceList
	t.Cleanup(func() {
		findCollectSourceById = origFind
		searchSourceList = origList
	})

	findCollectSourceById = func(id string) *model.FilmSource {
		return &model.FilmSource{Id: id, Name: "HD(SN)", Uri: "https://suoniapi.com/api.php/provide/vod", State: true}
	}
	searchSourceList = func(uri, keyword string, page int) (model.FilmListPage, error) {
		return model.FilmListPage{}, fmt.Errorf("%w: %s", spider.ErrCMSSearchUnsupported, "暂不支持搜索")
	}

	list, page, errMsg := searchCollectSourceCMS("sn", "2", 1)
	if len(list) != 0 || page.Total != 0 {
		t.Fatalf("expected empty result, list=%+v page=%+v", list, page)
	}
	if errMsg != "暂不支持搜索" {
		t.Fatalf("errMsg=%q", errMsg)
	}
}

func TestSearchCollectSourceCMSKeepsValidEmpty(t *testing.T) {
	origFind := findCollectSourceById
	origList := searchSourceList
	t.Cleanup(func() {
		findCollectSourceById = origFind
		searchSourceList = origList
	})

	findCollectSourceById = func(id string) *model.FilmSource {
		return &model.FilmSource{Id: id, Name: "非凡", Uri: "http://cj.ffzyapi.com/api.php/provide/vod/", State: true}
	}
	searchSourceList = func(uri, keyword string, page int) (model.FilmListPage, error) {
		return model.FilmListPage{Code: 1, Page: 1, PageCount: 0, Limit: 20, Total: 0, List: nil}, nil
	}

	list, page, errMsg := searchCollectSourceCMS("ff", "不存在的片名xyz", 1)
	if len(list) != 0 || page.Total != 0 || errMsg != "" {
		t.Fatalf("valid empty should not error, list=%+v page=%+v err=%q", list, page, errMsg)
	}
}

func TestSearchCollectSourceCMSUsesRemoteHits(t *testing.T) {
	origFind := findCollectSourceById
	origList := searchSourceList
	t.Cleanup(func() {
		findCollectSourceById = origFind
		searchSourceList = origList
	})

	findCollectSourceById = func(id string) *model.FilmSource {
		return &model.FilmSource{Id: id, Name: "速博", Uri: "https://subocaiji.com/api.php/provide/vod", State: true}
	}
	searchSourceList = func(uri, keyword string, page int) (model.FilmListPage, error) {
		return model.FilmListPage{
			Code:      1,
			Page:      1,
			PageCount: 1,
			Limit:     20,
			Total:     2,
			List:      []model.FilmList{{VodID: 11, VodName: "源站片"}},
		}, nil
	}

	list, page, errMsg := searchCollectSourceCMS("subo", "2", 1)
	if errMsg != "" {
		t.Fatalf("unexpected error %q", errMsg)
	}
	if len(list) != 1 || list[0].Name != "源站片" || list[0].SourceMid != 11 {
		t.Fatalf("cms list: %+v", list)
	}
	if page.Total != 2 {
		t.Fatalf("cms total: %+v", page)
	}
}
