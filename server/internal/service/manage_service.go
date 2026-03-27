package service

import (
	"server/internal/model"
	"server/internal/repository"
)

type ManageService struct{}

// NewManageService 创建管理服务实例
func NewManageService() *ManageService {
	return &ManageService{}
}

var ManageSvc = new(ManageService)

// GetSiteBasicConfig 获取网站基本配置信息
func (s *ManageService) GetSiteBasicConfig() model.BasicConfig {
	return repository.GetSiteBasic()
}

// UpdateSiteBasic 更新网站配置信息
func (s *ManageService) UpdateSiteBasic(c model.BasicConfig) error {
	return repository.SaveSiteBasic(c)
}

// ResetSiteBasic 重置网站配置信息
func (s *ManageService) ResetSiteBasic() error {
	return repository.SaveSiteBasic(defaultBasicConfig())
}

// GetBanners 获取轮播组件信息
func (s *ManageService) GetBanners() model.Banners {
	return repository.GetBanners()
}

// SaveBanners 保存轮播信息
func (s *ManageService) SaveBanners(bl model.Banners) error {
	return repository.SaveBanners(bl)
}
