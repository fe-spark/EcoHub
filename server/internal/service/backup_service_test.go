package service

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"server/internal/access"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"

	"github.com/glebarez/sqlite"
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
			Cd:       24,
			Format:   "json",
		},
	}
	if err := repository.ReplaceCollectSources(srcList); err != nil {
		t.Fatalf("replace collect sources: %v", err)
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
	if len(backup.Banners) != 1 || backup.Banners[0].Name != "测试轮播影片" {
		t.Fatalf("banners export mismatch: %+v", backup.Banners)
	}
	// 排片配置不再保存，导出里不能再出现 bannerConfig。
	raw, err := json.Marshal(backup)
	if err != nil {
		t.Fatalf("marshal backup: %v", err)
	}
	if strings.Contains(string(raw), "bannerConfig") {
		t.Fatalf("backup export should not contain bannerConfig: %s", raw)
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

func TestImportConfig_IgnoresLegacyBannerConfig(t *testing.T) {
	setupBackupTestDB(t)

	// 旧版本备份带 bannerConfig 和 isPinned/source，导入时忽略这些字段，不报错。
	legacy := `{
		"version": 1,
		"exportedAt": "2026-11-01T00:00:00+08:00",
		"filmSources": [],
		"mappingRules": [],
		"banners": [{"id": "imp_b1", "mid": 2001, "name": "导入的轮播", "sort": 1, "isPinned": true, "source": "auto"}],
		"bannerConfig": {"strategy": "score_random", "autoTMDB": true, "quotas": [{"categoryId": 3, "count": 1}]}
	}`
	var backup model.ConfigBackup
	if err := json.Unmarshal([]byte(legacy), &backup); err != nil {
		t.Fatalf("decode legacy backup: %v", err)
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

	banners, err := repository.GetBanners()
	if err != nil {
		t.Fatal(err)
	}
	if len(banners) != 1 || banners[0].Name != "导入的轮播" {
		t.Fatalf("imported banners mismatch: %+v", banners)
	}
	if db.Mdb.Migrator().HasTable("banner_config") {
		t.Fatalf("banner_config table should not be created")
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

func TestImportConfig_IgnoresDeprecatedCronTasks(t *testing.T) {
	setupBackupTestDB(t)

	// 准备当前系统的内置定时任务
	builtinTask := model.FilmCollectTask{
		Id:     model.TaskIDCollectClean,
		Spec:   "0 30 3 * * *",
		Model:  model.TaskModelCollectClean,
		State:  true,
		Remark: "系统内置清理任务",
	}
	if err := repository.SaveFilmTask(builtinTask); err != nil {
		t.Fatal(err)
	}

	// 模拟包含旧版 cronTasks 字段的 JSON 备份报文
	rawJSON := `{
		"modules": {
			"site": true,
			"cronTasks": true
		},
		"backup": {
			"version": 1,
			"site": {
				"siteName": "恢复后站点"
			},
			"cronTasks": [
				{
					"id": "deprecated_task",
					"spec": "0 0 * * *",
					"model": 0,
					"state": false,
					"remark": "已废弃的任务"
				}
			]
		}
	}`

	var req model.ConfigBackupImportRequest
	if err := json.Unmarshal([]byte(rawJSON), &req); err != nil {
		t.Fatalf("unmarshal legacy backup failed: %v", err)
	}

	// 执行导入：不应报错，且应直接忽略废弃的 cronTasks
	if err := BackupSvc.ImportConfig(req); err != nil {
		t.Fatalf("import config failed: %v", err)
	}

	// 验证 1：site 正常被导入
	site, err := repository.GetSiteBasic()
	if err != nil || site.SiteName != "恢复后站点" {
		t.Fatalf("site not updated: %+v, err: %v", site, err)
	}

	// 验证 2：系统内置任务完好无损，未被废弃的 deprecated_task 污染或清空
	tasks, err := repository.GetAllFilmTask()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Id != model.TaskIDCollectClean {
		t.Fatalf("builtin tasks corrupted, got: %+v", tasks)
	}
	if _, err := repository.GetFilmTaskById("deprecated_task"); err == nil {
		t.Fatal("deprecated task should not be imported")
	}
}
