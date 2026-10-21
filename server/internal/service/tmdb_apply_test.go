package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTMDBApplyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.FilmIndex{},
		&model.SearchTagItem{},
		&model.TMDBConfigRecord{},
		&model.ProxyConfigRecord{},
		&model.FilmTMDBScrape{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	origMdb := db.Mdb
	db.Mdb = gdb
	filmsnapshot.ClearActiveFilmReadModel()
	filmsnapshot.ClearActiveSnapshotVersion()

	t.Cleanup(func() {
		db.Mdb = origMdb
		filmsnapshot.ClearActiveFilmReadModel()
		filmsnapshot.ClearActiveSnapshotVersion()
	})
	return gdb
}

func TestApplyDetailWithOptions_CategoryIsolation(t *testing.T) {
	setupTMDBApplyTestDB(t)

	// 1. 初始化原始影片
	initialFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:           1001,
			FirstSourceId: "station_test",
			DbId:          123456,
		},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid:              1,
			Cid:              10,
			RootCategoryKey:  "movie",
			CategoryKey:      "sci_fi",
			OriginalCategory: "科幻片",
			CName:            "科幻",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name:         "测试电影",
			Picture:      "https://example.com/old_poster.jpg",
			PictureSlide: "https://example.com/old_slide.jpg",
			Year:         2020,
			Score:        7.0,
			Actor:        "原演员",
			Director:     "原导演",
			Content:      "原剧情简介",
			ClassTag:     "动作",
		},
	}
	if err := db.Mdb.Create(&initialFilm).Error; err != nil {
		t.Fatalf("create initial film: %v", err)
	}

	// 2. 启动 mock TMDB 服务
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := tmdbDetailRaw{
			rawTMDBItem: rawTMDBItem{
				ID:            9999,
				Title:         "测试电影",
				OriginalTitle: "Test Movie Original",
				PosterPath:    "/new_poster.jpg",
				BackdropPath:  "/new_backdrop.jpg",
				ReleaseDate:   "2023-05-20",
				VoteAverage:   8.8,
				Overview:      "这是TMDB抓取的全新高清剧情梗概",
			},
		}
		resp.Genres = []struct {
			Name string `json:"name"`
		}{
			{Name: "科幻"},
			{Name: "冒险"},
		}
		resp.Credits.Cast = []struct {
			Name string `json:"name"`
		}{
			{Name: "新演员A"},
			{Name: "新演员B"},
		}
		resp.Credits.Crew = []tmdbCreditPerson{
			{Name: "新导演A", Job: "Director"},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// 保存并切换 mock URL 与配置
	origURL := tmdbAPIBaseURL
	tmdbAPIBaseURL = ts.URL
	defer func() {
		tmdbAPIBaseURL = origURL
	}()

	_ = repository.SaveTMDBConfig(model.TMDBConfig{
		Enabled:     true,
		ApiKey:      "mock_api_key",
		Language:    "zh-CN",
		ImageDomain: "https://image.tmdb.org/t/p",
	})

	// 3. 执行刮削应用
	applyReq := model.TMDBApplyReq{
		Mid:       1001,
		TmdbID:    9999,
		MediaType: "movie",
		Fields:    []string{}, // 应用全部元数据
	}
	mid, err := TMDBSvc.ApplyDetailWithOptions(applyReq, false)
	if err != nil {
		t.Fatalf("ApplyDetailWithOptions failed: %v", err)
	}
	if mid != 1001 {
		t.Fatalf("expected mid=1001, got %d", mid)
	}

	var rawFilm model.FilmIndex
	if err := db.Mdb.Where("mid = ?", 1001).First(&rawFilm).Error; err != nil {
		t.Fatalf("fetch raw film failed: %v", err)
	}

	if rawFilm.Pid != 1 || rawFilm.Cid != 10 || rawFilm.Name != "测试电影" {
		t.Errorf("identity changed: pid=%d cid=%d name=%s", rawFilm.Pid, rawFilm.Cid, rawFilm.Name)
	}
	if rawFilm.Picture != "https://example.com/old_poster.jpg" {
		t.Errorf("collect picture should stay, got %q", rawFilm.Picture)
	}
	expectedPoster := "https://image.tmdb.org/t/p/w780/new_poster.jpg"
	if !rawFilm.IsCustomPicture || rawFilm.CustomPicture != expectedPoster {
		t.Errorf("custom picture=%q custom=%v", rawFilm.CustomPicture, rawFilm.IsCustomPicture)
	}
	if rawFilm.Content != "这是TMDB抓取的全新高清剧情梗概" || rawFilm.Year != 2023 || rawFilm.Actor != "新演员A/新演员B" {
		t.Errorf("written film content=%q year=%d actor=%q", rawFilm.Content, rawFilm.Year, rawFilm.Actor)
	}
	if !model.HasLock(rawFilm.LockedFields, model.LockOverview) || !model.HasLock(rawFilm.LockedFields, model.LockPoster) {
		t.Errorf("kept fields=%q", rawFilm.LockedFields)
	}

	var scrapeRec model.FilmTMDBScrape
	if err := db.Mdb.Where("mid = ?", 1001).First(&scrapeRec).Error; err != nil {
		t.Fatalf("fetch film_tmdb_scrapes failed: %v", err)
	}
	if scrapeRec.TmdbID != 9999 || scrapeRec.MediaType != "movie" {
		t.Errorf("scrape link mismatch: %+v", scrapeRec)
	}

	snap, err := filmsnapshot.GetSnapshotByMid("", 1001)
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatalf("GetSnapshotByMid returned nil")
	}
	if !snap.IsScraped || snap.TmdbID != 9999 {
		t.Errorf("snapshot scrape flag mismatch: isScraped=%v tmdbId=%d", snap.IsScraped, snap.TmdbID)
	}
	if snap.DisplayPicture() != expectedPoster || snap.Content != rawFilm.Content || snap.Score != 8.8 {
		t.Errorf("snapshot display=%s content=%q score=%v", snap.DisplayPicture(), snap.Content, snap.Score)
	}
}

