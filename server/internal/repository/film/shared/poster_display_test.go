package shared

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestApplyPosterSourcePictures_PrefersPosterSource(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.FilmIndex{}, &model.MovieMatchKey{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	gdb.Create(&model.FilmSource{Id: "main", Name: "主站", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "poster", Name: "海报站", Uri: "http://b", State: true, Sort: 1, IsPosterSource: true})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "main", DbId: 88},
		FilmIndexContent:  model.FilmIndexContent{Name: "流浪地球", Picture: "https://a/own.jpg", PictureSlide: "https://a/slide.jpg"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "poster", DbId: 88},
		FilmIndexContent:  model.FilmIndexContent{Name: "流浪地球", Picture: "https://b/poster.jpg", PictureSlide: "https://b/slide.jpg"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "main"},
		FilmIndexContent:  model.FilmIndexContent{Name: "锁定片", Picture: "https://a/raw.jpg", CustomPicture: "https://user/lock.jpg", IsCustomPicture: true},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 4, FirstSourceId: "poster", DbId: 88},
		FilmIndexContent:  model.FilmIndexContent{Name: "流浪地球", Picture: "https://b/self.jpg"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 5, FirstSourceId: "main", DbId: 77},
		FilmIndexContent:  model.FilmIndexContent{Name: "自定义片", Picture: "https://a/raw.jpg", CustomPicture: "https://a/lock.jpg", IsCustomPicture: true},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 6, FirstSourceId: "poster", DbId: 77},
		FilmIndexContent: model.FilmIndexContent{
			Name: "自定义片", Picture: "https://b/raw.jpg", PictureSlide: "https://b/raw-slide.jpg",
			CustomPicture: "https://b/final.jpg", CustomPictureSlide: "https://b/final-slide.jpg", IsCustomPicture: true,
		},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
	})
	for _, item := range []struct {
		mid  int64
		dbID int64
		name string
	}{
		{1, 88, "流浪地球"}, {2, 88, "流浪地球"}, {4, 88, "流浪地球"},
		{5, 77, "自定义片"}, {6, 77, "自定义片"},
	} {
		for _, key := range BuildMovieMatchKeysWithCategory(item.dbID, item.name, 1) {
			gdb.Create(&model.MovieMatchKey{Mid: item.mid, MatchKey: key})
		}
	}

	got := ApplyPosterSourcePictures([]model.FilmListSnapshot{
		{Mid: 1, SourceId: "main", DbId: 88, Name: "流浪地球", Pid: 1, Picture: "https://a/own.jpg", PictureSlide: "https://a/slide.jpg"},
		{Mid: 3, SourceId: "main", Name: "锁定片", Picture: "https://a/raw.jpg", CustomPicture: "https://user/lock.jpg", IsCustomPicture: true},
		{Mid: 4, SourceId: "poster", DbId: 88, Name: "流浪地球", Pid: 1, Picture: "https://b/self.jpg"},
		{Mid: 5, SourceId: "main", DbId: 77, Name: "自定义片", Pid: 1, Picture: "https://a/raw.jpg", CustomPicture: "https://a/lock.jpg", IsCustomPicture: true},
	})
	if got[0].Picture != "https://b/poster.jpg" || got[0].PictureSlide != "https://b/slide.jpg" {
		t.Fatalf("primary cover=%s slide=%s", got[0].Picture, got[0].PictureSlide)
	}
	if got[1].Picture != "https://a/raw.jpg" || got[1].DisplayPicture() != "https://user/lock.jpg" {
		t.Fatalf("unmatched lock changed: %+v", got[1])
	}
	if got[2].Picture != "https://b/self.jpg" {
		t.Fatalf("poster source should keep its own cover, got %s", got[2].Picture)
	}
	if got[3].DisplayPicture() != "https://b/final.jpg" || got[3].DisplayPictureSlide() != "https://b/final-slide.jpg" || got[3].IsCustomPicture {
		t.Fatalf("locked main should show poster source final: %+v", got[3])
	}
}

