package service

import (
	"context"
	"math/rand"
	"testing"

	"server/internal/model"
	"server/internal/repository"
)

func TestNormalizeBannerConfig(t *testing.T) {
	// 1. 默认值与非法值修复
	cfg := repository.NormalizeBannerConfig(model.BannerConfig{
		Mode:        "UNKNOWN",
		Strategy:    "INVALID",
		Count:       -5, // 非法值兜底
		RefreshCron: "",
	})

	if cfg.Mode != repository.BannerModeManual {
		t.Fatalf("Mode expected %s, got %s", repository.BannerModeManual, cfg.Mode)
	}
	if cfg.Strategy != repository.BannerStrategyHot {
		t.Fatalf("Strategy expected %s, got %s", repository.BannerStrategyHot, cfg.Strategy)
	}
	if cfg.Count != repository.DefaultBannerCount {
		t.Fatalf("Count expected %d, got %d", repository.DefaultBannerCount, cfg.Count)
	}
	if cfg.RefreshCron != repository.DefaultBannerRefreshCron {
		t.Fatalf("RefreshCron expected %s, got %s", repository.DefaultBannerRefreshCron, cfg.RefreshCron)
	}

	// 2. 合法值保留
	validCfg := repository.NormalizeBannerConfig(model.BannerConfig{
		Mode:        repository.BannerModeAuto,
		Strategy:    repository.BannerStrategyScore,
		Count:       8,
		RefreshCron: "0 0 4 * * *",
		AutoTMDB:    true,
	})
	if validCfg.Mode != repository.BannerModeAuto || validCfg.Strategy != repository.BannerStrategyScore || validCfg.Count != 8 {
		t.Fatalf("Valid config mutated: %+v", validCfg)
	}

	// 3. 超限值截断（最大 12）
	overCfg := repository.NormalizeBannerConfig(model.BannerConfig{
		Count: 50,
	})
	if overCfg.Count != repository.MaxBannerCount {
		t.Fatalf("Over-limit count expected %d, got %d", repository.MaxBannerCount, overCfg.Count)
	}
}

func TestBannerFromSnapshot(t *testing.T) {
	snap := model.FilmListSnapshot{
		Mid:          12345,
		Name:         "测试影片",
		Year:         2024,
		CName:        "动作片",
		Picture:      "https://example.com/poster.jpg",
		PictureSlide: "https://example.com/backdrop.jpg",
		Remarks:      "HD中字",
	}

	b := bannerFromSnapshot(snap, 1, false)
	if b.Mid != 12345 || b.Name != "测试影片" || b.Year != 2024 {
		t.Fatalf("Banner fields mismatch: %+v", b)
	}
	if b.PictureSlide != "https://example.com/backdrop.jpg" {
		t.Fatalf("PictureSlide expected https://example.com/backdrop.jpg, got %s", b.PictureSlide)
	}
	if b.Id == "" {
		t.Fatalf("Id should not be empty")
	}
	if b.Sort != 1 {
		t.Fatalf("Sort expected 1, got %d", b.Sort)
	}
	if !b.IsCustomPic {
		t.Fatalf("IsCustomPic expected true, got false")
	}
	if b.CustomPicture != "https://example.com/poster.jpg" {
		t.Fatalf("CustomPicture expected https://example.com/poster.jpg, got %s", b.CustomPicture)
	}
}

func TestGenerateAutoBannersNoSnapshotVersion(t *testing.T) {
	// 在无快照初始化状态下应优雅报错而非 panic
	_, err := BannerAutoSvc.GenerateAutoBanners(context.Background(), "test")
	if err == nil {
		t.Log("Generated successfully with active snapshot")
	} else {
		t.Logf("Gracefully returned expected error in test env: %v", err)
	}
}

func TestUpdateConfigWithoutTMDB(t *testing.T) {
	// 当 TMDB 未配置或未启用时，切换为 auto 模式依然能够成功保存，不被强制阻断
	err := BannerAutoSvc.UpdateConfig(model.BannerConfig{
		Mode:     repository.BannerModeAuto,
		Strategy: repository.BannerStrategyHot,
		Count:    6,
		AutoTMDB: false,
	})
	if err != nil {
		t.Fatalf("Expected update to auto mode to succeed even without TMDB, got: %v", err)
	}

	current := repository.GetBannerConfig()
	if current.Mode != repository.BannerModeAuto {
		t.Fatalf("Expected config mode to be auto, but got %s", current.Mode)
	}
}

func TestUpdateConfigManualSuccess(t *testing.T) {
	// 保存手动模式无需预刮削，直接快速保存
	err := BannerAutoSvc.UpdateConfig(model.BannerConfig{
		Mode:     repository.BannerModeManual,
		Strategy: repository.BannerStrategyHot,
		Count:    6,
	})
	if err != nil {
		t.Fatalf("Expected update to manual mode succeed without error, got: %v", err)
	}

	current := repository.GetBannerConfig()
	if current.Mode != repository.BannerModeManual {
		t.Fatalf("Expected config mode to be manual, but got %s", current.Mode)
	}
}

func TestHandleTMDBDisabled(t *testing.T) {
	// 验证降级逻辑不会 panic 并能正常执行
	BannerAutoSvc.HandleTMDBDisabled()
}

