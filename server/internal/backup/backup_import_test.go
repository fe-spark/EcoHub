package backup

import (
	"encoding/json"
	"server/internal/access"
	"server/internal/collect"
	"sync/atomic"
	"testing"

	filmsnapshot "server/internal/film/snapshot"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
)

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

	cfg, err := access.GetAnalyticsConfig()
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

	sources, err := collect.GetCollectSourceList()
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
	if err := collect.SaveFilmTask(builtinTask); err != nil {
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
	site, err := repobase.GetSiteBasic()
	if err != nil || site.SiteName != "恢复后站点" {
		t.Fatalf("site not updated: %+v, err: %v", site, err)
	}

	// 验证 2：系统内置任务完好无损，未被废弃的 deprecated_task 污染或清空
	tasks, err := collect.GetAllFilmTask()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Id != model.TaskIDCollectClean {
		t.Fatalf("builtin tasks corrupted, got: %+v", tasks)
	}
	if _, err := collect.GetFilmTaskById("deprecated_task"); err == nil {
		t.Fatal("deprecated task should not be imported")
	}
}

func TestImportConfig_FilmSources_ReplacesAndRejectsDuplicateURI(t *testing.T) {
	gdb := setupBackupTestDB(t)

	prevFn := syncImportedSourceCategoriesFn
	syncImportedSourceCategoriesFn = func() {}
	t.Cleanup(func() {
		syncImportedSourceCategoriesFn = prevFn
	})

	if err := gdb.Create(&model.FilmSource{
		Id:    "src_live",
		Name:  "线上主站",
		Uri:   "http://live.test/api",
		State: true,
		Sort:  0,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// 1. 备份中包含重复 Uri 时应在前置校验拦截，不覆盖现有站点
	dupReq := model.ConfigBackupImportRequest{
		Modules: model.ConfigBackupModules{FilmSources: true},
		Backup: model.ConfigBackup{
			Version: model.ConfigBackupVersion,
			FilmSources: []model.FilmSource{
				{Id: "a", Name: "站1", Uri: "http://dup.test/api"},
				{Id: "b", Name: "站2", Uri: "http://dup.test/api"},
			},
		},
	}
	if err := BackupSvc.ImportConfig(dupReq); err == nil {
		t.Fatal("expected error for duplicate source uri")
	}
	slashReq := dupReq
	slashReq.Backup.FilmSources = []model.FilmSource{
		{Id: "a", Name: "站1", Uri: "http://dup.test/api"},
		{Id: "b", Name: "站2", Uri: "http://DUP.test/api/"},
	}
	if err := BackupSvc.ImportConfig(slashReq); err == nil {
		t.Fatal("expected error for duplicate source uri differing by host case or trailing slash")
	}

	// 2. 正常导入采集站替换生效
	filmsnapshot.ResetSearchCacheVersionForTest()
	searchBefore := filmsnapshot.GetSearchCacheVersion()
	indexBefore := repobase.IndexPageGeneration()
	req := model.ConfigBackupImportRequest{
		Modules: model.ConfigBackupModules{FilmSources: true},
		Backup: model.ConfigBackup{
			Version: model.ConfigBackupVersion,
			FilmSources: []model.FilmSource{
				{Id: "src_live", Name: "线上主站-更新", Uri: "http://live.test/api", State: true},
				{Id: "src_new", Name: "新站点", Uri: "http://new.test/api", State: true},
			},
		},
	}
	if err := BackupSvc.ImportConfig(req); err != nil {
		t.Fatalf("import config failed: %v", err)
	}
	primary := sourcecatalog.CurrentPrimarySource()
	if primary == nil || primary.Id != "src_live" {
		t.Fatalf("expected primary to stay src_live, got %+v", primary)
	}
	if filmsnapshot.GetSearchCacheVersion() == searchBefore {
		t.Fatal("film source import should bump search cache when primary id stays")
	}
	if repobase.IndexPageGeneration() == indexBefore {
		t.Fatal("film source import should clear index page cache when primary id stays")
	}

	gotLive := sourcecatalog.FindCollectSourceById("src_live")
	if gotLive == nil || gotLive.Name != "线上主站-更新" {
		t.Fatalf("expected updated live source, got %+v", gotLive)
	}
	gotNew := sourcecatalog.FindCollectSourceById("src_new")
	if gotNew == nil || gotNew.Name != "新站点" {
		t.Fatalf("expected new source created, got %+v", gotNew)
	}
}

func TestImportConfig_RejectsInvalidProxyConfig(t *testing.T) {
	setupBackupTestDB(t)

	cases := []model.ProxyConfig{
		{Enabled: true, ProxyURL: "", Modules: model.ProxyModuleScope{Spider: true}},
		{Enabled: false, ProxyURL: "ftp://127.0.0.1:21", Modules: model.ProxyModuleScope{Spider: true}},
	}
	for _, cfg := range cases {
		req := model.ConfigBackupImportRequest{
			Modules: model.ConfigBackupModules{Proxy: true},
			Backup: model.ConfigBackup{
				Version: model.ConfigBackupVersion,
				Proxy:   &cfg,
			},
		}
		if err := BackupSvc.ImportConfig(req); err == nil {
			t.Fatalf("expected error for invalid backup proxy config: %+v", cfg)
		}
	}
}
