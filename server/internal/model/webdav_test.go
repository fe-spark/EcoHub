package model

import (
	"encoding/json"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestWebdavConfigSerialization(t *testing.T) {
	cfg := WebdavConfig{
		ServerURL:       "http://nas.local:5005/dav",
		Username:        "admin",
		Password:        "secret123",
		RootPath:        "/media/movies",
		MediaType:       "movie",
		TmdbApiKey:      "tmdb-test-key",
		TmdbBaseURL:     "https://api.themoviedb.org/3",
		ScanIntervalMin: 60,
		MinFileBytes:    52428800,
		PlayFromName:    "私有NAS高清",
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal WebdavConfig failed: %v", err)
	}

	var parsed WebdavConfig
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("Unmarshal WebdavConfig failed: %v", err)
	}

	if parsed != cfg {
		t.Fatalf("Parsed WebdavConfig mismatch: got %+v, want %+v", parsed, cfg)
	}

	// 验证 FilmSource 嵌套序列化（用于配置备份与恢复）
	source := FilmSource{
		Id:           "src_wdv_001",
		Name:         "我的NAS电影",
		Uri:          "webdav|http://nas.local:5005/dav|/media/movies",
		Grade:        SlaveCollect,
		State:        true,
		SourceType:   SourceTypeWebDAV,
		WebdavConfig: string(raw),
	}

	sourceRaw, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("Marshal FilmSource failed: %v", err)
	}

	var parsedSource FilmSource
	if err := json.Unmarshal(sourceRaw, &parsedSource); err != nil {
		t.Fatalf("Unmarshal FilmSource failed: %v", err)
	}

	if parsedSource.SourceType != SourceTypeWebDAV {
		t.Fatalf("FilmSource SourceType mismatch: got %s, want %s", parsedSource.SourceType, SourceTypeWebDAV)
	}
	if parsedSource.WebdavConfig != string(raw) {
		t.Fatalf("FilmSource WebdavConfig mismatch: got %s, want %s", parsedSource.WebdavConfig, string(raw))
	}
}

func TestWebdavTableNames(t *testing.T) {
	if (WebdavMediaGroup{}).TableName() != TableWebdavMediaGroup {
		t.Fatalf("WebdavMediaGroup.TableName() = %s, want %s", (WebdavMediaGroup{}).TableName(), TableWebdavMediaGroup)
	}
	if (WebdavScanItem{}).TableName() != TableWebdavScanItem {
		t.Fatalf("WebdavScanItem.TableName() = %s, want %s", (WebdavScanItem{}).TableName(), TableWebdavScanItem)
	}
	if (WebdavScanReport{}).TableName() != TableWebdavScanReport {
		t.Fatalf("WebdavScanReport.TableName() = %s, want %s", (WebdavScanReport{}).TableName(), TableWebdavScanReport)
	}
}

func TestWebdavAutoMigrateAndCRUD(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Open sqlite in-memory failed: %v", err)
	}

	// 执行 AllModels 全局 AutoMigrate
	if err := db.AutoMigrate(AllModels...); err != nil {
		t.Fatalf("db.AutoMigrate(AllModels...) failed: %v", err)
	}

	// 验证 WebdavMediaGroup CRUD 与唯一索引
	group := WebdavMediaGroup{
		SourceId:  "src_001",
		GroupKey:  "tv:qingyunan_s1",
		GlobalMid: 1001,
		TmdbId:    93405,
		TmdbType:  "tv",
		Title:     "庆余年 第一季",
		Year:      2019,
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("Create WebdavMediaGroup failed: %v", err)
	}

	// 插入相同 source_id + group_key 应违反唯一索引
	dupGroup := WebdavMediaGroup{
		SourceId:  "src_001",
		GroupKey:  "tv:qingyunan_s1",
		GlobalMid: 1002,
		Title:     "庆余年 第一季重复",
	}
	if err := db.Create(&dupGroup).Error; err == nil {
		t.Fatalf("Create duplicate WebdavMediaGroup should fail due to unique index")
	}

	// 验证 WebdavScanItem CRUD 与唯一索引
	item := WebdavScanItem{
		SourceId:     "src_001",
		PathHash:     "hash_e01",
		RelPath:      "庆余年/Season 1/E01.mkv",
		Size:         1024 * 1024 * 500,
		LastModified: "2026-09-15T00:00:00Z",
		Fingerprint:  "fp_001",
		GroupKey:     "tv:qingyunan_s1",
		Title:        "庆余年",
		Year:         2019,
		Season:       1,
		Episode:      1,
		TmdbId:       93405,
		Status:       "scraped",
		Hint:         "",
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("Create WebdavScanItem failed: %v", err)
	}

	dupItem := WebdavScanItem{
		SourceId: "src_001",
		PathHash: "hash_e01",
		RelPath:  "庆余年/Season 1/E01_dup.mkv",
	}
	if err := db.Create(&dupItem).Error; err == nil {
		t.Fatalf("Create duplicate WebdavScanItem should fail due to unique index")
	}

	// 验证 WebdavScanReport CRUD
	now := time.Now()
	report := WebdavScanReport{
		SourceId:     "src_001",
		StartedAt:    now.Add(-time.Minute),
		FinishedAt:   now,
		Status:       "completed",
		Found:        10,
		Parsed:       10,
		TmdbHit:      10,
		Unmatched:    0,
		Skipped:      0,
		Saved:        10,
		Deleted:      0,
		Failed:       0,
		Truncated:    false,
		ErrorSummary: "",
	}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("Create WebdavScanReport failed: %v", err)
	}

	var fetchedReport WebdavScanReport
	if err := db.First(&fetchedReport, report.ID).Error; err != nil {
		t.Fatalf("Query WebdavScanReport failed: %v", err)
	}
	if fetchedReport.Status != "completed" || fetchedReport.Found != 10 {
		t.Fatalf("Fetched WebdavScanReport mismatch: %+v", fetchedReport)
	}
}
