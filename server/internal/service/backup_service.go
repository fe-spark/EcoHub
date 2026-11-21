package service

import (
	"errors"
	"fmt"
	"server/internal/access"
	"sort"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/migration"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/service/upgrade"
	"server/internal/spider"
	"server/internal/utils"
)

// BackupService 站点配置备份/恢复（不含影视库存与账号）
type BackupService struct{}

var (
	BackupSvc                      = new(BackupService)
	syncImportedSourceCategoriesFn = func() {
		go func() {
			if err := SpiderSvc.SyncAllSourceCategories(); err != nil {
				syslog.Errorf("[Backup] 导入采集站后自动同步分类树失败: %v", err)
			}
		}()
	}
)

// ExportConfig 导出当前可迁移配置
func (s *BackupService) ExportConfig() (model.ConfigBackup, error) {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	if sources == nil {
		sources = []model.FilmSource{}
	}
	bannerSlots, err := repository.ListAllBannerSlots()
	if err != nil {
		return model.ConfigBackup{}, err
	}

	site, err := repository.GetSiteBasic()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	proxyCfg, err := repository.GetProxyConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	tmdbCfg, err := repository.GetTMDBConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	notifyCfg, err := repository.GetNotifyConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	analyticsCfg, err := repository.GetAnalyticsConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}

	rules, err := repository.ListAllMappingRules(repository.MappingRuleQuery{})
	if err != nil {
		return model.ConfigBackup{}, err
	}
	exports := make([]model.MappingRuleExport, 0, len(rules))
	for _, r := range rules {
		exports = append(exports, model.MappingRuleExport{
			SourceId:  r.SourceId,
			Group:     r.Group,
			Raw:       r.Raw,
			Target:    r.Target,
			MatchType: r.MatchType,
			Remarks:   r.Remarks,
		})
	}

	return model.ConfigBackup{
		Version:      model.ConfigBackupVersion,
		ExportedAt:   time.Now().Format(time.RFC3339),
		AppVersion:   config.Version,
		Site:         &site,
		FilmSources:  sources,
		BannerSlots:  bannerSlots,
		MappingRules: exports,
		Proxy:        &proxyCfg,
		TMDB:         &tmdbCfg,
		Notify:       &notifyCfg,
		Analytics:    &analyticsCfg,
	}, nil
}

