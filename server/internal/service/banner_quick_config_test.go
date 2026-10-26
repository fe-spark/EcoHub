package service

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
)

func TestBannerQuickConfig_SaveAndSchedule(t *testing.T) {
	setupBackupTestDB(t)
	origRdb := db.Rdb
	db.Rdb = nil
	prevVersion := filmsnapshot.GetActiveSnapshotVersion()
	filmsnapshot.ResetActiveSnapshotFallbackForTest()
	t.Cleanup(func() {
		db.Rdb = origRdb
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
}

func TestBannerQuickConfig_AutoDefaultOnCategoriesReady(t *testing.T) {
	gdb := setupBackupTestDB(t)
	origRdb := db.Rdb
	db.Rdb = nil
	t.Cleanup(func() {
		db.Rdb = origRdb
	})
	_ = gdb.AutoMigrate(&model.Category{}, &model.SourceCategory{}, &model.CategoryMapping{})

	// 1. 无主站/分类时，EnsureBannerQuickDefaultConfig 返回 ok = false
	if _, ok, err := ManageSvc.EnsureBannerQuickDefaultConfig(); err != nil || ok {
		t.Fatalf("expected ok=false when no categories, got ok=%v err=%v", ok, err)
	}

	// 2. 配置主站与分类
	src := model.FilmSource{Id: "src_master_banner", Name: "主站", Uri: "http://test", State: true, Sort: 1}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	sc1 := model.SourceCategory{SourceId: src.Id, SourceTypeId: 1, RawName: "电影", Sort: 1, Depth: 0}
	sc2 := model.SourceCategory{SourceId: src.Id, SourceTypeId: 2, RawName: "电视剧", Sort: 2, Depth: 0}
	if err := gdb.Create(&[]model.SourceCategory{sc1, sc2}).Error; err != nil {
		t.Fatal(err)
	}
	m1 := model.CategoryMapping{SourceId: src.Id, SourceTypeId: 1, CategoryId: 101}
	m2 := model.CategoryMapping{SourceId: src.Id, SourceTypeId: 2, CategoryId: 102}
	if err := gdb.Create(&[]model.CategoryMapping{m1, m2}).Error; err != nil {
		t.Fatal(err)
	}
	cat1 := model.Category{Id: 101, Pid: 0, Name: "电影", Show: true, Sort: 1, StableKey: "source:src_master_banner:1"}
	cat2 := model.Category{Id: 102, Pid: 0, Name: "电视剧", Show: true, Sort: 2, StableKey: "source:src_master_banner:2"}
	if err := gdb.Create(&[]model.Category{cat1, cat2}).Error; err != nil {
		t.Fatal(err)
	}

	// 3. 分类就绪后，EnsureBannerQuickDefaultConfig 应自愈补建默认排片规则 (每类 1 部)
	defaultCfg, ok, err := ManageSvc.EnsureBannerQuickDefaultConfig()
	if err != nil || !ok {
		t.Fatalf("expected ok=true when categories ready, got ok=%v err=%v", ok, err)
	}
	if len(defaultCfg.Quotas) != 2 || defaultCfg.Quotas[0].Count != 1 || defaultCfg.Quotas[1].Count != 1 {
		t.Fatalf("unexpected auto default quotas: %+v", defaultCfg.Quotas)
	}

	// 4. 验证已存入 repository
	cfg, err := repository.GetBannerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Quotas) != 2 || cfg.Quotas[0].Count != 1 || cfg.Quotas[1].Count != 1 {
		t.Fatalf("stored quotas mismatch: %+v", cfg.Quotas)
	}
}

func TestSaveBannerConfigIfEmptyKeepsSavedQuotas(t *testing.T) {
	setupBackupTestDB(t)
	origRdb := db.Rdb
	db.Rdb = nil
	t.Cleanup(func() {
		_ = repository.DeleteBannerConfig()
		db.Rdb = origRdb
	})
	if err := repository.DeleteBannerConfig(); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveBannerConfig(model.BannerConfig{
		Strategy: repository.BannerStrategyScore,
		Quotas:   []model.BannerQuota{{CategoryID: 3, Count: 4}},
	}); err != nil {
		t.Fatal(err)
	}
	got, wrote, err := repository.SaveBannerConfigIfEmpty(model.BannerConfig{
		Strategy: repository.BannerStrategyHot,
		Quotas:   []model.BannerQuota{{CategoryID: 3, Count: 1}},
	})
	if err != nil || wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	if len(got.Quotas) != 1 || got.Quotas[0].Count != 4 {
		t.Fatalf("got %+v", got.Quotas)
	}
	stored, err := repository.GetBannerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Quotas) != 1 || stored.Quotas[0].Count != 4 {
		t.Fatalf("stored %+v", stored.Quotas)
	}

	if err := repository.DeleteBannerConfig(); err != nil {
		t.Fatal(err)
	}
	got, wrote, err = repository.SaveBannerConfigIfEmpty(model.BannerConfig{
		Strategy: repository.BannerStrategyHot,
		Quotas:   []model.BannerQuota{{CategoryID: 3, Count: 1}},
	})
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	if len(got.Quotas) != 1 || got.Quotas[0].Count != 1 {
		t.Fatalf("default %+v", got.Quotas)
	}
}
