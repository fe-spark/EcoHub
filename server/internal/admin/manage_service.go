package admin

import (
	"errors"
	"server/internal/mediaproxy"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/utils"
	"strings"
	"sync"
)

type ManageService struct{}

var ManageSvc = new(ManageService)

// siteBasicMu 串行化 BasicConfig 局部字段的读-改-写，避免并发保存不同子模块相互覆盖。
var siteBasicMu sync.Mutex

// GetSiteBasicConfig 获取网站基本配置信息
func (s *ManageService) GetSiteBasicConfig() (model.BasicConfig, error) {
	return repobase.GetSiteBasic()
}

// UpdateSiteBasic 更新网站名称和简介。访问控制、弹幕、缺省的赞赏和公告保留。
func (s *ManageService) UpdateSiteBasic(bc model.BasicConfig) error {
	siteBasicMu.Lock()
	defer siteBasicMu.Unlock()
	curr, err := repobase.GetSiteBasic()
	if err != nil {
		return err
	}
	curr.SiteName = bc.SiteName
	curr.SiteURL = bc.SiteURL
	curr.Logo = bc.Logo
	curr.Keyword = bc.Keyword
	curr.Describe = bc.Describe
	if bc.Tip.Title != "" || len(bc.Tip.Channels) > 0 {
		curr.Tip = bc.Tip
	}
	if bc.Notice.Title != "" || bc.Notice.Content != "" {
		curr.Notice = bc.Notice
	}
	if bc.MediaProxy.Enabled || strings.TrimSpace(bc.MediaProxy.Template) != "" {
		if err := mediaproxy.ValidateMediaProxyConfig(bc.MediaProxy); err != nil {
			return err
		}
		curr.MediaProxy = bc.MediaProxy
	}
	if err := repobase.SaveSiteBasic(curr); err != nil {
		return err
	}
	mediaproxy.UpdateMediaProxyRuntime(curr.MediaProxy, curr.SiteURL)
	return nil
}

// GetSiteMediaProxyConfig 获取媒体图片边缘反向代理配置
func (s *ManageService) GetSiteMediaProxyConfig() (model.MediaProxyConfig, error) {
	b, err := repobase.GetSiteBasic()
	if err != nil {
		return model.MediaProxyConfig{}, err
	}
	return b.MediaProxy, nil
}

// UpdateSiteMediaProxyConfig 更新媒体图片边缘反向代理配置
func (s *ManageService) UpdateSiteMediaProxyConfig(cfg model.MediaProxyConfig) error {
	if err := mediaproxy.ValidateMediaProxyConfig(cfg); err != nil {
		return err
	}
	siteBasicMu.Lock()
	defer siteBasicMu.Unlock()
	curr, err := repobase.GetSiteBasic()
	if err != nil {
		return err
	}
	curr.MediaProxy = cfg
	if err := repobase.SaveSiteBasic(curr); err != nil {
		return err
	}
	mediaproxy.UpdateMediaProxyRuntime(curr.MediaProxy, curr.SiteURL)
	return nil
}

// GetSiteAccessConfig 获取网站访问控制配置
func (s *ManageService) GetSiteAccessConfig() (model.AccessConfig, error) {
	b, err := repobase.GetSiteBasic()
	if err != nil {
		return model.AccessConfig{}, err
	}
	return model.AccessConfig{
		State:         b.State,
		Hint:          b.Hint,
		PrivateAccess: b.PrivateAccess,
		ProvideKey:    b.ProvideKey,
	}, nil
}

// UpdateSiteAccessConfig 更新网站访问控制配置
func (s *ManageService) UpdateSiteAccessConfig(ac model.AccessConfig) error {
	siteBasicMu.Lock()
	defer siteBasicMu.Unlock()
	curr, err := repobase.GetSiteBasic()
	if err != nil {
		return err
	}
	curr.State = ac.State
	curr.Hint = ac.Hint
	curr.PrivateAccess = ac.PrivateAccess
	if curr.PrivateAccess {
		if strings.TrimSpace(ac.ProvideKey) == "" {
			key, err := utils.RandomString(16)
			if err != nil {
				return err
			}
			curr.ProvideKey = key
		} else {
			cleanKey, err := model.ValidateProvideKey(true, ac.ProvideKey)
			if err != nil {
				return err
			}
			curr.ProvideKey = cleanKey
		}
	} else {
		cleanKey, err := model.ValidateProvideKey(false, ac.ProvideKey)
		if err != nil {
			return err
		}
		curr.ProvideKey = cleanKey
	}
	return repobase.SaveSiteBasic(curr)
}

// GetSiteDanmakuConfig 获取网站弹幕系统配置
func (s *ManageService) GetSiteDanmakuConfig() (model.DanmakuConfig, error) {
	b, err := repobase.GetSiteBasic()
	if err != nil {
		return model.DanmakuConfig{}, err
	}
	return b.Danmaku, nil
}

// UpdateSiteDanmakuConfig 更新网站弹幕系统配置
func (s *ManageService) UpdateSiteDanmakuConfig(cfg model.DanmakuConfig) error {
	siteBasicMu.Lock()
	defer siteBasicMu.Unlock()
	curr, err := repobase.GetSiteBasic()
	if err != nil {
		return err
	}
	if cfg.Enabled && cfg.AutoSniff && strings.TrimSpace(cfg.ApiURL) == "" {
		if strings.TrimSpace(cfg.AppID) == "" {
			return errors.New("使用弹弹play官方接口嗅探时，必须填写 AppId")
		}
		if strings.TrimSpace(cfg.AppSecret) == "" && strings.TrimSpace(curr.Danmaku.AppSecret) == "" {
			return errors.New("使用弹弹play官方接口嗅探时，必须填写 AppSecret")
		}
	}
	next := cfg
	if strings.TrimSpace(next.ApiURL) == "" {
		if strings.TrimSpace(next.AppID) == "" {
			next.AppID = curr.Danmaku.AppID
		}
		if strings.TrimSpace(next.AppSecret) == "" {
			next.AppSecret = curr.Danmaku.AppSecret
		}
	}
	curr.Danmaku = next
	return repobase.SaveSiteBasic(curr)
}

// GetSiteTipConfig 获取赞赏配置
func (s *ManageService) GetSiteTipConfig() (model.TipConfig, error) {
	b, err := repobase.GetSiteBasic()
	if err != nil {
		return model.TipConfig{}, err
	}
	return b.Tip, nil
}

// UpdateSiteTipConfig 更新赞赏配置
func (s *ManageService) UpdateSiteTipConfig(tip model.TipConfig) error {
	siteBasicMu.Lock()
	defer siteBasicMu.Unlock()
	curr, err := repobase.GetSiteBasic()
	if err != nil {
		return err
	}
	curr.Tip = tip
	return repobase.SaveSiteBasic(curr)
}

// GetSiteNoticeConfig 获取站点公告配置
func (s *ManageService) GetSiteNoticeConfig() (model.NoticeConfig, error) {
	b, err := repobase.GetSiteBasic()
	if err != nil {
		return model.NoticeConfig{}, err
	}
	return b.Notice, nil
}

// UpdateSiteNoticeConfig 更新站点公告配置
func (s *ManageService) UpdateSiteNoticeConfig(notice model.NoticeConfig) error {
	siteBasicMu.Lock()
	defer siteBasicMu.Unlock()
	curr, err := repobase.GetSiteBasic()
	if err != nil {
		return err
	}
	curr.Notice = notice
	return repobase.SaveSiteBasic(curr)
}