// ImportConfig 按模块恢复配置备份（调用方已校验密码）
func (s *BackupService) ImportConfig(req model.ConfigBackupImportRequest) error {
	if !req.Modules.Any() {
		return errors.New("请至少选择一个要导入的模块")
	}
	backup := req.Backup
	if backup.Version != 0 && backup.Version != model.ConfigBackupVersion {
		return fmt.Errorf("不支持的备份版本：%d（当前支持 %d）", backup.Version, model.ConfigBackupVersion)
	}

	// 1. 动库前统一校验所有勾选模块的数据是否存在，避免部分模块生效后才报错
	if err := validateImportPayload(req); err != nil {
		return err
	}

	// 2. 导入采集站前停掉采集与排队删站，且禁止与数据重置并发执行。
	if SpiderSvc.SiteResetRunning() {
		return errors.New("站点数据正在重置中，请稍后再导入配置")
	}
	var oldPrimaryBeforeImport *model.FilmSource
	if req.Modules.FilmSources {
		if err := spider.StopAllTasksAndWait(0, "导入采集站配置，停止采集"); err != nil {
			return fmt.Errorf("停止进行中的采集失败: %w", err)
		}
		CollectSvc.CancelDelFilmSources(nil)
		deleteSourceJobs.Wait()
		oldPrimaryBeforeImport = repository.CurrentPrimarySource()
	}

	needCategoryRefresh := false
	primaryChanged := false

	if req.Modules.Site {
		if err := repository.SaveSiteBasic(*backup.Site); err != nil {
			return fmt.Errorf("导入网站配置失败: %w", err)
		}
		InvalidateMediaProxyMemoryCache()
		if !req.Modules.FilmSources && !req.Modules.MappingRules {
			filmsnapshot.BumpSearchCacheVersion()
		}
	}

	if req.Modules.FilmSources {
		list := backup.FilmSources
		var replaceErr error
		if err := withPrimaryMutation(func(_ bool) error {
			// 导入前清理旧站限流器
			oldSources, err := repository.GetCollectSourceList()
			if err != nil {
				return err
			}
			for _, src := range oldSources {
				spider.ClearLimiter(src.Id)
			}
			if err := repository.ReplaceCollectSources(list); err != nil {
				return fmt.Errorf("导入采集站失败: %w", err)
			}
			for _, src := range list {
				spider.ClearLimiter(src.Id)
			}
			return nil
		}); err != nil {
			replaceErr = err
		}
		if replaceErr != nil {
			return replaceErr
		}
		primaryChanged = applyPrimaryChanged(oldPrimaryBeforeImport, repository.CurrentPrimarySource())
		spider.ClearAllCMSSearchUnsupported()
		clearProvideNetworkConfigCache()
		filmsnapshot.ClearDynamicPlayCaches()
		filmsnapshot.ClearDerivedFilmListCaches()
		needCategoryRefresh = true
	}

	if req.Modules.Banners {
		slots, err := importBannerSlots(backup)
		if err != nil {
			return fmt.Errorf("导入首页轮播失败: %w", err)
		}
		if err := repository.ReplaceAllBannerSlots(slots); err != nil {
			return fmt.Errorf("导入首页轮播失败: %w", err)
		}
	}

	if req.Modules.MappingRules {
		exports := backup.MappingRules
		if err := repository.ReplaceMappingRules(exports); err != nil {
			return fmt.Errorf("导入映射规则失败: %w", err)
		}
		needCategoryRefresh = true
	}

	if req.Modules.Proxy {
		if err := repository.SaveProxyConfig(*backup.Proxy); err != nil {
			return fmt.Errorf("导入网络代理配置失败: %w", err)
		}
		utils.CloseIdleHTTPConnections()
		NotifySvc.EnsureBotPoller()
		upgrade.TriggerReleasePoll()
	}

	if req.Modules.TMDB {
		if err := repository.SaveTMDBConfig(*backup.TMDB); err != nil {
			return fmt.Errorf("导入 TMDB 刮削配置失败: %w", err)
		}
	}

	if req.Modules.Notify {
		if err := repository.SaveNotifyConfig(*backup.Notify); err != nil {
			return fmt.Errorf("导入通知配置失败: %w", err)
		}
		NotifySvc.EnsureBotPoller()
	}

	if needCategoryRefresh {
		if err := repository.RefreshFutureCategoryMappingsFromSourceCategories(); err != nil {
			syslog.Errorf("[Backup] 刷新来源分类映射失败: %v", err)
		}
		// applyPrimaryChanged 已执行过 RefreshAccessDataCaches，避免重复扫删 Redis 前缀
		if !primaryChanged {
			if err := filmsnapshot.RefreshActiveProjectedReadModel(); err != nil {
				syslog.Errorf("[Backup] 刷新读模型投影失败: %v", err)
			}
		}
	}
	if req.Modules.Analytics {
		if err := repository.SaveAnalyticsConfig(*backup.Analytics); err != nil {
			return fmt.Errorf("导入数据分析配置失败: %w", err)
		}
		access.SetEnabled(backup.Analytics.Enabled)
	}

	if req.Modules.FilmSources {
		syncImportedSourceCategoriesFn()
	}
	if req.Modules.FilmSources || req.Modules.MappingRules {
		filmrepo.RefreshInventoryStatsAsync()
	}

	return nil
}

