package repository

import (
	"encoding/json"
	"log"
	"sort"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// ExistSiteConfig 判断 MySQL 中是否已有网站配置
func ExistSiteConfig() bool {
	var count int64
	db.Mdb.Model(&model.SiteConfigRecord{}).Count(&count)
	return count > 0
}

// ExistBannersConfig 判断 MySQL 中是否已有轮播配置
func ExistBannersConfig() bool {
	var count int64
	db.Mdb.Model(&model.Banner{}).Count(&count)
	return count > 0
}

// SaveSiteBasic 保存网站基本配置信息 (MySQL + Redis 短期缓存)
func SaveSiteBasic(c model.BasicConfig) error {
	rec := model.SiteConfigRecord{
		SiteName: c.SiteName, Logo: c.Logo,
		Keyword: c.Keyword, Describe: c.Describe, State: c.State, Hint: c.Hint,
		IsVideoProxy: c.IsVideoProxy,
	}
	// 采用覆盖式更新 (因为只维护单行配置)
	db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SiteConfigRecord{})
	if err := db.Mdb.Create(&rec).Error; err != nil {
		return err
	}
	// write-through
	data, _ := json.Marshal(c)
	err := db.Rdb.Set(db.Cxt, config.SiteConfigBasic, data, config.ConfigCacheTTL).Err()
	if err == nil {
		// 主动同步清理首页缓存
		ClearIndexPageCache()
	}
	return err
}

// GetSiteBasic 获取网站基本配置信息 (Redis 缓存优先，MySQL 兜底)
func GetSiteBasic() model.BasicConfig {
	c := model.BasicConfig{}
	// 1. Redis 缓存
	if data := db.Rdb.Get(db.Cxt, config.SiteConfigBasic).Val(); data != "" {
		_ = json.Unmarshal([]byte(data), &c)
		return c
	}
	// 2. MySQL 兜底
	var rec model.SiteConfigRecord
	if err := db.Mdb.Order("id DESC").First(&rec).Error; err != nil {
		log.Println("GetSiteBasic MySQL Error:", err)
		return c
	}
	c = model.BasicConfig{
		SiteName: rec.SiteName, Logo: rec.Logo,
		Keyword: rec.Keyword, Describe: rec.Describe, State: rec.State, Hint: rec.Hint,
		IsVideoProxy: rec.IsVideoProxy,
	}
	// 回填缓存
	data, _ := json.Marshal(c)
	_ = db.Rdb.Set(db.Cxt, config.SiteConfigBasic, data, config.ConfigCacheTTL).Err()
	return c
}

// GetBanners 获取轮播配置信息 (Redis 缓存优先，MySQL 兜底)
func GetBanners() model.Banners {
	bl := make(model.Banners, 0)
	// 1. Redis 缓存
	data := db.Rdb.Get(db.Cxt, config.BannersKey).Val()
	if data != "" && data != "null" {
		if err := json.Unmarshal([]byte(data), &bl); err == nil && len(bl) > 0 {
			sort.Sort(bl)
			return bl
		}
	}
	// 2. MySQL 兜底
	if err := db.Mdb.Order("sort ASC").Find(&bl).Error; err != nil {
		log.Println("GetBanners MySQL Error:", err)
		return bl
	}

	if len(bl) > 0 {
		sort.Sort(bl)
		// 回填缓存
		data, _ := json.Marshal(bl)
		_ = db.Rdb.Set(db.Cxt, config.BannersKey, data, config.ConfigCacheTTL).Err()
	}
	return bl
}

// SaveBanners 保存轮播配置信息 (MySQL + Redis 短期缓存)
func SaveBanners(bl model.Banners) error {
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
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
		// write-through cache
		data, _ := json.Marshal(bl)
		err := db.Rdb.Set(db.Cxt, config.BannersKey, data, config.ConfigCacheTTL).Err()
		if err == nil {
			// Banner 变动也刷新首页
			ClearIndexPageCache()
		}
		return err
	})
}
