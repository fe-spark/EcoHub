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

func TestApplyCMSDetailToCardJoinsByVodID(t *testing.T) {
	card := model.MovieBasicInfo{
		Name:      "仙逆",
		CName:     "国产剧",
		Remarks:   "第158集",
		SourceMid: 101,
	}
	applyCMSDetailToCard(&card, model.MovieDetail{
		Id:      101,
		Picture: "https://cdn.example.com/tv.jpg",
		MovieDescriptor: model.MovieDescriptor{
			CName:    "电视剧",
			ClassTag: "玄幻,热血",
			Year:     "2024",
			Actor:    "张三",
			Blurb:    "连载版简介",
			Remarks:  "全集完结",
		},
	}, "https://api.example.com/api.php/provide/vod/")
	if card.Picture != "https://cdn.example.com/tv.jpg" {
		t.Fatalf("picture from detail: %s", card.Picture)
	}
	if card.CName != "国产剧" {
		t.Fatalf("list type_name must win, got %q", card.CName)
	}
	if card.ClassTag != "玄幻,热血" {
		t.Fatalf("plot tag from detail vod_class, got %q", card.ClassTag)
	}
	if card.Remarks != "第158集" {
		t.Fatalf("list remarks must win, got %q", card.Remarks)
	}
	if card.Year != "2024" || card.Actor != "张三" {
		t.Fatalf("year/actor from detail: %+v", card)
	}
}

func TestMovieBasicInfoFromCMSListLeavesClassTagEmpty(t *testing.T) {
	card := movieBasicInfoFromCMSList(
		&model.FilmSource{Id: "jy", Uri: "https://jy.example.com/api.php/provide/vod/"},
		model.FilmList{VodID: 101, VodName: "仙逆", TypeID: 12, TypeName: "国产剧", VodRemarks: "第158集"},
	)
	if card.CName != "国产剧" {
		t.Fatalf("cName from list type_name, got %q", card.CName)
	}
	if card.ClassTag != "" {
		t.Fatalf("classTag must wait for detail vod_class, got %q", card.ClassTag)
	}
}

func TestApplyCMSDetailToCardDoesNotCopyCNameToClassTag(t *testing.T) {
	card := model.MovieBasicInfo{Name: "仙逆", CName: "国产剧", SourceMid: 101}
	applyCMSDetailToCard(&card, model.MovieDetail{
		Id: 101,
		MovieDescriptor: model.MovieDescriptor{CName: "电视剧"},
	}, "https://api.example.com/")
	if card.ClassTag != "" {
		t.Fatalf("empty vod_class must not copy 大类 into classTag, got %q", card.ClassTag)
	}
	if card.CName != "国产剧" {
		t.Fatalf("list type_name must stay, got %q", card.CName)
	}
}

func TestApplyCMSDetailToCardRejectsMismatchedID(t *testing.T) {
	card := model.MovieBasicInfo{Name: "仙逆", SourceMid: 101, Picture: ""}
	applyCMSDetailToCard(&card, model.MovieDetail{
		Id:              202,
		Picture:         "https://cdn.example.com/other.jpg",
		MovieDescriptor: model.MovieDescriptor{Year: "2016"},
	}, "https://api.example.com/")
	if card.Picture != "" || card.Year != "" {
		t.Fatalf("must not attach another vod_id: %+v", card)
	}
}

func TestSetSearchSourceCount(t *testing.T) {
	tabs := []model.SearchSourceTab{{Id: "", Name: "综合"}, {Id: "s1", Name: "源1"}}
	setSearchSourceCount(tabs, "", 9)
	if tabs[0].Count != 9 {
		t.Fatalf("综合 count=%d", tabs[0].Count)
	}
	setSearchSourceCount(tabs, "s1", 4)
	if tabs[1].Count != 4 {
		t.Fatalf("源 count=%d", tabs[1].Count)
	}
}

