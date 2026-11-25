package admin

import (
	"server/internal/mediaproxy"
	"server/internal/film/catalog"
	"errors"
	"fmt"
	"net/url"
	"server/internal/access"
	"server/internal/collect"
	"sort"
	"strings"
	"time"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/migration"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/repobase"
	filmrepo "server/internal/film"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/admin/upgrade"
	"server/internal/tmdb"
	"server/internal/utils"
)

// BackupService 站点配置备份/恢复（不含影视库存与账号）
type BackupService struct{}

var (
	BackupSvc                      = new(BackupService)
	syncImportedSourceCategoriesFn = func() {
		go func() {
			if err := collect.SpiderSvc.SyncAllSourceCategories(); err != nil {
				syslog.Errorf("[Backup] 导入采集站后自动同步分类树失败: %v", err)
			}
		}()
	}
)

// ExportConfig 导出当前可迁移配置
func (s *BackupService) ExportConfig() (model.ConfigBackup, error) {
	sources, err := collect.GetCollectSourceList()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	if sources == nil {
		sources = []model.FilmSource{}
	}
	bannerSlots, err := catalog.ListAllBannerSlots()
	if err != nil {
		return model.ConfigBackup{}, err
	}

	site, err := repobase.GetSiteBasic()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	proxyCfg, err := repobase.GetProxyConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	tmdbCfg, err := tmdb.GetTMDBConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	notifyCfg, err := notify.GetNotifyConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	analyticsCfg, err := access.GetAnalyticsConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}

	rules, err := catalog.ListAllMappingRules(catalog.MappingRuleQuery{})
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
	if collect.SpiderSvc.SiteResetRunning() {
		return errors.New("站点数据正在重置中，请稍后再导入配置")
	}
	var oldPrimaryBeforeImport *model.FilmSource
	if req.Modules.FilmSources {
		if err := collect.StopAllTasksAndWait(0, "导入采集站配置，停止采集"); err != nil {
			return fmt.Errorf("停止进行中的采集失败: %w", err)
		}
		collect.CollectSvc.CancelDelFilmSources(nil)
		collect.WaitDeleteSourceJobs()
		oldPrimaryBeforeImport = repobase.CurrentPrimarySource()
	}

	needCategoryRefresh := false
	primaryChanged := false

	if req.Modules.Site {
		if err := repobase.SaveSiteBasic(*backup.Site); err != nil {
			return fmt.Errorf("导入网站配置失败: %w", err)
		}
		mediaproxy.InvalidateMediaProxyMemoryCache()
		if !req.Modules.FilmSources && !req.Modules.MappingRules {
			filmsnapshot.BumpSearchCacheVersion()
		}
	}

	if req.Modules.FilmSources {
		list := backup.FilmSources
		var replaceErr error
		if err := filmsnapshot.WithPrimaryMutation(func(_ bool) error {
			// 导入前清理旧站限流器
			oldSources, err := collect.GetCollectSourceList()
			if err != nil {
				return err
			}
			for _, src := range oldSources {
				collect.ClearLimiter(src.Id)
			}
			if err := collect.ReplaceCollectSources(list); err != nil {
				return fmt.Errorf("导入采集站失败: %w", err)
			}
			for _, src := range list {
				collect.ClearLimiter(src.Id)
			}
			return nil
		}); err != nil {
			replaceErr = err
		}
		if replaceErr != nil {
			return replaceErr
		}
		primaryChanged = collect.ApplyPrimaryChanged(oldPrimaryBeforeImport, repobase.CurrentPrimarySource())
		// 主站 ID 没变时 applyPrimaryChanged 不会动缓存。地址或启用状态变了，搜索和首页仍要失效。
		if !primaryChanged {
			repobase.ClearIndexPageCache()
			filmsnapshot.BumpSearchCacheVersion()
		}
		repobase.ClearProvideNetworkConfigCache()
		filmsnapshot.ClearDynamicPlayCaches()
		filmsnapshot.ClearDerivedFilmListCaches()
		needCategoryRefresh = true
	}

	if req.Modules.Banners {
		slots, err := importBannerSlots(backup)
		if err != nil {
			return fmt.Errorf("导入首页轮播失败: %w", err)
		}
		if err := catalog.ReplaceAllBannerSlots(slots); err != nil {
			return fmt.Errorf("导入首页轮播失败: %w", err)
		}
	}

	if req.Modules.MappingRules {
		exports := backup.MappingRules
		if err := catalog.ReplaceMappingRules(exports); err != nil {
			return fmt.Errorf("导入映射规则失败: %w", err)
		}
		needCategoryRefresh = true
	}

	if req.Modules.Proxy {
		proxyCfg, err := validateAndNormalizeProxyConfig(*backup.Proxy)
		if err != nil {
			return fmt.Errorf("导入网络代理配置失败: %w", err)
		}
		if err := repobase.SaveProxyConfig(proxyCfg); err != nil {
			return fmt.Errorf("导入网络代理配置失败: %w", err)
		}
		utils.ResetProxyTransports()
		notify.NotifySvc.EnsureBotPoller()
		upgrade.TriggerReleasePoll()
	}

	if req.Modules.TMDB {
		if err := tmdb.SaveTMDBConfig(*backup.TMDB); err != nil {
			return fmt.Errorf("导入 TMDB 刮削配置失败: %w", err)
		}
	}

	if req.Modules.Notify {
		if err := notify.SaveNotifyConfig(*backup.Notify); err != nil {
			return fmt.Errorf("导入通知配置失败: %w", err)
		}
		notify.NotifySvc.EnsureBotPoller()
	}

	if needCategoryRefresh {
		if err := catalog.RefreshFutureCategoryMappingsFromSourceCategories(); err != nil {
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
		if err := access.SaveAnalyticsConfig(*backup.Analytics); err != nil {
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
			// shortcut: 协议大小写和连续多个结尾斜杠仍视为不同地址, 导入里出现再收紧
			key := uri
			if u, err := url.Parse(uri); err == nil && u.Host != "" {
				u.Host = strings.ToLower(u.Host)
				u.Path = strings.TrimSuffix(u.Path, "/")
				key = u.String()
			}
			if _, dup := seenURI[key]; dup {
				return fmt.Errorf("备份中包含重复采集站地址: %s", uri)
			}
			seenURI[key] = struct{}{}
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
	if req.Modules.Proxy {
		if b.Proxy == nil {
			return errors.New("备份中缺少网络代理配置数据")
		}
		if _, err := validateAndNormalizeProxyConfig(*b.Proxy); err != nil {
			return fmt.Errorf("网络代理配置无效: %w", err)
		}
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
	existing, err := catalog.ExistingFilmMids(mids)
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
			ok = repobase.FindCollectSourceById(s.SourceId) != nil
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
