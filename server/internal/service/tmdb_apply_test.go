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

	// 4. 验证分类与核心隔离字段绝未被修改
	var updatedFilm model.FilmIndex
	if err := db.Mdb.Where("mid = ?", 1001).First(&updatedFilm).Error; err != nil {
		t.Fatalf("fetch updated film failed: %v", err)
	}

	if updatedFilm.Pid != 1 {
		t.Errorf("pid corrupted: expected 1, got %d", updatedFilm.Pid)
	}
	if updatedFilm.Cid != 10 {
		t.Errorf("cid corrupted: expected 10, got %d", updatedFilm.Cid)
	}
	if updatedFilm.RootCategoryKey != "movie" {
		t.Errorf("rootCategoryKey corrupted: expected 'movie', got %q", updatedFilm.RootCategoryKey)
	}
	if updatedFilm.CategoryKey != "sci_fi" {
		t.Errorf("categoryKey corrupted: expected 'sci_fi', got %q", updatedFilm.CategoryKey)
	}
	if updatedFilm.OriginalCategory != "科幻片" {
		t.Errorf("originalCategory corrupted: expected '科幻片', got %q", updatedFilm.OriginalCategory)
	}
	if updatedFilm.FirstSourceId != "station_test" {
		t.Errorf("firstSourceId corrupted: expected 'station_test', got %q", updatedFilm.FirstSourceId)
	}

	// 5. 验证白名单展示字段成功生效
	expectedPoster := "https://image.tmdb.org/t/p/w780/new_poster.jpg"
	if updatedFilm.Picture != expectedPoster || updatedFilm.CustomPicture != expectedPoster {
		t.Errorf("picture mismatch: got picture=%q custom=%q", updatedFilm.Picture, updatedFilm.CustomPicture)
	}
	if !updatedFilm.IsCustomPicture {
		t.Errorf("is_custom_picture should be true for poster update")
	}

	expectedSlide := "https://image.tmdb.org/t/p/original/new_backdrop.jpg"
	if updatedFilm.PictureSlide != expectedSlide || updatedFilm.CustomPictureSlide != expectedSlide {
		t.Errorf("pictureSlide mismatch: got %q custom=%q", updatedFilm.PictureSlide, updatedFilm.CustomPictureSlide)
	}

	if updatedFilm.Content != "这是TMDB抓取的全新高清剧情梗概" {
		t.Errorf("content mismatch: got %q", updatedFilm.Content)
	}
	if updatedFilm.SubTitle != "Test Movie Original" {
		t.Errorf("subTitle mismatch: got %q", updatedFilm.SubTitle)
	}
	if updatedFilm.Actor != "新演员A/新演员B" {
		t.Errorf("actor mismatch: got %q", updatedFilm.Actor)
	}
	if updatedFilm.Director != "新导演A" {
		t.Errorf("director mismatch: got %q", updatedFilm.Director)
	}
	if updatedFilm.Year != 2023 {
		t.Errorf("year mismatch: expected 2023, got %d", updatedFilm.Year)
	}
	if updatedFilm.ReleaseDate != "2023-05-20" {
		t.Errorf("releaseDate mismatch: expected 2023-05-20, got %q", updatedFilm.ReleaseDate)
	}
	if updatedFilm.Score != 8.8 {
		t.Errorf("score mismatch: expected 8.8, got %f", updatedFilm.Score)
	}
	if updatedFilm.ClassTag != "科幻,冒险" {
		t.Errorf("classTag mismatch: expected '科幻,冒险', got %q", updatedFilm.ClassTag)
	}
	if updatedFilm.UpdateReason != "TMDB刮削" {
		t.Errorf("updateReason mismatch: got %q", updatedFilm.UpdateReason)
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

	var updatedFilm model.FilmIndex
	if err := db.Mdb.Where("mid = ?", 1002).First(&updatedFilm).Error; err != nil {
		t.Fatalf("fetch updated film: %v", err)
	}

	// 验证 overview 变了，但 picture 和 year 没有被覆盖
	if updatedFilm.Content != "改动后的内容" {
		t.Errorf("content should be updated, got %q", updatedFilm.Content)
	}
	if updatedFilm.Picture != "https://example.com/stay_same.jpg" {
		t.Errorf("picture should remain unchanged, got %q", updatedFilm.Picture)
	}
	if updatedFilm.Year != 1999 {
		t.Errorf("year should remain 1999, got %d", updatedFilm.Year)
	}
}
