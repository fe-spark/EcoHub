package service

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
)

func TestBannerQuickConfig_SaveAndSchedule(t *testing.T) {
	setupBackupTestDB(t)
	origRdb := db.Rdb
	db.Rdb = nil
	prevVersion := filmsnapshot.GetActiveSnapshotVersion()
	filmsnapshot.ResetActiveSnapshotFallbackForTest()
	t.Cleanup(func() {
		db.Rdb = origRdb
		spider.RemoveCronByTaskId(model.TaskIDBannerAuto)
		if prevVersion != "" {
			_ = filmsnapshot.SetActiveSnapshotVersion(prevVersion)
		}
	})

	view, err := ManageSvc.GetBannerQuickConfig()
	if err != nil {
		t.Fatal(err)
	}
	if view.Saved || view.Strategy != "hot" {
		t.Fatalf("unsaved view: %+v", view)
	}

	saved, err := ManageSvc.SaveBannerQuickConfig(BannerQuickConfigInput{
		Strategy:   "score",
		AutoScrape: true,
		Quotas: []model.BannerQuota{
			{CategoryID: 1, Count: 2},
			{CategoryID: 2, Count: 0},
		},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !saved.Saved || saved.Strategy != "score" || !saved.AutoScrape {
		t.Fatalf("saved view: %+v", saved)
	}
	cfg, err := repository.GetBannerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Strategy != repository.BannerStrategyScore || len(cfg.Quotas) != 1 || cfg.Quotas[0].Count != 2 {
		t.Fatalf("stored quotas: %+v", cfg)
	}
	if err := saveBannerCron(t, false); err != nil {
		t.Fatal(err)
	}
	if err := CronSvc.ChangeFilmCrontab(model.TaskIDBannerAuto, true); err != nil {
		t.Fatalf("enable cron: %v", err)
	}
	if spider.GetEntryByTaskId(model.TaskIDBannerAuto).ID == 0 {
		t.Fatal("enabled schedule was not registered")
	}

	if err := repository.SaveBannerConfig(model.BannerConfig{Strategy: repository.BannerStrategyHot}); err != nil {
		t.Fatal(err)
	}
	if err := CronSvc.ChangeFilmCrontab(model.TaskIDBannerAuto, false); err != nil {
		t.Fatal(err)
	}
	if err := CronSvc.ChangeFilmCrontab(model.TaskIDBannerAuto, true); err == nil {
		t.Fatal("enabled schedule without quotas should fail")
	}

	enabled, err := ManageSvc.SaveBannerQuickConfig(BannerQuickConfigInput{
		Strategy: "latest",
		Quotas:   []model.BannerQuota{{CategoryID: 9, Count: 1}},
	})
	if err != nil {
		t.Fatalf("save latest: %v", err)
	}
	if enabled.Strategy != "latest" {
		t.Fatalf("enabled view: %+v", enabled)
	}
	if err := CronSvc.ChangeFilmCrontab(model.TaskIDBannerAuto, true); err != nil {
		t.Fatalf("re-enable: %v", err)
	}

	if err := ManageSvc.RunBannerQuickSchedule(); err == nil {
		t.Fatal("run without snapshot should fail and keep the live deck")
	}
	banners, err := repository.GetBanners()
	if err != nil {
		t.Fatal(err)
	}
	if len(banners) != 0 {
		t.Fatalf("live deck changed: %+v", banners)
	}
}

func TestResetBannerDeckForSourceChange(t *testing.T) {
	setupBackupTestDB(t)
	origRdb := db.Rdb
	db.Rdb = nil
	t.Cleanup(func() {
		db.Rdb = origRdb
		spider.RemoveCronByTaskId(model.TaskIDBannerAuto)
	})

	if err := repository.SaveBanners(model.Banners{{Id: "b1", Mid: 1, Name: "旧站影片", Sort: 1}}); err != nil {
		t.Fatalf("save banners: %v", err)
	}
	if _, err := ManageSvc.SaveBannerQuickConfig(BannerQuickConfigInput{
		Strategy: "hot",
		Quotas:   []model.BannerQuota{{CategoryID: 8, Count: 2}},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := saveBannerCron(t, true); err != nil {
		t.Fatal(err)
	}
	if spider.GetEntryByTaskId(model.TaskIDBannerAuto).ID == 0 {
		t.Fatal("schedule should be registered before reset")
	}

	if err := ResetBannerDeckForSourceChange(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	banners, err := repository.GetBanners()
	if err != nil {
		t.Fatal(err)
	}
	if len(banners) != 0 {
		t.Fatalf("banners not cleared: %+v", banners)
	}
	if exists, err := repository.BannerConfigExists(); err != nil || exists {
		t.Fatal("banner config should be removed")
	}
	view, err := ManageSvc.GetBannerQuickConfig()
	if err != nil {
		t.Fatal(err)
	}
	if view.Saved || len(view.Quotas) != 0 {
		t.Fatalf("config view: %+v", view)
	}
	task, err := repository.GetFilmTaskById(model.TaskIDBannerAuto)
	if err != nil || task.State {
		t.Fatalf("schedule should be off: %+v err=%v", task, err)
	}
	if spider.GetEntryByTaskId(model.TaskIDBannerAuto).ID != 0 {
		t.Fatal("schedule should be unregistered")
	}
}

func TestDisableBannerQuickSchedule_KeepsRecipe(t *testing.T) {
	setupBackupTestDB(t)
	origRdb := db.Rdb
	db.Rdb = nil
	t.Cleanup(func() {
		db.Rdb = origRdb
		spider.RemoveCronByTaskId(model.TaskIDBannerAuto)
	})

	if _, err := ManageSvc.SaveBannerQuickConfig(BannerQuickConfigInput{
		Strategy:   "score",
		AutoScrape: true,
		Quotas:     []model.BannerQuota{{CategoryID: 8, Count: 2}},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := saveBannerCron(t, true); err != nil {
		t.Fatal(err)
	}
	if spider.GetEntryByTaskId(model.TaskIDBannerAuto).ID == 0 {
		t.Fatal("schedule should be registered")
	}

	if err := DisableBannerQuickSchedule(); err != nil {
		t.Fatalf("disable: %v", err)
	}
	view, err := ManageSvc.GetBannerQuickConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !view.Saved || !view.AutoScrape || view.Strategy != "score" {
		t.Fatalf("recipe should stay: %+v", view)
	}
	if len(view.Quotas) != 1 || view.Quotas[0].CategoryID != 8 || view.Quotas[0].Count != 2 {
		t.Fatalf("quotas: %+v", view.Quotas)
	}
	task, err := repository.GetFilmTaskById(model.TaskIDBannerAuto)
	if err != nil || task.State {
		t.Fatalf("schedule should be off: %+v err=%v", task, err)
	}
	if spider.GetEntryByTaskId(model.TaskIDBannerAuto).ID != 0 {
		t.Fatal("schedule should be unregistered")
	}
}

func saveBannerCron(t *testing.T, enabled bool) error {
	t.Helper()
	if err := repository.SaveFilmTask(model.FilmCollectTask{
		Id: model.TaskIDBannerAuto, Spec: "0 0 4 * * *", Model: model.TaskModelBannerAuto, State: false,
	}); err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	return CronSvc.ChangeFilmCrontab(model.TaskIDBannerAuto, true)
}