func TestFetchCMSSearchDetailsKeepsRequestedIDsOnly(t *testing.T) {
	orig := fetchSourceDetails
	t.Cleanup(func() { fetchSourceDetails = orig })
	fetchSourceDetails = func(uri, ids string) ([]model.MovieDetail, error) {
		if ids != "101,202" {
			t.Fatalf("ids=%q", ids)
		}
		return []model.MovieDetail{
			{Id: 101, Picture: "https://cdn.example.com/a.jpg", MovieDescriptor: model.MovieDescriptor{CName: "国产剧"}},
			{Id: 202, Picture: "https://cdn.example.com/b.jpg", MovieDescriptor: model.MovieDescriptor{CName: "电影"}},
			{Id: 999, Picture: "https://cdn.example.com/stray.jpg"},
		}, nil
	}
	got := fetchCMSSearchDetails("https://api.example.com/", []int64{101, 202})
	if len(got) != 2 || got[101].Picture == "" || got[202].Picture == "" {
		t.Fatalf("got %+v", got)
	}
	if _, ok := got[999]; ok {
		t.Fatalf("stray detail must be dropped")
	}
}

func TestSearchCollectSourceCMSJoinsDetailByVodIDNotName(t *testing.T) {
	origFind := findCollectSourceById
	origList := searchSourceList
	origDetail := fetchSourceDetails
	t.Cleanup(func() {
		findCollectSourceById = origFind
		searchSourceList = origList
		fetchSourceDetails = origDetail
	})

	findCollectSourceById = func(id string) *model.FilmSource {
		return &model.FilmSource{Id: id, Name: "金鹰2(JY)", Uri: "https://jy.example.com/api.php/provide/vod/", State: true}
	}
	searchSourceList = func(uri, keyword string, page int) (model.FilmListPage, error) {
		return model.FilmListPage{
			Code: 1, Page: 1, PageCount: 1, Limit: 20, Total: 2,
			List: []model.FilmList{
				{VodID: 101, VodName: "仙逆", TypeID: 12, TypeName: "国产剧", VodRemarks: "第158集"},
				{VodID: 202, VodName: "仙逆", TypeID: 1, TypeName: "电影", VodRemarks: "全集完结"},
			},
		}, nil
	}
	fetchSourceDetails = func(uri, ids string) ([]model.MovieDetail, error) {
		return []model.MovieDetail{
			{
				Id: 101, Picture: "https://cdn.example.com/tv.jpg",
				MovieDescriptor: model.MovieDescriptor{CName: "电视剧", ClassTag: "玄幻,热血", Year: "2024", Actor: "连载主演"},
			},
			{
				Id: 202, Picture: "https://cdn.example.com/movie.jpg",
				MovieDescriptor: model.MovieDescriptor{CName: "剧情片", ClassTag: "奇幻", Year: "2023", Actor: "电影主演"},
			},
		}, nil
	}

	list, page, errMsg := searchCollectSourceCMS("jy", "仙逆", 1)
	if errMsg != "" || page.Total != 2 || len(list) != 2 {
		t.Fatalf("err=%q page=%+v list=%+v", errMsg, page, list)
	}
	if list[0].SourceMid != 101 || list[0].Picture != "https://cdn.example.com/tv.jpg" || list[0].CName != "国产剧" || list[0].ClassTag != "玄幻,热血" || list[0].Remarks != "第158集" || list[0].Year != "2024" {
		t.Fatalf("tv card: %+v", list[0])
	}
	if list[1].SourceMid != 202 || list[1].Picture != "https://cdn.example.com/movie.jpg" || list[1].CName != "电影" || list[1].ClassTag != "奇幻" || list[1].Remarks != "全集完结" || list[1].Year != "2023" {
		t.Fatalf("movie card: %+v", list[1])
	}
	if list[0].Picture == list[1].Picture {
		t.Fatalf("same-name cards must not share a poster")
	}
}

