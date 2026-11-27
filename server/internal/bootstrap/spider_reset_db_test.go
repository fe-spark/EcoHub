package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"server/internal/access"
	"server/internal/collect"
	"server/internal/config"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"server/internal/collectctl"
	"server/internal/siteops"
)

func waitResetSearchWorkIdle(t *testing.T) {
	t.Helper()
	if !collectctl.WaitAsyncWork(15 * time.Second) {
		t.Fatal("primary warmup / category sync did not finish")
	}
	deadline := time.Now().Add(15 * time.Second)
	for collectctl.IsPrimarySiteSwitching() || filmsnapshot.SearchRebuildScheduled() {
		if time.Now().After(deadline) {
			t.Fatal("search index rebuild / primary warmup did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	filmsnapshot.WaitActiveFilmSearchIndexBuilt()
}

func TestClearFilms_ClearsDatabaseAndReinitializesDefaults(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	waitResetSearchWorkIdle(t)

	prevDB := db.Mdb
	prevRdb := db.Rdb
	prevUploadDir := config.FilmPictureUploadDir
	prevSyncFn := siteops.GetTriggerResetCategorySync()
	prevAccess := access.IsEnabled()

	uploadDir := t.TempDir()
	db.Mdb = gdb
	db.Rdb = nil
	config.FilmPictureUploadDir = uploadDir
	access.SetEnabled(true)

	catSyncCalled := false
	siteops.SetTriggerResetCategorySync(func() {
		catSyncCalled = true
	})

	t.Cleanup(func() {
		waitResetSearchWorkIdle(t)
		collect.ClearAllCrons()
		collect.ResetAllThrottles()
		db.Mdb = prevDB
		db.Rdb = prevRdb
		config.FilmPictureUploadDir = prevUploadDir
		siteops.SetTriggerResetCategorySync(prevSyncFn)
		access.SetEnabled(prevAccess)
		sourcecatalog.InvalidatePrimarySourceCache()
	})

	// 1. 写入必须保留的账号与迁移版本表数据
	if err := gdb.Create(&model.User{
		UserName: "keep_admin",
		Password: "secret",
		Role:     model.UserRoleAdmin,
		Status:   0,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.SchemaMigration{Version: "20261120_01"}).Error; err != nil {
		t.Fatal(err)
	}

	// 2. 写入全量测试脏数据与自定义配置
	gdb.Create(&model.FilmSource{Id: "custom_src", Name: "自定义站", Uri: "https://custom.example/api", State: true, Sort: 0})
	gdb.Create(&model.PrimaryState{Id: 1, PrimarySourceId: "custom_src"})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1001, FirstSourceId: "custom_src"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 2},
		FilmIndexContent:  model.FilmIndexContent{Name: "测试影片"},
	})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 1001, SourceId: "custom_src", LineKind: "play", Content: "第1集$https://example.com/1.m3u8"})
	gdb.Create(&model.MovieSourceMapping{SourceId: "custom_src", SourceMid: 11, GlobalMid: 1001})
	gdb.Create(&model.MovieMatchKey{Mid: 1001, MatchKey: "mk_test"})
	gdb.Create(&model.FilmTMDBScrape{Mid: 1001, TmdbID: 99, MediaType: "movie"})
	gdb.Create(&model.CollectSourceStats{SourceId: "custom_src", LastRunPageCount: 10})
	gdb.Create(&model.SearchTagItem{Pid: 1, TagType: "Area", Name: "大陆", Value: "大陆", Score: 1})
	rec := model.CollectRecord{BatchID: "rec_1", Trigger: "manual"}
	gdb.Create(&rec)
	gdb.Create(&model.CollectRecordFilmItem{RecordID: rec.ID, SourceID: "custom_src", Mid: 1001})
	gdb.Create(&model.Category{Id: 99, Pid: 0, Name: "旧分类", Show: true})
	gdb.Create(&model.SourceCategory{SourceId: "custom_src", SourceTypeId: 1, RawName: "动作"})
	gdb.Create(&model.CategoryMapping{SourceId: "custom_src", SourceTypeId: 1, CategoryId: 99})
	gdb.Create(&model.MappingRule{SourceId: "", Group: "Area", Raw: "中国大陆", Target: "大陆"})
	gdb.Create(&model.BannerSlot{Id: "b_old", SourceId: "custom_src", Mid: 1001})
	gdb.Create(&model.FileInfo{Fid: "old.png", Link: "/upload/old.png", Uid: 10000})
	gdb.Create(&model.AccessDailyStats{Day: "2026-11-19", PV: 12})
	gdb.Create(&model.AccessDailyTop{Day: "2026-11-19", Kind: "film", Rank: 1, ItemKey: "1001", Title: "测试", Count: 5})
	gdb.Create(&model.DanmakuRecord{Mid: 1001, Text: "hi"})
	gdb.Create(&model.CrontabRecord{TaskId: "old_task", TaskModel: 9, Spec: "0 0 * * * *"})
	gdb.Create(&model.SiteConfigRecord{SiteName: "OldSite"})
	gdb.Create(&model.TMDBConfigRecord{Payload: `{"apiKey":"old_key"}`})
	gdb.Create(&model.ProxyConfigRecord{Payload: `{"proxyUrl":"http://127.0.0.1:8888"}`})
	gdb.Create(&model.NotifyConfigRecord{Payload: `{"botToken":"old_token"}`})
	gdb.Create(&model.AnalyticsConfigRecord{Payload: `{"enabled":true}`})

	oldFile := filepath.Join(uploadDir, "old.png")
	if err := os.WriteFile(oldFile, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	collect.RegisterTaskCid("old_task", 999)

	// 3. 执行数据重置
	if err := siteops.ResetSvc.ClearFilms(); err != nil {
		t.Fatalf("ClearFilms failed: %v", err)
	}
	waitResetSearchWorkIdle(t)

	// 4. 校验账号与迁移版本表完整保留
	var users []model.User
	if err := gdb.Find(&users).Error; err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].UserName != "keep_admin" {
		t.Fatalf("expected user table preserved, got %+v", users)
	}
	var migCount int64
	gdb.Model(&model.SchemaMigration{}).Count(&migCount)
	if migCount != 1 {
		t.Fatalf("expected schema_migrations preserved, got %d", migCount)
	}

	// 5. 校验旧文件、旧配置与业务表已清零
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("expected old gallery file removed, stat err=%v", err)
	}
	for _, zeroCheck := range []struct {
		name string
		cnt  func() int64
	}{
		{"film_index", func() int64 { var c int64; gdb.Unscoped().Model(&model.FilmIndex{}).Count(&c); return c }},
		{"film_source_playlists", func() int64 { var c int64; gdb.Model(&model.FilmSourcePlaylist{}).Count(&c); return c }},
		{"movie_source_mapping", func() int64 { var c int64; gdb.Unscoped().Model(&model.MovieSourceMapping{}).Count(&c); return c }},
		{"banner_slot", func() int64 { var c int64; gdb.Model(&model.BannerSlot{}).Count(&c); return c }},
		{"files", func() int64 { var c int64; gdb.Model(&model.FileInfo{}).Count(&c); return c }},
		{"tmdb_config", func() int64 { var c int64; gdb.Model(&model.TMDBConfigRecord{}).Count(&c); return c }},
		{"proxy_config", func() int64 { var c int64; gdb.Model(&model.ProxyConfigRecord{}).Count(&c); return c }},
		{"notify_config", func() int64 { var c int64; gdb.Model(&model.NotifyConfigRecord{}).Count(&c); return c }},
		{"analytics_config_records", func() int64 { var c int64; gdb.Model(&model.AnalyticsConfigRecord{}).Count(&c); return c }},
		{"access_daily_stats", func() int64 { var c int64; gdb.Model(&model.AccessDailyStats{}).Count(&c); return c }},
		{"danmaku_record", func() int64 { var c int64; gdb.Model(&model.DanmakuRecord{}).Count(&c); return c }},
	} {
		if got := zeroCheck.cnt(); got != 0 {
			t.Fatalf("expected %s to be cleared, got %d rows", zeroCheck.name, got)
		}
	}

	// 6. 校验默认初始化数据已重新写入并生效
	var srcCount int64
	gdb.Unscoped().Model(&model.FilmSource{}).Count(&srcCount)
	if srcCount != int64(len(defaultFilmSources())) {
		t.Fatalf("expected %d default sources re-seeded, got %d", len(defaultFilmSources()), srcCount)
	}
	var customSrcCount int64
	gdb.Unscoped().Model(&model.FilmSource{}).Where("id = ?", "custom_src").Count(&customSrcCount)
	if customSrcCount != 0 {
		t.Fatalf("expected custom_src removed, got %d", customSrcCount)
	}

	primary := sourcecatalog.CurrentPrimarySource()
	if primary == nil || primary.Id != defaultFilmSources()[0].Id {
		t.Fatalf("expected default primary source %q, got %+v", defaultFilmSources()[0].Id, primary)
	}
	var primaryState model.PrimaryState
	if err := gdb.First(&primaryState, 1).Error; err != nil || primaryState.PrimarySourceId != primary.Id {
		t.Fatalf("expected primary_state updated to %q, got %+v err=%v", primary.Id, primaryState, err)
	}

	basicCfg, err := repobase.GetSiteBasic()
	if err != nil {
		t.Fatalf("GetSiteBasic failed: %v", err)
	}
	if basicCfg.SiteName != "EcoHub" {
		t.Fatalf("expected default SiteName EcoHub, got %q", basicCfg.SiteName)
	}

	var catCount, ruleCount, cronCount int64
	gdb.Model(&model.Category{}).Count(&catCount)
	gdb.Model(&model.MappingRule{}).Count(&ruleCount)
	gdb.Model(&model.CrontabRecord{}).Count(&cronCount)
	if catCount != 0 || ruleCount != 0 || cronCount != int64(len(defaultFilmTasks())) {
		t.Fatalf("expected old categories/rules cleared and %d default crons initialized, got cats=%d rules=%d crons=%d", len(defaultFilmTasks()), catCount, ruleCount, cronCount)
	}

	var oldCronCount int64
	gdb.Model(&model.CrontabRecord{}).Where("task_id = ?", "old_task").Count(&oldCronCount)
	if oldCronCount != 0 {
		t.Fatalf("expected old_task removed, got %d", oldCronCount)
	}
	if entry := collect.GetEntryByTaskId("old_task"); entry.ID != 0 {
		t.Fatalf("expected old_task unregistered from CronCollect, got entry %d", entry.ID)
	}

	if access.IsEnabled() != model.DefaultAnalyticsConfig().Enabled {
		t.Fatalf("expected analytics runtime reset to default (%v), got %v", model.DefaultAnalyticsConfig().Enabled, access.IsEnabled())
	}
	if !catSyncCalled {
		t.Fatalf("expected background source category sync triggered after reset")
	}
}
