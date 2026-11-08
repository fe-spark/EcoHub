package service

import (
	"errors"
	"server/internal/access"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/utils"
	"strings"
)

type ManageService struct{}

var ManageSvc = new(ManageService)

// AccessGuardConfig 返回私有化开关和订阅密钥。
func (s *ManageService) AccessGuardConfig() (bool, string, error) {
	cfg, err := repository.GetSiteBasic()
	if err != nil {
		return false, "", err
	}
	return cfg.PrivateAccess, strings.TrimSpace(cfg.ProvideKey), nil
}

// GetSiteBasicConfig 获取网站基本配置信息
func (s *ManageService) GetSiteBasicConfig() (model.BasicConfig, error) {
	return repository.GetSiteBasic()
}

// UpdateSiteBasic 更新网站名称和简介。访问控制、弹幕、缺省的赞赏和公告保留。
func (s *ManageService) UpdateSiteBasic(bc model.BasicConfig) error {
	curr, err := repository.GetSiteBasic()
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
		curr.MediaProxy = bc.MediaProxy
	}
	if err := repository.SaveSiteBasic(curr); err != nil {
		return err
	}
	UpdateMediaProxyRuntime(curr.MediaProxy, curr.SiteURL)
	return nil
}

// GetSiteMediaProxyConfig 获取媒体图片边缘反向代理配置
func (s *ManageService) GetSiteMediaProxyConfig() (model.MediaProxyConfig, error) {
	b, err := repository.GetSiteBasic()
	if err != nil {
		return model.MediaProxyConfig{}, err
	}
	return b.MediaProxy, nil
}

// UpdateSiteMediaProxyConfig 更新媒体图片边缘反向代理配置
func (s *ManageService) UpdateSiteMediaProxyConfig(cfg model.MediaProxyConfig) error {
	if err := ValidateMediaProxyConfig(cfg); err != nil {
		return err
	}
	curr, err := repository.GetSiteBasic()
	if err != nil {
		return err
	}
	curr.MediaProxy = cfg
	if err := repository.SaveSiteBasic(curr); err != nil {
		return err
	}
	UpdateMediaProxyRuntime(curr.MediaProxy, curr.SiteURL)
	repository.ClearIndexPageCache()
	return nil
}

// GetSiteAccessConfig 获取网站访问控制配置
func (s *ManageService) GetSiteAccessConfig() (model.AccessConfig, error) {
	b, err := repository.GetSiteBasic()
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
	curr, err := repository.GetSiteBasic()
	if err != nil {
		return err
	}
	curr.State = ac.State
	curr.Hint = ac.Hint
	curr.PrivateAccess = ac.PrivateAccess
	if curr.PrivateAccess {
		if strings.TrimSpace(ac.ProvideKey) == "" {
			key, err := utils.RandomString(8)
			if err != nil {
				return err
			}
			curr.ProvideKey = key
		} else {
			curr.ProvideKey = strings.TrimSpace(ac.ProvideKey)
		}
	} else {
		curr.ProvideKey = ac.ProvideKey
	}
	return repository.SaveSiteBasic(curr)
}

// GetSiteDanmakuConfig 获取网站弹幕系统配置
func (s *ManageService) GetSiteDanmakuConfig() (model.DanmakuConfig, error) {
	b, err := repository.GetSiteBasic()
	if err != nil {
		return model.DanmakuConfig{}, err
	}
	return b.Danmaku, nil
}

// UpdateSiteDanmakuConfig 更新网站弹幕系统配置
func (s *ManageService) UpdateSiteDanmakuConfig(cfg model.DanmakuConfig) error {
	curr, err := repository.GetSiteBasic()
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
	if strings.TrimSpace(next.AppID) == "" {
		next.AppID = curr.Danmaku.AppID
	}
	if strings.TrimSpace(next.AppSecret) == "" {
		next.AppSecret = curr.Danmaku.AppSecret
	}
	curr.Danmaku = next
	return repository.SaveSiteBasic(curr)
}

// GetSiteTipConfig 获取赞赏配置
func (s *ManageService) GetSiteTipConfig() (model.TipConfig, error) {
	b, err := repository.GetSiteBasic()
	if err != nil {
		return model.TipConfig{}, err
	}
	return b.Tip, nil
}

