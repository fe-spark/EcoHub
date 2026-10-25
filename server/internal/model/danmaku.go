package model

import (
	"encoding/json"
	"strings"
	"time"
)

type DanmakuConfig struct {
	Enabled   bool   `json:"enabled"`   // 弹幕功能总开关，默认 true
	AutoSniff bool   `json:"autoSniff"` // 自动嗅探全网弹幕，默认 true
	ApiURL    string `json:"apiUrl"`    // 自定义弹幕接口地址，留空走官方弹弹play接口
	AppID     string `json:"appId"`     // 弹弹play 开放平台 AppId
	AppSecret string `json:"appSecret"` // 弹弹play 开放平台 AppSecret
}

func DefaultDanmakuConfig() DanmakuConfig {
	return DanmakuConfig{
		Enabled:   true,
		AutoSniff: true,
		ApiURL:    "",
	}
}

func EncodeDanmakuJSON(c DanmakuConfig) (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func DecodeDanmakuJSON(raw string) (DanmakuConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultDanmakuConfig(), nil
	}
	var c DanmakuConfig
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return DanmakuConfig{}, err
	}
	return c, nil
}

type DanmakuRecord struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Mid       int64     `gorm:"index:idx_mid_episode;not null" json:"mid"`
	Episode   int       `gorm:"index:idx_mid_episode;default:1" json:"episode"`
	Time      float64   `gorm:"type:decimal(10,2);not null" json:"time"`
	Text      string    `gorm:"size:255;not null" json:"text"`
	Color     string    `gorm:"size:16;default:'#ffffff'" json:"color"`
	Mode      int       `gorm:"default:0" json:"mode"` // 0: 滚动, 1: 顶部, 2: 底部
	UserID    int64     `gorm:"default:0" json:"userId"`
	IP        string    `gorm:"size:64" json:"ip"`
}

func (DanmakuRecord) TableName() string {
	return TableDanmakuRecord
}

// DanmakuItem 前端播放器 (Artplayer) 弹幕通用标准格式
type DanmakuItem struct {
	Text   string  `json:"text"`
	Time   float64 `json:"time"`
	Color  string  `json:"color"`
	Mode   int     `json:"mode"` // 0: scroll, 1: top, 2: bottom
	Border bool    `json:"border"`
}
