package service

import (
	"encoding/json"
	"testing"

	"server/internal/model"
	"server/internal/repository"
)

// 首页 /home 的 banners 由 model.Banner 直接序列化，Web、Android、鸿蒙客户端都按这些键读取。
func TestBannerJSONContract_HomeKeys(t *testing.T) {
	b := model.Banner{
		Id: "b1", Mid: 1, Name: "影片", Year: 2026, CName: "电影",
		Poster: "p", Picture: "pic", PictureSlide: "slide", Remark: "HD",
		Blurb: "简介", Area: "中国大陆", Score: 8.1, ClassTag: "剧情",
		Actor: "演员", Director: "导演",
	}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"id", "mid", "name", "year", "cName", "poster", "picture", "pictureSlide",
		"remark", "blurb", "area", "score", "classTag", "actor", "director",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("banner json missing client key %q: %s", key, raw)
		}
	}
	for _, key := range []string{"isPinned", "source"} {
		if _, ok := m[key]; ok {
			t.Errorf("banner json should not contain removed key %q", key)
		}
	}
}

// 跟随影片封面的行即使带横版图，也不能被当成自定义图片锁住。
func TestSaveBannerDeck_FollowRowWithSlideStaysFollow(t *testing.T) {
	setupBackupTestDB(t)

	deck := model.Banners{
		{Id: "follow", Mid: 1, Name: "跟随", Picture: "p1", PictureSlide: "s1"},
		{Id: "scraped", Mid: 2, Name: "已刮削", Picture: "p2", PictureSlide: "s2", TmdbID: 99},
	}
	if err := ManageSvc.SaveBannerDeck(deck); err != nil {
		t.Fatalf("save deck: %v", err)
	}
	bl, err := repository.GetBanners()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]model.Banner{}
	for _, b := range bl {
		got[b.Id] = b
	}
	if got["follow"].IsCustomPic {
		t.Fatalf("follow row should keep isCustomPic=false: %+v", got["follow"])
	}
	if !got["scraped"].IsCustomPic {
		t.Fatalf("scraped row should be custom: %+v", got["scraped"])
	}
}

func TestMergeBannerUpdate_KeepsMetadata(t *testing.T) {
	old := model.Banner{
		Id: "b1", Mid: 1, Sort: 3, Area: "中国大陆", ClassTag: "剧情",
		Actor: "演员", Director: "导演", Blurb: "简介", Score: 7.5, TmdbID: 10,
	}
	// 编辑弹窗只回传了名称和图片。
	got := MergeBannerUpdate(old, model.Banner{Id: "b1", Mid: 1, Name: "新名称", Picture: "new"})
	if got.Area != "中国大陆" || got.ClassTag != "剧情" || got.Actor != "演员" || got.Director != "导演" {
		t.Fatalf("metadata should be kept: %+v", got)
	}
	if got.Blurb != "简介" || got.Score != 7.5 || got.TmdbID != 10 || got.Sort != 3 {
		t.Fatalf("blurb/score/tmdb/sort should be kept: %+v", got)
	}
	if got.Name != "新名称" || got.Picture != "new" {
		t.Fatalf("incoming fields should win: %+v", got)
	}

	// 提交了新值时用新值。
	got = MergeBannerUpdate(old, model.Banner{Id: "b1", Mid: 1, Area: "美国"})
	if got.Area != "美国" {
		t.Fatalf("incoming area should win: %+v", got)
	}

	// 换了影片时不沿用旧影片的信息。
	got = MergeBannerUpdate(old, model.Banner{Id: "b1", Mid: 2, Name: "另一部"})
	if got.Actor != "" || got.Director != "" || got.TmdbID != 0 {
		t.Fatalf("replaced film should not inherit old metadata: %+v", got)
	}
	if got.Sort != 3 {
		t.Fatalf("sort should be kept: %+v", got)
	}
}