func TestNormalizeBannerTargetCount(t *testing.T) {
	if got := normalizeBannerTargetCount(0); got != repository.DefaultBannerCount {
		t.Fatalf("count=0 expected %d, got %d", repository.DefaultBannerCount, got)
	}
	if got := normalizeBannerTargetCount(-1); got != repository.DefaultBannerCount {
		t.Fatalf("count=-1 expected %d, got %d", repository.DefaultBannerCount, got)
	}
	if got := normalizeBannerTargetCount(30); got != repository.MaxBannerCount {
		t.Fatalf("count=30 expected %d, got %d", repository.MaxBannerCount, got)
	}
	if got := normalizeBannerTargetCount(8); got != 8 {
		t.Fatalf("count=8 expected 8, got %d", got)
	}
}

func TestBannerFromSnapshotFallbackPosterSlide(t *testing.T) {
	snap := model.FilmListSnapshot{
		Mid:     1,
		Name:    "无横图片",
		Picture: "https://example.com/poster.jpg",
	}

	withFallback := bannerFromSnapshot(snap, 1, true)
	if withFallback.PictureSlide != snap.Picture {
		t.Fatalf("fallback on: PictureSlide expected %s, got %s", snap.Picture, withFallback.PictureSlide)
	}

	noFallback := bannerFromSnapshot(snap, 1, false)
	if noFallback.PictureSlide != "" {
		t.Fatalf("fallback off: PictureSlide expected empty, got %s", noFallback.PictureSlide)
	}
}

func TestReplaceMissingSlidesPrefersFresh(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	picked := []model.FilmListSnapshot{{Mid: 1, Name: "新片无横图"}}
	fresh := []model.FilmListSnapshot{
		{Mid: 1, Name: "新片无横图"},
		{Mid: 2, Name: "全新有横图", PictureSlide: "https://example.com/fresh-slide.jpg"},
	}
	existing := []model.FilmListSnapshot{
		{Mid: 3, Name: "在展旧片", PictureSlide: "https://example.com/old-slide.jpg"},
	}

	got, extra := replaceMissingSlides(picked, fresh, existing, nil, r)
	if extra != 0 {
		t.Fatalf("expected extraReused=0 when fresh replacement exists, got %d", extra)
	}
	if len(got) != 1 || got[0].Mid != 2 {
		t.Fatalf("expected fresh replacement mid=2, got %+v", got)
	}
}

func TestReplaceMissingSlidesFallsBackToExisting(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	picked := []model.FilmListSnapshot{{Mid: 1, Name: "新片无横图"}}
	fresh := []model.FilmListSnapshot{{Mid: 1, Name: "新片无横图"}}
	existing := []model.FilmListSnapshot{
		{Mid: 3, Name: "在展旧片", PictureSlide: "https://example.com/old-slide.jpg"},
	}

	got, extra := replaceMissingSlides(picked, fresh, existing, nil, r)
	if extra != 1 {
		t.Fatalf("expected extraReused=1 when only existing has slide, got %d", extra)
	}
	if len(got) != 1 || got[0].Mid != 3 {
		t.Fatalf("expected existing replacement mid=3, got %+v", got)
	}
}

func TestReplaceMissingSlidesKeepsExistingReuseCount(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	picked := []model.FilmListSnapshot{{Mid: 3, Name: "在展无横图"}}
	fresh := []model.FilmListSnapshot{}
	existing := []model.FilmListSnapshot{
		{Mid: 3, Name: "在展无横图"},
		{Mid: 4, Name: "另一在展", PictureSlide: "https://example.com/old-slide.jpg"},
	}

	got, extra := replaceMissingSlides(picked, fresh, existing, nil, r)
	if extra != 0 {
		t.Fatalf("replacing already-reused film should not increment extraReused, got %d", extra)
	}
	if len(got) != 1 || got[0].Mid != 4 {
		t.Fatalf("expected replacement mid=4, got %+v", got)
	}
}

func TestScrapeUntilTargetCountWithCustomPosters(t *testing.T) {
	fresh := []model.FilmListSnapshot{
		{Mid: 101, Name: "已有高清A", IsCustomPicture: true, CustomPicture: "https://example.com/posterA.jpg"},
		{Mid: 102, Name: "已有高清B", IsCustomPicture: true, CustomPicture: "https://example.com/posterB.jpg"},
		{Mid: 103, Name: "已有高清C", IsCustomPicture: true, CustomPicture: "https://example.com/posterC.jpg"},
	}
	ctx := context.Background()
	picked, attempts, successes, _ := BannerAutoSvc.scrapeUntilTargetCount(ctx, 3, fresh, nil, "v_test")
	if len(picked) != 3 {
		t.Fatalf("expected 3 valid picked, got %d", len(picked))
	}
	if attempts != 0 || successes != 0 {
		t.Fatalf("expected 0 scrape attempts since all have custom posters, got %d", attempts)
	}
}

func TestScrapeUntilTargetCountTimeoutExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 模拟已超时上下文

	pool := []model.FilmListSnapshot{
		{Mid: 201, Name: "影片1"},
		{Mid: 202, Name: "影片2"},
	}
	picked, _, _, _ := BannerAutoSvc.scrapeUntilTargetCount(ctx, 2, pool, nil, "v_test")
	if len(picked) != 2 {
		t.Fatalf("expected 2 picked fallback items after timeout, got %d", len(picked))
	}
}

