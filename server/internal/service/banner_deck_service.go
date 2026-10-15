package service

import (
	"server/internal/model"
)

// SaveBannerDeck 保存排片卡组列表与排序。轮播刮削物料独立存储于轮播，不改动片库影视列表。
func (s *ManageService) SaveBannerDeck(bl model.Banners) error {
	for i := range bl {
		bl[i].Sort = int64(i + 1)
		bl[i].PendingScrape = false
		// 轮播专属横图与海报独立持久化，不随片库快照冲刷，且不污染影视列表
		if bl[i].PictureSlide != "" {
			bl[i].IsCustomPic = true
			if bl[i].CustomPicture == "" && bl[i].Picture != "" {
				bl[i].CustomPicture = bl[i].Picture
			}
		}
	}
	return s.SaveBanners(bl)
}