func TestApplyPosterSourcePictures_PrefersPosterSourceScrape(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.FilmIndex{}, &model.MovieMatchKey{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	gdb.Create(&model.FilmSource{Id: "main", Name: "主站", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "poster", Name: "海报站", Uri: "http://b", State: true, Sort: 1, IsPosterSource: true})
	seedPosterPair(gdb, 1, 2, 88, "流浪地球", "https://a/own.jpg", "https://b/raw.jpg", "https://b/raw-slide.jpg")
	seedPosterPair(gdb, 3, 4, 89, "简介片", "https://a/brief.jpg", "https://b/brief.jpg", "")
	seedPosterPair(gdb, 5, 6, 90, "停用片", "https://a/off.jpg", "https://b/off-raw.jpg", "")
	seedPosterPair(gdb, 7, 8, 91, "空图片", "https://a/empty.jpg", "", "")
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 2).Updates(map[string]any{
		"custom_picture": "https://tmdb/p.jpg", "custom_picture_slide": "https://tmdb/b.jpg", "is_custom_picture": true,
	})
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 8).Updates(map[string]any{
		"custom_picture": "https://tmdb/empty.jpg", "is_custom_picture": true,
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 9, FirstSourceId: "main", DbId: 92},
		FilmIndexContent:  model.FilmIndexContent{Name: "锁定片", Picture: "https://a/raw.jpg", CustomPicture: "https://a/lock.jpg", IsCustomPicture: true},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 10, FirstSourceId: "poster", DbId: 92},
		FilmIndexContent: model.FilmIndexContent{
			Name: "锁定片", Picture: "https://b/raw.jpg",
			CustomPicture: "https://tmdb/final.jpg", CustomPictureSlide: "https://tmdb/final-slide.jpg", IsCustomPicture: true,
		},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
	})
	for _, mid := range []int64{9, 10} {
		for _, key := range BuildMovieMatchKeysWithCategory(92, "锁定片", 1) {
			gdb.Create(&model.MovieMatchKey{Mid: mid, MatchKey: key})
		}
	}

	got := ApplyPosterSourcePictures([]model.FilmListSnapshot{
		{Mid: 1, SourceId: "main", DbId: 88, Name: "流浪地球", Pid: 1, Picture: "https://a/own.jpg", PictureSlide: "https://a/slide.jpg", CustomPicture: "https://a/lock.jpg", IsCustomPicture: true},
		{Mid: 3, SourceId: "main", DbId: 89, Name: "简介片", Pid: 1, Picture: "https://a/brief.jpg", PictureSlide: "https://a/brief-slide.jpg"},
		{Mid: 5, SourceId: "main", DbId: 90, Name: "停用片", Pid: 1, Picture: "https://a/off.jpg"},
		{Mid: 7, SourceId: "main", DbId: 91, Name: "空图片", Pid: 1, Picture: "https://a/empty.jpg", PictureSlide: "https://a/raw-slide.jpg", CustomPictureSlide: "https://a/custom-slide.jpg", IsCustomPicture: true},
		{Mid: 9, SourceId: "main", DbId: 92, Name: "锁定片", Pid: 1, Picture: "https://a/raw.jpg", CustomPicture: "https://a/lock.jpg", IsCustomPicture: true},
		{Mid: 11, SourceId: "poster", DbId: 88, Name: "流浪地球", Pid: 1, Picture: "https://b/self-scraped.jpg", PictureSlide: "https://b/self-slide.jpg"},
	})
	if got[0].DisplayPicture() != "https://tmdb/p.jpg" || got[0].DisplayPictureSlide() != "https://tmdb/b.jpg" || got[0].IsCustomPicture {
		t.Fatalf("locked main scrape cover=%s slide=%s custom=%v", got[0].DisplayPicture(), got[0].DisplayPictureSlide(), got[0].IsCustomPicture)
	}
	if got[1].Picture != "https://b/brief.jpg" || got[1].PictureSlide != "https://a/brief-slide.jpg" {
		t.Fatalf("empty poster slide must keep local slide, picture=%s slide=%s", got[1].Picture, got[1].PictureSlide)
	}
	if got[2].Picture != "https://b/off-raw.jpg" {
		t.Fatalf("disabled scrape must be ignored, got %s", got[2].Picture)
	}
	if got[3].Picture != "https://tmdb/empty.jpg" || got[3].PictureSlide != "https://a/custom-slide.jpg" {
		t.Fatalf("empty poster slide must keep local custom slide, picture=%s slide=%s", got[3].Picture, got[3].PictureSlide)
	}
	if got[4].DisplayPicture() != "https://tmdb/final.jpg" || got[4].DisplayPictureSlide() != "https://tmdb/final-slide.jpg" {
		t.Fatalf("poster source scrape should replace its custom cover, got %+v", got[4])
	}
	if got[5].Picture != "https://b/self-scraped.jpg" || got[5].PictureSlide != "https://b/self-slide.jpg" {
		t.Fatalf("poster source film changed: cover=%s slide=%s", got[5].Picture, got[5].PictureSlide)
	}
}

func seedPosterPair(gdb *gorm.DB, mainMid, posterMid, dbID int64, name, mainPic, posterPic, posterSlide string) {
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: mainMid, FirstSourceId: "main", DbId: dbID},
		FilmIndexContent:  model.FilmIndexContent{Name: name, Picture: mainPic},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: posterMid, FirstSourceId: "poster", DbId: dbID},
		FilmIndexContent:  model.FilmIndexContent{Name: name, Picture: posterPic, PictureSlide: posterSlide},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
	})
	for _, mid := range []int64{mainMid, posterMid} {
		for _, key := range BuildMovieMatchKeysWithCategory(dbID, name, 1) {
			gdb.Create(&model.MovieMatchKey{Mid: mid, MatchKey: key})
		}
	}
}
