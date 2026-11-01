package service

import (
	"strings"

	"server/internal/model"
)

// SaveBannerDeck 保存排片。刮削结果写入轮播行，不写影片，也不写刮削表。
func (s *ManageService) SaveBannerDeck(bl model.Banners) error {
	for i := range bl {
		bl[i].Sort = int64(i + 1)
		scraped := bl[i].TmdbID > 0 || bl[i].PendingScrape
		bl[i].PendingScrape = false
		if scraped || strings.TrimSpace(bl[i].PictureSlide) != "" {
			bl[i].IsCustomPic = true
			if strings.TrimSpace(bl[i].CustomPicture) == "" && strings.TrimSpace(bl[i].Picture) != "" {
				bl[i].CustomPicture = strings.TrimSpace(bl[i].Picture)
			}
			if strings.TrimSpace(bl[i].Poster) == "" {
				bl[i].Poster = strings.TrimSpace(bl[i].Picture)
			}
		}
	}
	return s.SaveBanners(bl)
}
