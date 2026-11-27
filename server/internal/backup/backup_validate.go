package backup

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"server/internal/banner"
	"server/internal/infra/db"
	"server/internal/mapping"
	"server/internal/mediaproxy"
	"server/internal/migration"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/proxy"
	"server/internal/sourcecatalog"
	"server/internal/tmdb"
	"server/internal/utils"
)

func validateImportPayload(req model.ConfigBackupImportRequest) error {
	b := req.Backup
	if req.Modules.Site {
		if b.Site == nil {
			return errors.New("备份中缺少网站配置数据")
		}
		cleanKey, err := model.ValidateProvideKey(b.Site.PrivateAccess, b.Site.ProvideKey)
		if err != nil {
			return err
		}
		b.Site.ProvideKey = cleanKey
		if err := mediaproxy.ValidateMediaProxyConfig(b.Site.MediaProxy); err != nil {
			return fmt.Errorf("媒体反代配置无效: %w", err)
		}
		if apiURL := strings.TrimSpace(b.Site.Danmaku.ApiURL); apiURL != "" && !utils.IsSafeOutboundURL(apiURL) {
			return errors.New("弹幕接口地址无效或不允许访问内网地址")
		}
	}
	if req.Modules.FilmSources {
		if b.FilmSources == nil {
			return errors.New("备份中缺少采集站")
		}
		seenID := make(map[string]struct{}, len(b.FilmSources))
		seenURI := make(map[string]struct{}, len(b.FilmSources))
		for _, src := range b.FilmSources {
			uri := strings.TrimSpace(src.Uri)
			if !utils.IsSafeOutboundURL(uri) {
				return fmt.Errorf("采集站 %q 接口地址无效或不允许访问内网地址", src.Name)
			}
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
	if req.Modules.MappingRules {
		if b.MappingRules == nil {
			return errors.New("备份中缺少映射规则")
		}
		if err := mapping.ValidateMappingRulesForImport(b.MappingRules); err != nil {
			return fmt.Errorf("映射规则无效: %w", err)
		}
	}
	if req.Modules.Proxy {
		if b.Proxy == nil {
			return errors.New("备份中缺少网络代理配置数据")
		}
		if _, err := proxy.ValidateAndNormalizeProxyConfig(*b.Proxy); err != nil {
			return fmt.Errorf("网络代理配置无效: %w", err)
		}
	}
	if req.Modules.TMDB {
		if b.TMDB == nil {
			return errors.New("备份中缺少 TMDB 刮削配置数据")
		}
		if err := tmdb.ValidateTMDBImageDomain(b.TMDB.ImageDomain); err != nil {
			return err
		}
	}
	if req.Modules.Notify {
		if b.Notify == nil {
			return errors.New("备份中缺少通知配置数据")
		}
		if _, err := notify.ValidateNotifyConfig(*b.Notify); err != nil {
			return fmt.Errorf("通知配置无效: %w", err)
		}
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
	existing, err := banner.ExistingFilmMids(mids)
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
			ok = sourcecatalog.FindCollectSourceById(s.SourceId) != nil
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
