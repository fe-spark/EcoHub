package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

func normalizeBannerImageFields(bl model.Banners) model.Banners {
	for index := range bl {
		picture := strings.TrimSpace(bl[index].Picture)
		if picture == "" {
			picture = strings.TrimSpace(bl[index].Poster)
		}
		if picture == "" {
			continue
		}

		bl[index].Picture = picture
		if strings.TrimSpace(bl[index].Poster) == "" {
			bl[index].Poster = picture
		}

		// 严禁拿竖版封面/海报填充横屏大图 PictureSlide！
		// 若 PictureSlide 等于 Picture 或 Poster，说明是伪横图，直接置为空字符串
		slide := strings.TrimSpace(bl[index].PictureSlide)
		if slide == picture || slide == strings.TrimSpace(bl[index].Poster) {
			bl[index].PictureSlide = ""
		}
	}

	return bl
}

// ExistSiteConfig 判断 MySQL 中是否已有网站配置。
func ExistSiteConfig() (bool, error) {
	if db.Mdb == nil {
		return false, errors.New("数据库未连接")
	}
	var count int64
	if err := db.Mdb.Model(&model.SiteConfigRecord{}).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// NormalizeSiteURL 规范化网站访问地址：trim、去尾斜杠；非法 scheme 返回空。
func NormalizeSiteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.TrimRight(raw, "/")
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return raw
	}
	// 允许无 scheme 时补 https://
	if strings.Contains(raw, ".") && !strings.Contains(raw, " ") {
		return "https://" + strings.TrimLeft(raw, "/")
	}
	return ""
}

// SaveSiteBasic 保存网站基本配置信息 (MySQL + Redis 短期缓存)
func SaveSiteBasic(c model.BasicConfig) error {
	c.SiteURL = NormalizeSiteURL(c.SiteURL)
	c.Tip = model.NormalizeTipConfig(c.Tip)
	c.Notice = model.NormalizeNoticeConfig(c.Notice)
	tipJSON, err := model.EncodeTipJSON(c.Tip)
	if err != nil {
		return err
	}
	noticeJSON, err := model.EncodeNoticeJSON(c.Notice)
	if err != nil {
		return err
	}
	rec := model.SiteConfigRecord{
		SiteName: c.SiteName, SiteURL: c.SiteURL, Logo: c.Logo,
		Keyword: c.Keyword, Describe: c.Describe, State: c.State, Hint: c.Hint,
		PrivateAccess: c.PrivateAccess, ProvideKey: strings.TrimSpace(c.ProvideKey),
		TipJSON:    tipJSON,
		NoticeJSON: noticeJSON,
	}
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SiteConfigRecord{}).Error; err != nil {
			return err
		}
		return tx.Create(&rec).Error
	}); err != nil {
		return err
	}

	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := writeCache(config.SiteConfigBasic, string(data)); err != nil {
		return err
	}
	ClearIndexPageCache()
	return nil
}

// GetSiteBasic 读取网站基本配置。没有记录时返回默认赞赏和公告，不写入缓存。
func GetSiteBasic() (model.BasicConfig, error) {
	if data, ok, err := readCache(config.SiteConfigBasic); err != nil {
		return model.BasicConfig{}, err
	} else if ok && data != "" {
		var cached model.BasicConfig
		if err := json.Unmarshal([]byte(data), &cached); err != nil {
			if delErr := dropCache(config.SiteConfigBasic); delErr != nil {
				return model.BasicConfig{}, fmt.Errorf("站点配置缓存损坏: %v; 删除失败: %w", err, delErr)
			}
		} else {
			cached.SiteURL = NormalizeSiteURL(cached.SiteURL)
			cached.Tip = model.NormalizeTipConfig(cached.Tip)
			cached.Notice = model.NormalizeNoticeConfig(cached.Notice)
			return cached, nil
		}
	}
	if db.Mdb == nil {
		return model.BasicConfig{}, errors.New("数据库未连接")
	}
	var rec model.SiteConfigRecord
	if err := db.Mdb.Order("id DESC").First(&rec).Error; err != nil {
		if recordNotFound(err) {
			return model.BasicConfig{
				Tip:    model.DefaultTipConfig(),
				Notice: model.DefaultNoticeConfig(),
			}, nil
		}
		return model.BasicConfig{}, err
	}
	tip, err := model.DecodeTipJSON(rec.TipJSON)
	if err != nil {
		return model.BasicConfig{}, err
	}
	notice, err := model.DecodeNoticeJSON(rec.NoticeJSON)
	if err != nil {
		return model.BasicConfig{}, err
	}
	c := model.BasicConfig{
		SiteName: rec.SiteName, SiteURL: NormalizeSiteURL(rec.SiteURL), Logo: rec.Logo,
		Keyword: rec.Keyword, Describe: rec.Describe, State: rec.State, Hint: rec.Hint,
		PrivateAccess: rec.PrivateAccess, ProvideKey: rec.ProvideKey,
		Tip:    tip,
		Notice: notice,
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return model.BasicConfig{}, err
	}
	if err := fillCache(config.SiteConfigBasic, string(raw)); err != nil {
		return model.BasicConfig{}, err
	}
	return c, nil
}

// GetBanners 读取轮播。没有行时返回空切片。查询失败返回错误。
func GetBanners() (model.Banners, error) {
	if data, ok, err := readCache(config.BannersKey); err != nil {
		return nil, err
	} else if ok && data != "" && data != "null" {
		var cached model.Banners
		if err := json.Unmarshal([]byte(data), &cached); err != nil {
			if delErr := dropCache(config.BannersKey); delErr != nil {
				return nil, fmt.Errorf("轮播缓存损坏: %v; 删除失败: %w", err, delErr)
			}
		} else {
			cached = normalizeBannerImageFields(cached)
			sort.Sort(cached)
			return cached, nil
		}
	}
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	bl := make(model.Banners, 0)
	if err := db.Mdb.Order("sort ASC").Find(&bl).Error; err != nil {
		return nil, err
	}
	bl = normalizeBannerImageFields(bl)
	sort.Sort(bl)
	raw, err := json.Marshal(bl)
	if err != nil {
		return nil, err
	}
	if err := fillCache(config.BannersKey, string(raw)); err != nil {
		return nil, err
	}
	return bl, nil
}

// SaveBanners 保存轮播配置信息 (MySQL + Redis 短期缓存)
func SaveBanners(bl model.Banners) error {
	bl = normalizeBannerImageFields(bl)
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		// 清空旧轮播数据
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.Banner{}).Error; err != nil {
			return err
		}
		// 批量插入新数据
		if len(bl) > 0 {
			if err := tx.Create(&bl).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	raw, err := json.Marshal(bl)
	if err != nil {
		return err
	}
	if err := writeCache(config.BannersKey, string(raw)); err != nil {
		return err
	}
	ClearIndexPageCache()
	return nil
}
