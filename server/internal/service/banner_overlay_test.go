package service

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOverlayBanner_KeepsBannerCopy(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}, &model.FilmSource{}, &model.Banner{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	prevRdb := db.Rdb
	db.Mdb = gdb
	db.Rdb = nil
	t.Cleanup(func() {
		db.Mdb = prev
		db.Rdb = prevRdb
	})

	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "main"},
		FilmIndexContent: model.FilmIndexContent{
			Name: "片", Remarks: "更新至3集", Blurb: "片库简介", Score: 6,
			Picture: "https://film/p.jpg",
		},
	})
	got := overlayBannerLiveRemarks(model.Banners{{
		Id: "b1", Mid: 1, Name: "片",
		Picture: "https://banner/p.jpg", PictureSlide: "https://banner/s.jpg",
		CustomPicture: "https://banner/p.jpg", IsCustomPic: true,
		Blurb: "轮播简介", Score: 8.8, Remark: "旧",
	}})
	if len(got) != 1 {
		t.Fatal(len(got))
	}
	item := got[0]
	if item.Remark != "更新至3集" {
		t.Fatalf("remark=%s", item.Remark)
	}
	if item.Blurb != "轮播简介" || item.Score != 8.8 {
		t.Fatalf("banner text changed: blurb=%s score=%v", item.Blurb, item.Score)
	}
	if item.Picture != "https://banner/p.jpg" || item.PictureSlide != "https://banner/s.jpg" {
		t.Fatalf("banner image changed: %+v", item)
	}

	plain := overlayBannerLiveRemarks(model.Banners{{
		Id: "b2", Mid: 1, Name: "片",
		Picture: "https://banner/plain.jpg", PictureSlide: "https://banner/plain-slide.jpg",
		Blurb: "轮播简介", Remark: "旧",
	}})
	if plain[0].Picture != "https://banner/plain.jpg" || plain[0].PictureSlide != "https://banner/plain-slide.jpg" || plain[0].Blurb != "轮播简介" || plain[0].Remark != "更新至3集" {
		t.Fatalf("plain banner followed film: %+v", plain[0])
	}
}

func TestSaveBannerDeck_StoresScrapeOnBanner(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}, &model.FilmTMDBScrape{}, &model.Banner{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	prevRdb := db.Rdb
	db.Mdb = gdb
	db.Rdb = nil
	t.Cleanup(func() {
		db.Mdb = prev
		db.Rdb = prevRdb
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 7, FirstSourceId: "main"},
		FilmIndexContent: model.FilmIndexContent{
			Name: "片", Picture: "https://film/raw.jpg", Blurb: "片库简介", Remarks: "更新至2集",
		},
	})

	err = ManageSvc.SaveBannerDeck(model.Banners{{
		Id: "b7", Mid: 7, Name: "片",
		Picture: "https://tmdb/p.jpg", PictureSlide: "https://tmdb/s.jpg", Poster: "https://tmdb/p.jpg",
		Blurb: "轮播简介", Score: 9.1, TmdbID: 42, PendingScrape: true,
	}})
	if err != nil {
		t.Fatal(err)
	}

	var row model.Banner
	if err := gdb.Where("id = ?", "b7").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.TmdbID != 42 || row.Picture != "https://tmdb/p.jpg" || row.PictureSlide != "https://tmdb/s.jpg" || row.Blurb != "轮播简介" || row.Score != 9.1 || !row.IsCustomPic {
		t.Fatalf("banner row=%+v", row)
	}
	var scrapeCount int64
	gdb.Model(&model.FilmTMDBScrape{}).Count(&scrapeCount)
	if scrapeCount != 0 {
		t.Fatalf("scrape rows=%d", scrapeCount)
	}
	var film model.FilmIndex
	if err := gdb.Where("mid = ?", 7).First(&film).Error; err != nil {
		t.Fatal(err)
	}
	if film.Picture != "https://film/raw.jpg" || film.Blurb != "片库简介" {
		t.Fatalf("film changed: picture=%s blurb=%s", film.Picture, film.Blurb)
	}

	shown := overlayBannerLiveRemarks(model.Banners{row})
	if shown[0].Picture != "https://tmdb/p.jpg" || shown[0].Blurb != "轮播简介" || shown[0].Remark != "更新至2集" || !shown[0].IsScraped {
		t.Fatalf("shown=%+v", shown[0])
	}
}