func validateImportPayload(req model.ConfigBackupImportRequest) error {
	b := req.Backup
	if req.Modules.Site && b.Site == nil {
		return errors.New("备份中缺少网站配置数据")
	}
	if req.Modules.FilmSources {
		if b.FilmSources == nil {
			return errors.New("备份中缺少采集站")
		}
		seenID := make(map[string]struct{}, len(b.FilmSources))
		seenURI := make(map[string]struct{}, len(b.FilmSources))
		for _, src := range b.FilmSources {
			uri := strings.TrimSpace(src.Uri)
			if !utils.ValidURL(uri) {
				return fmt.Errorf("采集站 %q 接口地址无效", src.Name)
			}
			if _, dup := seenURI[uri]; dup {
				return fmt.Errorf("备份中包含重复采集站地址: %s", uri)
			}
			seenURI[uri] = struct{}{}
			id := strings.TrimSpace(src.Id)
			if id == "" {
				id = utils.GenerateHashKey(uri)
			}
			if _, dup := seenID[id]; dup {
				return fmt.Errorf("备份中包含重复采集站 ID: %s", id)
			}
			seenID[id] = struct{}{}
		}
	}
	if req.Modules.Banners && b.BannerSlots == nil && b.Banners == nil {
		return errors.New("备份中缺少首页轮播")
	}
	if req.Modules.MappingRules && b.MappingRules == nil {
		return errors.New("备份中缺少映射规则")
	}
	if req.Modules.Proxy && b.Proxy == nil {
		return errors.New("备份中缺少网络代理配置数据")
	}
	if req.Modules.TMDB && b.TMDB == nil {
		return errors.New("备份中缺少 TMDB 刮削配置数据")
	}
	if req.Modules.Notify && b.Notify == nil {
		return errors.New("备份中缺少通知配置数据")
	}
	if req.Modules.Analytics && b.Analytics == nil {
		return errors.New("备份中缺少数据分析配置数据")
	}
	return nil
}

// importBannerSlots 新版备份直接用 bannerSlots，只保留现有站点、且片库里还有这部影片的行；
// 旧版备份的全局 banners 按迁移规则分到站点。备份里的 id 重复时重新生成；非法的覆盖值只丢弃该字段。
func importBannerSlots(backup model.ConfigBackup) ([]model.BannerSlot, error) {
	if backup.BannerSlots == nil {
		return migration.ConvertLegacyBanners(db.Mdb, backup.Banners)
	}
	mids := make([]int64, 0, len(backup.BannerSlots))
	for _, s := range backup.BannerSlots {
		if s.Mid > 0 {
			mids = append(mids, s.Mid)
		}
	}
	existing, err := repository.ExistingFilmMids(mids)
	if err != nil {
		return nil, err
	}
	out := make([]model.BannerSlot, 0, len(backup.BannerSlots))
	seen := make(map[string]bool, len(backup.BannerSlots))
	usedIDs := make(map[string]bool, len(backup.BannerSlots))
	sourceOK := map[string]bool{}
	nextSort := map[string]int64{}
	sorted := append([]model.BannerSlot(nil), backup.BannerSlots...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Sort < sorted[j].Sort })
	for _, s := range sorted {
		s.SourceId = strings.TrimSpace(s.SourceId)
		if s.SourceId == "" || s.Mid <= 0 || !existing[s.Mid] {
			continue
		}
		ok, checked := sourceOK[s.SourceId]
		if !checked {
			ok = repository.FindCollectSourceById(s.SourceId) != nil
			sourceOK[s.SourceId] = ok
		}
		if !ok {
			continue
		}
		key := fmt.Sprintf("%s:%d", s.SourceId, s.Mid)
		if seen[key] {
			continue
		}
		seen[key] = true
		s.Id = strings.TrimSpace(s.Id)
		if s.Id == "" || len(s.Id) > 64 || usedIDs[s.Id] {
			id, err := utils.GenerateSalt()
			if err != nil {
				return nil, err
			}
			s.Id = id
		}
		usedIDs[s.Id] = true
		model.SanitizeBannerSlot(&s)
		nextSort[s.SourceId]++
		s.Sort = nextSort[s.SourceId]
		out = append(out, s)
	}
	return out, nil
}
