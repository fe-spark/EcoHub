package writer

import "testing"

import "server/internal/model"

func TestOwnedFilmUpdates_SkipsLockedFields(t *testing.T) {
	existing := &model.FilmIndex{
		FilmIndexContent: model.FilmIndexContent{
			Name: "原名", Blurb: "刮削简介", Content: "刮削简介", Year: 2020, Score: 8,
			Picture: "https://raw", PictureSlide: "https://raw-slide",
			CustomPicture: "https://custom", IsCustomPicture: true,
			LockedFields: model.LockOverview + "," + model.LockPoster + "," + model.LockScore,
		},
	}
	next := *existing
	next.Name = "新名"
	next.Blurb = "采集简介"
	next.Content = "采集简介"
	next.Year = 2024
	next.Score = 1
	next.Picture = "https://new"
	next.PictureSlide = "https://new-slide"
	next.Actor = "新演员"

	updates := ownedFilmUpdates(existing, &next)
	for _, col := range []string{"blurb", "content", "score", "picture", "picture_slide"} {
		if _, ok := updates[col]; ok {
			t.Fatalf("locked column %s was updated: %+v", col, updates)
		}
	}
	if updates["name"] != "新名" || updates["year"] != int64(2024) || updates["actor"] != "新演员" {
		t.Fatalf("unlocked fields missing: %+v", updates)
	}
}

func TestOwnedFilmUpdates_SkipsEmptyIdentityAndPicture(t *testing.T) {
	existing := &model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{DbId: 36117912},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid: 3, Cid: 9, CName: "动漫", RootCategoryKey: "root", CategoryKey: "cat",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "仙逆", Picture: "https://a/p.jpg", PictureSlide: "https://a/s.jpg",
		},
	}
	blank := *existing
	blank.DbId = 0
	blank.Pid = 0
	blank.Cid = 0
	blank.CName = ""
	blank.RootCategoryKey = ""
	blank.CategoryKey = ""
	blank.Name = ""
	blank.Picture = ""
	blank.PictureSlide = ""
	updates := ownedFilmUpdates(existing, &blank)
	for _, col := range []string{"db_id", "pid", "cid", "c_name", "root_category_key", "category_key", "name", "picture", "picture_slide"} {
		if _, ok := updates[col]; ok {
			t.Fatalf("empty %s overwrote: %+v", col, updates)
		}
	}

	next := *existing
	next.DbId = 99
	next.Name = "仙逆年番"
	next.Pid = 4
	next.Picture = "https://a/p2.jpg"
	next.PictureSlide = ""
	updates = ownedFilmUpdates(existing, &next)
	if updates["db_id"] != int64(99) || updates["name"] != "仙逆年番" || updates["pid"] != int64(4) || updates["picture"] != "https://a/p2.jpg" {
		t.Fatalf("present fields missing: %+v", updates)
	}
	if _, ok := updates["picture_slide"]; ok {
		t.Fatalf("empty slide overwrote: %+v", updates)
	}
}
