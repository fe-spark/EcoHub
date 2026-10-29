package service

import (
	"fmt"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"
	"server/internal/access"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupBackupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:backup_test_%d_%d?mode=memory&cache=shared", time.Now().UnixNano(), rand.Intn(100000))
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.SiteConfigRecord{},
		&model.FilmSource{},
		&model.CrontabRecord{},
		&model.Banner{},
		&model.BannerConfigRecord{},
		&model.MappingRule{},
		&model.ProxyConfigRecord{},
		&model.TMDBConfigRecord{},
		&model.NotifyConfigRecord{},
		&model.AnalyticsConfigRecord{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	origMdb := db.Mdb
	db.Mdb = gdb

	t.Cleanup(func() {
		db.Mdb = origMdb
	})
	return gdb
}

func TestExportConfig_CompleteAndClean(t *testing.T) {
	setupBackupTestDB(t)

	// 1. 准备初始数据
	siteCfg := model.BasicConfig{
		SiteName: "测试站点",
		SiteURL:  "https://example.com",
		Tip: model.TipConfig{
			Enabled: true,
			Title:   "赞赏测试",
		},
		Notice: model.NoticeConfig{
			Enabled: true,
			Content: "开屏公告测试",
		},
	}
	if err := repository.SaveSiteBasic(siteCfg); err != nil {
		t.Fatalf("save site basic: %v", err)
	}

	srcList := []model.FilmSource{
		{
			Id:       "src_1",
			Name:     "资源站A",
			Uri:      "https://api.source1.com/api.php/provide/vod",
			State:    true,
			Sort:     1,
			Interval: 300,
			Cd:       24,
			Format:   "json",
		},
	}
	if err := repository.ReplaceCollectSources(srcList); err != nil {
		t.Fatalf("replace collect sources: %v", err)
	}

	task := model.FilmCollectTask{
		Id:     "task_1",
		Ids:    []string{"src_1"},
		Cid:    99, // 运行时临时内存 ID
		Time:   24,
		Spec:   "0 0 */2 * * *",
		Model:  0,
		State:  true,
		Remark: "定时采集任务",
	}
	if err := repository.SaveFilmTask(task); err != nil {
		t.Fatalf("save film task: %v", err)
	}

	bannerItems := model.Banners{
		{
			Id:   "b1",
			Mid:  1001,
			Name: "测试轮播影片",
			Sort: 1,
		},
	}
	if err := repository.SaveBanners(bannerItems); err != nil {
		t.Fatalf("save banners: %v", err)
	}

	bannerCfg := model.BannerConfig{
		Strategy: repository.BannerStrategyHot,
		AutoTMDB: true,
		Quotas: []model.BannerQuota{
			{CategoryID: 1, Count: 2},
			{CategoryID: 2, Count: 1},
		},
	}
	if err := repository.SaveBannerConfig(bannerCfg); err != nil {
		t.Fatalf("save banner config: %v", err)
	}

	proxyCfg := model.ProxyConfig{
		Enabled:  true,
		ProxyURL: "http://user:secret123@proxy.test:8080",
		Modules:  model.ProxyModuleScope{Spider: true, TMDB: true},
	}
	if err := repository.SaveProxyConfig(proxyCfg); err != nil {
		t.Fatalf("save proxy config: %v", err)
	}

	tmdbCfg := model.TMDBConfig{
		Enabled: true,
		ApiKey:  "tmdb_api_key_secret",
	}
	if err := repository.SaveTMDBConfig(tmdbCfg); err != nil {
		t.Fatalf("save tmdb config: %v", err)
	}

	notifyCfg := model.NotifyConfig{
		Enabled:  true,
		BotToken: "123456:ABC-DEF-bot-token",
		ChatIDs:  []string{"-10012345678"},
	}
	if err := repository.SaveNotifyConfig(notifyCfg); err != nil {
		t.Fatalf("save notify config: %v", err)
	}
	analyticsCfg := model.AnalyticsConfig{
		Enabled: true,
	}
	if err := repository.SaveAnalyticsConfig(analyticsCfg); err != nil {
		t.Fatalf("save analytics config: %v", err)
	}

	rule := model.MappingRule{
		Group:     "Area",
		Raw:       "中国大陆",
		Target:    "大陆",
		MatchType: "exact",
		Remarks:   "地区转换",
	}
	if err := repository.CreateMappingRule(&rule); err != nil {
		t.Fatalf("create mapping rule: %v", err)
	}

	// 2. 执行导出
	backup, err := BackupSvc.ExportConfig()
	if err != nil {
		t.Fatal(err)
	}

	// 3. 校验导出结果
	if backup.Version != model.ConfigBackupVersion {
		t.Fatalf("expected backup version %d, got %d", model.ConfigBackupVersion, backup.Version)
	}
	if backup.Site == nil || backup.Site.SiteName != "测试站点" {
		t.Fatalf("site config export mismatch: %+v", backup.Site)
	}
	if len(backup.FilmSources) != 1 || backup.FilmSources[0].Id != "src_1" {
		t.Fatalf("film sources export mismatch: %+v", backup.FilmSources)
	}
	if len(backup.CronTasks) != 1 || backup.CronTasks[0].Id != "task_1" {
		t.Fatalf("cron tasks export mismatch: %+v", backup.CronTasks)
	}
	// 验证运行时 Cid 是否已被清零，不污染备份
	if backup.CronTasks[0].Cid != 0 {
		t.Fatalf("expected task Cid to be cleared to 0 in export, got %d", backup.CronTasks[0].Cid)
	}
	if len(backup.Banners) != 1 || backup.Banners[0].Name != "测试轮播影片" {
		t.Fatalf("banners export mismatch: %+v", backup.Banners)
	}
	// 验证 BannerConfig 轮播排片配置导出
	if backup.BannerConfig == nil || backup.BannerConfig.Strategy != repository.BannerStrategyHot || !backup.BannerConfig.AutoTMDB {
		t.Fatalf("bannerConfig export mismatch: %+v", backup.BannerConfig)
	}
	if len(backup.BannerConfig.Quotas) != 2 {
		t.Fatalf("bannerConfig fields mismatch: %+v", backup.BannerConfig)
	}
	// 验证敏感凭据在灾备导出中完整保留
	if backup.Proxy == nil || backup.Proxy.ProxyURL != "http://user:secret123@proxy.test:8080" {
		t.Fatalf("proxy config raw url should be retained for migration: %+v", backup.Proxy)
	}
	if backup.TMDB == nil || backup.TMDB.ApiKey != "tmdb_api_key_secret" {
		t.Fatalf("tmdb raw api key should be retained for migration: %+v", backup.TMDB)
	}
	if backup.Notify == nil || backup.Notify.BotToken != "123456:ABC-DEF-bot-token" {
		t.Fatalf("notify bot token should be retained for migration: %+v", backup.Notify)
	}
	if len(backup.MappingRules) != 1 || backup.MappingRules[0].Raw != "中国大陆" {
		t.Fatalf("mapping rules export mismatch: %+v", backup.MappingRules)
	}
	if backup.Analytics == nil || !backup.Analytics.Enabled {
		t.Fatalf("analytics export mismatch: %+v", backup.Analytics)
	}
}

func TestImportConfig_WithBannerConfig(t *testing.T) {
	setupBackupTestDB(t)

	// 构建备份数据并导入
	backup := model.ConfigBackup{
		Version:    model.ConfigBackupVersion,
		ExportedAt: time.Now().Format(time.RFC3339),
		Banners: model.Banners{
			{
				Id:   "imp_b1",
				Mid:  2001,
				Name: "导入的轮播",
				Sort: 1,
			},
		},
		BannerConfig: &model.BannerConfig{
			Strategy: repository.BannerStrategyScore,
			AutoTMDB: false,
			Quotas:   []model.BannerQuota{{CategoryID: 3, Count: 1}},
		},
	}

	req := model.ConfigBackupImportRequest{
		Modules: model.ConfigBackupModules{
			Banners: true,
		},
		Backup: backup,
	}

	if err := BackupSvc.ImportConfig(req); err != nil {
		t.Fatalf("import config failed: %v", err)
	}

	// 验证落库
	banners, err := repository.GetBanners()
	if err != nil {
		t.Fatal(err)
	}
	if len(banners) != 1 || banners[0].Name != "导入的轮播" {
		t.Fatalf("imported banners mismatch: %+v", banners)
	}

	bannerCfg, err := repository.GetBannerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if bannerCfg.Strategy != repository.BannerStrategyScore || len(bannerCfg.Quotas) != 1 || bannerCfg.Quotas[0].CategoryID != 3 {
		t.Fatalf("imported bannerConfig mismatch: %+v", bannerCfg)
	}
}

func TestImportConfig_RejectsMissingPayloadBeforeWrite(t *testing.T) {
	setupBackupTestDB(t)

	// 初始先写一个网站配置
	initialSite := model.BasicConfig{SiteName: "初始站点"}
	if err := repository.SaveSiteBasic(initialSite); err != nil {
		t.Fatal(err)
	}

	// 构造一个备份包：声明要导入 Site 和 FilmSources，但 FilmSources 字段为 nil
	req := model.ConfigBackupImportRequest{
		Modules: model.ConfigBackupModules{
			Site:        true,
			FilmSources: true,
		},
		Backup: model.ConfigBackup{
			Version:     model.ConfigBackupVersion,
			Site:        &model.BasicConfig{SiteName: "试图覆盖的新站点"},
			FilmSources: nil, // 缺失
		},
	}

	err := BackupSvc.ImportConfig(req)
	if err == nil {
		t.Fatal("expected error on missing FilmSources payload, got nil")
	}
	if err.Error() != "备份中缺少采集站" {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证库里的网站配置完全未被动过，没有发生部分导入
	savedSite, err := repository.GetSiteBasic()
	if err != nil {
		t.Fatal(err)
	}
	if savedSite.SiteName != "初始站点" {
		t.Fatalf("site config should not be touched when validation fails, got %q", savedSite.SiteName)
	}
}

func TestImportConfig_Analytics(t *testing.T) {
	setupBackupTestDB(t)

	access.SetEnabled(false)
	backup := model.ConfigBackup{
		Version: model.ConfigBackupVersion,
		Analytics: &model.AnalyticsConfig{
			Enabled: true,
		},
	}

	req := model.ConfigBackupImportRequest{
		Modules: model.ConfigBackupModules{
			Analytics: true,
		},
		Backup: backup,
	}

	if err := BackupSvc.ImportConfig(req); err != nil {
		t.Fatalf("import config failed: %v", err)
	}

	cfg, err := repository.GetAnalyticsConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled {
		t.Fatalf("expected analytics enabled=true in repo")
	}
	if !access.IsEnabled() {
		t.Fatalf("expected access.IsEnabled() == true")
	}

	// 验证缺少 payload 时拒绝导入
	badReq := model.ConfigBackupImportRequest{
		Modules: model.ConfigBackupModules{
			Analytics: true,
		},
		Backup: model.ConfigBackup{
			Version:   model.ConfigBackupVersion,
			Analytics: nil,
		},
	}
	if err := BackupSvc.ImportConfig(badReq); err == nil {
		t.Fatal("expected error on missing analytics payload")
	}
}

func TestImportConfig_FilmSources_TriggersCategorySync(t *testing.T) {
	setupBackupTestDB(t)

	var synced atomic.Bool
	prevFn := syncImportedSourceCategoriesFn
	syncImportedSourceCategoriesFn = func() {
		synced.Store(true)
	}
	t.Cleanup(func() {
		syncImportedSourceCategoriesFn = prevFn
	})

	backup := model.ConfigBackup{
		Version: model.ConfigBackupVersion,
		FilmSources: []model.FilmSource{
			{Id: "imp_src_1", Name: "导入站", Uri: "http://imp.test"},
		},
	}
	req := model.ConfigBackupImportRequest{
		Modules: model.ConfigBackupModules{FilmSources: true},
		Backup:  backup,
	}

	if err := BackupSvc.ImportConfig(req); err != nil {
		t.Fatalf("import config failed: %v", err)
	}

	if !synced.Load() {
		t.Fatal("expected syncImportedSourceCategoriesFn to be called when importing FilmSources")
	}

	sources, err := repository.GetCollectSourceList()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Id != "imp_src_1" {
		t.Fatalf("imported sources mismatch: %+v", sources)
	}
}
