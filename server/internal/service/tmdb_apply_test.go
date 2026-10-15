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

	// 4. 验证底层 film_index 原始采集数据绝未被修改篡改
	var rawFilm model.FilmIndex
	if err := db.Mdb.Where("mid = ?", 1001).First(&rawFilm).Error; err != nil {
		t.Fatalf("fetch raw film failed: %v", err)
	}

	if rawFilm.Pid != 1 || rawFilm.Cid != 10 {
		t.Errorf("pid/cid corrupted: got %d/%d", rawFilm.Pid, rawFilm.Cid)
	}
	if rawFilm.Picture != "https://example.com/old_poster.jpg" {
		t.Errorf("film_index picture should remain raw untouched, got %q", rawFilm.Picture)
	}
	if rawFilm.Content != "原剧情简介" {
		t.Errorf("film_index content should remain raw untouched, got %q", rawFilm.Content)
	}
	if rawFilm.Year != 2020 {
		t.Errorf("film_index year should remain raw untouched, got %d", rawFilm.Year)
	}

	// 5. 验证独立刮削持久化表 film_tmdb_scrapes 写入完整
	var scrapeRec model.FilmTMDBScrape
	if err := db.Mdb.Where("mid = ?", 1001).First(&scrapeRec).Error; err != nil {
		t.Fatalf("fetch film_tmdb_scrapes failed: %v", err)
	}
	if scrapeRec.TmdbID != 9999 || scrapeRec.Status != 1 {
		t.Errorf("scrape record mismatch: tmdbID=%d status=%d", scrapeRec.TmdbID, scrapeRec.Status)
	}
	expectedPoster := "https://image.tmdb.org/t/p/w780/new_poster.jpg"
	if scrapeRec.Poster != expectedPoster {
		t.Errorf("scrape poster mismatch: got %q, expected %q", scrapeRec.Poster, expectedPoster)
	}
	if scrapeRec.Overview != "这是TMDB抓取的全新高清剧情梗概" {
		t.Errorf("scrape overview mismatch: got %q", scrapeRec.Overview)
	}

	// 6. 验证快照读取层合成投影生效（TMDB 覆盖层优先）
	snap := filmsnapshot.GetSnapshotByMid("", 1001)
	if snap == nil {
		t.Fatalf("GetSnapshotByMid returned nil")
	}
	if !snap.IsScraped || snap.TmdbID != 9999 {
		t.Errorf("snapshot scrape flag mismatch: isScraped=%v tmdbId=%d", snap.IsScraped, snap.TmdbID)
	}
	if snap.Picture != expectedPoster || snap.CustomPicture != expectedPoster {
		t.Errorf("snapshot picture mismatch: got %q", snap.Picture)
	}
	if snap.Content != "这是TMDB抓取的全新高清剧情梗概" {
		t.Errorf("snapshot content mismatch: got %q", snap.Content)
	}
	if snap.Actor != "新演员A/新演员B" {
		t.Errorf("snapshot actor mismatch: got %q", snap.Actor)
	}
	if snap.Year != 2023 {
		t.Errorf("snapshot year mismatch: expected 2023, got %d", snap.Year)
	}
	if snap.Score != 8.8 {
		t.Errorf("snapshot score mismatch: expected 8.8, got %f", snap.Score)
	}

	// 7. 测试还原刮削 (Revert)
	if err := TMDBSvc.RevertDetail(1001); err != nil {
		t.Fatalf("RevertDetail failed: %v", err)
	}
	var countScrapes int64
	db.Mdb.Model(&model.FilmTMDBScrape{}).Where("mid = ?", 1001).Count(&countScrapes)
	if countScrapes != 0 {
		t.Errorf("film_tmdb_scrapes should be empty after revert, got %d", countScrapes)
	}

	// 还原后快照重新投影，无缝变回采集站原始数据
	revertedSnap := filmsnapshot.GetSnapshotByMid("", 1001)
	if revertedSnap == nil {
		t.Fatalf("reverted snapshot is nil")
	}
	if revertedSnap.IsScraped {
		t.Errorf("isScraped should be false after revert")
	}
	if revertedSnap.Picture != "https://example.com/old_poster.jpg" {
		t.Errorf("reverted picture mismatch: got %q, expected raw poster", revertedSnap.Picture)
	}
	if revertedSnap.Content != "原剧情简介" {
		t.Errorf("reverted content mismatch: got %q, expected raw content", revertedSnap.Content)
	}
	if revertedSnap.Year != 2020 {
		t.Errorf("reverted year mismatch: got %d, expected 2020", revertedSnap.Year)
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

	// 验证底层 film_index 仍然完整保留
	var rawFilm model.FilmIndex
	if err := db.Mdb.Where("mid = ?", 1002).First(&rawFilm).Error; err != nil {
		t.Fatalf("fetch raw film: %v", err)
	}
	if rawFilm.Content != "旧内容" || rawFilm.Picture != "https://example.com/stay_same.jpg" {
		t.Errorf("film_index was touched unexpectedly: content=%q picture=%q", rawFilm.Content, rawFilm.Picture)
	}

	// 验证快照层精准合成 overview，其余字段保持原样
	snap := filmsnapshot.GetSnapshotByMid("", 1002)
	if snap == nil {
		t.Fatalf("snapshot is nil")
	}
	if snap.Content != "改动后的内容" {
		t.Errorf("content should be updated in snapshot, got %q", snap.Content)
	}
	if snap.Picture != "https://example.com/stay_same.jpg" {
		t.Errorf("picture should remain unchanged, got %q", snap.Picture)
	}
	if snap.Year != 1999 {
		t.Errorf("year should remain 1999, got %d", snap.Year)
	}
	if !snap.IsScraped || snap.TmdbID != 9998 {
		t.Errorf("snapshot scrape metadata mismatch")
	}

	// 还原并检查
	if err := TMDBSvc.RevertDetail(1002); err != nil {
		t.Fatalf("revert failed: %v", err)
	}
	revertedSnap := filmsnapshot.GetSnapshotByMid("", 1002)
	if revertedSnap.Content != "旧内容" {
		t.Errorf("reverted content mismatch: got %q, expected '旧内容'", revertedSnap.Content)
	}
	if revertedSnap.IsScraped {
		t.Errorf("reverted isScraped should be false")
	}
}

func TestRevertDetails_BatchAndSingle(t *testing.T) {
	setupTMDBApplyTestDB(t)

	// 初始化两条已刮削数据
	db.Mdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2001, DbId: 1},
		FilmIndexContent:  model.FilmIndexContent{Name: "片1"},
	})
	db.Mdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2002, DbId: 2},
		FilmIndexContent:  model.FilmIndexContent{Name: "片2"},
	})
	db.Mdb.Create(&model.FilmTMDBScrape{
		Mid:    2001,
		TmdbID: 101,
		Status: 1,
	})
	db.Mdb.Create(&model.FilmTMDBScrape{
		Mid:    2002,
		TmdbID: 102,
		Status: 1,
	})

	// 1. 批量还原
	if err := TMDBSvc.RevertDetails([]int64{2001, 2002}); err != nil {
		t.Fatalf("batch revert failed: %v", err)
	}

	var count int64
	db.Mdb.Model(&model.FilmTMDBScrape{}).Where("mid IN ?", []int64{2001, 2002}).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 scrape records, got %d", count)
	}
}