// UpdateSiteTipConfig 更新赞赏配置
func (s *ManageService) UpdateSiteTipConfig(tip model.TipConfig) error {
	curr, err := repository.GetSiteBasic()
	if err != nil {
		return err
	}
	curr.Tip = tip
	return repository.SaveSiteBasic(curr)
}

// GetSiteNoticeConfig 获取站点公告配置
func (s *ManageService) GetSiteNoticeConfig() (model.NoticeConfig, error) {
	b, err := repository.GetSiteBasic()
	if err != nil {
		return model.NoticeConfig{}, err
	}
	return b.Notice, nil
}

// UpdateSiteNoticeConfig 更新站点公告配置
func (s *ManageService) UpdateSiteNoticeConfig(notice model.NoticeConfig) error {
	curr, err := repository.GetSiteBasic()
	if err != nil {
		return err
	}
	curr.Notice = notice
	return repository.SaveSiteBasic(curr)
}

// GetAnalyticsConfig 获取数据分析配置
func (s *ManageService) GetAnalyticsConfig() (model.AnalyticsConfig, error) {
	cfg, err := repository.GetAnalyticsConfig()
	if err != nil {
		return model.DefaultAnalyticsConfig(), err
	}
	access.SetEnabled(cfg.Enabled)
	return cfg, nil
}

// UpdateAnalyticsConfig 更新数据分析配置并即时热生效
func (s *ManageService) UpdateAnalyticsConfig(cfg model.AnalyticsConfig) error {
	if err := repository.SaveAnalyticsConfig(cfg); err != nil {
		return err
	}
	access.SetEnabled(cfg.Enabled)
	return nil
}

// GetBanners 获取轮播。更新状态跟片库，图片和简介用轮播行自己的。
func (s *ManageService) GetBanners() (model.Banners, error) {
	banners, err := repository.GetBanners()
	if err != nil {
		return nil, err
	}
	return OverlayBannerLiveRemarks(banners), nil
}

// SaveBanners 保存轮播信息
func (s *ManageService) SaveBanners(bl model.Banners) error {
	if len(bl) == 0 {
		return repository.SaveBanners(bl)
	}
	// 若轮播项为跟随海报源（IsCustomPic == false），自动查询快照获取最新海报与幻灯图打底并清空 custom_picture
	mids := make([]int64, 0, len(bl))
	for _, b := range bl {
		if b.Mid > 0 && !b.IsCustomPic && b.TmdbID <= 0 {
			mids = append(mids, b.Mid)
		}
	}
	if len(mids) > 0 {
		liveData := filmrepo.LiveBannerSnapshotsByMIDs(mids)
		for i := range bl {
			if !bl[i].IsCustomPic && bl[i].TmdbID <= 0 {
				bl[i].CustomPicture = ""
				if snap, ok := liveData[bl[i].Mid]; ok {
					dispPic := snap.DisplayPicture()
					if dispPic != "" {
						bl[i].Picture = dispPic
						bl[i].Poster = dispPic
					}
					dispSlide := snap.DisplayPictureSlide()
					if dispSlide != "" && dispSlide != dispPic {
						bl[i].PictureSlide = dispSlide
					} else {
						bl[i].PictureSlide = ""
					}
					if bl[i].Blurb == "" {
						bl[i].Blurb = snap.Blurb
					}
					if bl[i].Score == 0 && snap.Score > 0 {
						bl[i].Score = snap.Score
					}
					if bl[i].Area == "" {
						bl[i].Area = snap.Area
					}
					if bl[i].ClassTag == "" {
						bl[i].ClassTag = snap.ClassTag
					}
					if bl[i].Actor == "" {
						bl[i].Actor = snap.Actor
					}
					if bl[i].Director == "" {
						bl[i].Director = snap.Director
					}
				}
			}
		}
	}
	return repository.SaveBanners(bl)
}

func dtoError(msg string) error {
	return &manageError{message: msg}
}

type manageError struct {
	message string
}

func (e *manageError) Error() string {
	return e.message
}