func TestApplyDetailWithOptions_SelectiveFields(t *testing.T) {
	setupTMDBApplyTestDB(t)

	initialFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid: 1002,
		},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid: 2,
			Cid: 20,
		},
		FilmIndexContent: model.FilmIndexContent{
			Name:    "单字段测试",
			Picture: "https://example.com/stay_same.jpg",
			Content: "旧内容",
			Year:    1999,
		},
	}
	if err := db.Mdb.Create(&initialFilm).Error; err != nil {
		t.Fatalf("create initial film: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := tmdbDetailRaw{
			rawTMDBItem: rawTMDBItem{
				ID:          9998,
				Title:       "单字段测试",
				PosterPath:  "/changed_poster.jpg",
				Overview:    "改动后的内容",
				ReleaseDate: "2025-01-01",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	origURL := tmdbAPIBaseURL
	tmdbAPIBaseURL = ts.URL
	defer func() {
		tmdbAPIBaseURL = origURL
	}()

	_ = repository.SaveTMDBConfig(model.TMDBConfig{
		Enabled:     true,
		ApiKey:      "mock_api_key",
		Language:    "zh-CN",
		ImageDomain: "https://image.tmdb.org/t/p",
	})

	// 仅选择应用 overview
	applyReq := model.TMDBApplyReq{
		Mid:       1002,
		TmdbID:    9998,
		MediaType: "movie",
		Fields:    []string{"overview"},
	}
	_, err := TMDBSvc.ApplyDetailWithOptions(applyReq, false)
	if err != nil {
		t.Fatalf("ApplyDetailWithOptions failed: %v", err)
	}

	var rawFilm model.FilmIndex
	if err := db.Mdb.Where("mid = ?", 1002).First(&rawFilm).Error; err != nil {
		t.Fatalf("fetch raw film: %v", err)
	}
	if rawFilm.Content != "改动后的内容" || rawFilm.Blurb != "改动后的内容" {
		t.Errorf("overview should be written, content=%q blurb=%q", rawFilm.Content, rawFilm.Blurb)
	}
	if rawFilm.Picture != "https://example.com/stay_same.jpg" || rawFilm.IsCustomPicture || rawFilm.Year != 1999 {
		t.Errorf("unselected fields changed: picture=%q custom=%v year=%d", rawFilm.Picture, rawFilm.IsCustomPicture, rawFilm.Year)
	}
	if rawFilm.LockedFields != model.LockOverview {
		t.Errorf("kept fields=%q", rawFilm.LockedFields)
	}

	snap, err := filmsnapshot.GetSnapshotByMid("", 1002)
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatalf("snapshot is nil")
	}
	if snap.Content != "改动后的内容" || snap.Picture != "https://example.com/stay_same.jpg" || snap.Year != 1999 {
		t.Errorf("snapshot content=%q picture=%q year=%d", snap.Content, snap.Picture, snap.Year)
	}
	if !snap.IsScraped || snap.TmdbID != 9998 {
		t.Errorf("snapshot scrape metadata mismatch")
	}
}