func TestAssignCMSSearchLocalIDsKeepsOnlyMatchingSerial(t *testing.T) {
	animeSnap := model.FilmListSnapshot{
		Mid: 9, Name: "牧神记", CName: "中国动漫", Year: 2024, Remarks: "第100集",
	}
	cards := []model.MovieBasicInfo{
		{Name: "牧神记", CName: "国产动漫", Remarks: "第100集", Year: "2024", SourceId: "subo", SourceMid: 101},
		{Name: "牧神记", CName: "反转爽剧", Remarks: "第81-106集完结", Year: "2024", SourceId: "subo", SourceMid: 202},
	}
	assignCMSSearchLocalIDs(
		cards,
		map[int64]int64{101: 9, 202: 9},
		map[int64]model.FilmListSnapshot{9: animeSnap},
		nil,
	)
	if cards[0].Id != 9 {
		t.Fatalf("serial 牧神记 should keep local mid, got %d", cards[0].Id)
	}
	if cards[1].Id != 0 {
		t.Fatalf("packed 牧神记 must play live, got %d", cards[1].Id)
	}
}

func TestAssignCMSSearchLocalIDsDropsAmbiguousSharedMid(t *testing.T) {
	snap := model.FilmListSnapshot{Mid: 9, Name: "牧神记", Year: 2024, Remarks: "第100集"}
	cards := []model.MovieBasicInfo{
		{Name: "牧神记", Remarks: "第100集", Year: "2024", SourceMid: 101},
		{Name: "牧神记", Remarks: "第100集", Year: "2024", SourceMid: 202},
	}
	assignCMSSearchLocalIDs(
		cards,
		map[int64]int64{101: 9, 202: 9},
		map[int64]model.FilmListSnapshot{9: snap},
		nil,
	)
	if cards[0].Id != 0 || cards[1].Id != 0 {
		t.Fatalf("two vod_ids mapped to one mid must both go live, got %+v", cards)
	}
}

func TestAssignCMSSearchLocalIDsKeepsUniqueMatch(t *testing.T) {
	snap := model.FilmListSnapshot{Mid: 9, Name: "牧神记", Year: 2024, Remarks: "第100集"}
	cards := []model.MovieBasicInfo{
		{Name: "牧神记", Remarks: "第100集", Year: "2024", SourceMid: 101},
		{Name: "牧神记之延康变法", Remarks: "第41-61集完结", Year: "2025", SourceMid: 303},
	}
	assignCMSSearchLocalIDs(
		cards,
		map[int64]int64{101: 9},
		map[int64]model.FilmListSnapshot{9: snap},
		nil,
	)
	if cards[0].Id != 9 {
		t.Fatalf("unique mapping should keep mid, got %d", cards[0].Id)
	}
	if cards[1].Id != 0 {
		t.Fatalf("unmapped title must stay live, got %d", cards[1].Id)
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
	origDetail := fetchSourceDetails
	t.Cleanup(func() {
		findCollectSourceById = origFind
		searchSourceList = origList
		fetchSourceDetails = origDetail
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
	fetchSourceDetails = func(uri, ids string) ([]model.MovieDetail, error) {
		return nil, nil
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

func TestFetchCMSSearchDetailsWithProxy(t *testing.T) {
	origProxyDetail := fetchSourceDetailsWithProxy
	t.Cleanup(func() {
		fetchSourceDetailsWithProxy = origProxyDetail
	})

	calledProxy := ""
	fetchSourceDetailsWithProxy = func(uri, ids, proxyURL string) ([]model.MovieDetail, error) {
		calledProxy = proxyURL
		return []model.MovieDetail{
			{Id: 101, Picture: "https://cdn.example.com/proxy.jpg"},
		}, nil
	}

	got := fetchCMSSearchDetails("https://api.example.com/", []int64{101}, "http://127.0.0.1:7890")
	if calledProxy != "http://127.0.0.1:7890" {
		t.Fatalf("expected proxyURL passed, got %q", calledProxy)
	}
	if len(got) != 1 || got[101].Picture != "https://cdn.example.com/proxy.jpg" {
		t.Fatalf("unexpected detail: %+v", got)
	}
}

