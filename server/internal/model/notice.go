package model

import (
	"encoding/json"
	"strings"
)

const (
	MaxNoticeTitleLen      = 64
	MaxNoticeContentLen    = 2048
	MaxNoticeAppVersionLen = 128
	DefaultNoticeTitle     = "站点公告"
)

// NoticeConfig 站点公告配置（支持 Web 端与 App 移动端独立控制）
type NoticeConfig struct {
	Enabled    bool   `json:"enabled"`              // 公告总开关
	Title      string `json:"title"`                // 公告标题
	Content    string `json:"content"`              // 公告正文
	ShowInWeb  bool   `json:"showInWeb"`            // 是否在 Web 端展示
	ShowInApp  bool   `json:"showInApp"`            // 是否在 App 移动端展示
	AppVersion string `json:"appVersion"`           // App 目标版本（留空所有版本都弹；多个版本逗号分隔，如 "1.0.2, 1.0.3"）
	Version    string `json:"version,omitempty"`   // 兼容旧字段（等同于 AppVersion）
}

type noticeConfigDTO struct {
	Enabled    bool   `json:"enabled"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	ShowInWeb  *bool  `json:"showInWeb"`
	ShowInApp  *bool  `json:"showInApp"`
	AppVersion string `json:"appVersion"`
	Version    string `json:"version"`
}

// DefaultNoticeConfig 关闭状态的默认站点公告配置
func DefaultNoticeConfig() NoticeConfig {
	return NoticeConfig{
		Enabled:    false,
		Title:      DefaultNoticeTitle,
		Content:    "",
		ShowInWeb:  true,
		ShowInApp:  true,
		AppVersion: "",
		Version:    "",
	}
}

// NormalizeNoticeConfig 清洗站点公告配置
func NormalizeNoticeConfig(n NoticeConfig) NoticeConfig {
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = DefaultNoticeTitle
	}
	title = truncateRunes(title, MaxNoticeTitleLen)

	content := strings.TrimSpace(n.Content)
	content = truncateRunes(content, MaxNoticeContentLen)

	appVer := strings.TrimSpace(n.AppVersion)
	if appVer == "" && strings.TrimSpace(n.Version) != "" {
		appVer = strings.TrimSpace(n.Version)
	}
	appVer = truncateRunes(appVer, MaxNoticeAppVersionLen)

	return NoticeConfig{
		Enabled:    n.Enabled,
		Title:      title,
		Content:    content,
		ShowInWeb:  n.ShowInWeb,
		ShowInApp:  n.ShowInApp,
		AppVersion: appVer,
		Version:    appVer,
	}
}

// EncodeNoticeJSON 将站点公告配置编码为持久化 JSON
func EncodeNoticeJSON(n NoticeConfig) string {
	raw, err := json.Marshal(NormalizeNoticeConfig(n))
	if err != nil {
		return ""
	}
	return string(raw)
}

// DecodeNoticeJSON 解析站点公告配置 JSON，非法或空值回退默认
func DecodeNoticeJSON(raw string) NoticeConfig {
	if strings.TrimSpace(raw) == "" {
		return DefaultNoticeConfig()
	}
	var dto noticeConfigDTO
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		return DefaultNoticeConfig()
	}

	showInWeb := true
	if dto.ShowInWeb != nil {
		showInWeb = *dto.ShowInWeb
	}
	showInApp := true
	if dto.ShowInApp != nil {
		showInApp = *dto.ShowInApp
	}

	appVer := strings.TrimSpace(dto.AppVersion)
	if appVer == "" && strings.TrimSpace(dto.Version) != "" {
		appVer = strings.TrimSpace(dto.Version)
	}

	return NormalizeNoticeConfig(NoticeConfig{
		Enabled:    dto.Enabled,
		Title:      dto.Title,
		Content:    dto.Content,
		ShowInWeb:  showInWeb,
		ShowInApp:  showInApp,
		AppVersion: appVer,
		Version:    appVer,
	})
}
