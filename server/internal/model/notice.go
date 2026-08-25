package model

import (
	"encoding/json"
	"strings"
)

const (
	MaxNoticeTitleLen   = 64
	MaxNoticeContentLen = 2048
	MaxNoticeVersionLen = 128
	DefaultNoticeTitle  = "站点公告"
)

// NoticeConfig 开屏公告配置
type NoticeConfig struct {
	Enabled bool   `json:"enabled"`
	Title   string `json:"title"`
	Content string `json:"content"`
	// Version 目标版本列表（留空表示所有版本都弹；多个版本逗号分隔，如 "1.0.2, 1.0.3"）
	Version string `json:"version"`
}

// DefaultNoticeConfig 关闭状态的默认开屏公告配置
func DefaultNoticeConfig() NoticeConfig {
	return NoticeConfig{
		Enabled: false,
		Title:   DefaultNoticeTitle,
		Content: "",
		Version: "", // 默认留空，所有版本生效
	}
}

// NormalizeNoticeConfig 清洗开屏公告配置
func NormalizeNoticeConfig(n NoticeConfig) NoticeConfig {
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = DefaultNoticeTitle
	}
	title = truncateRunes(title, MaxNoticeTitleLen)

	content := strings.TrimSpace(n.Content)
	content = truncateRunes(content, MaxNoticeContentLen)

	version := strings.TrimSpace(n.Version)
	version = truncateRunes(version, MaxNoticeVersionLen)

	return NoticeConfig{
		Enabled: n.Enabled,
		Title:   title,
		Content: content,
		Version: version,
	}
}

// EncodeNoticeJSON 将开屏公告配置编码为持久化 JSON
func EncodeNoticeJSON(n NoticeConfig) string {
	raw, err := json.Marshal(NormalizeNoticeConfig(n))
	if err != nil {
		return ""
	}
	return string(raw)
}

// DecodeNoticeJSON 解析开屏公告配置 JSON，非法或空值回退默认
func DecodeNoticeJSON(raw string) NoticeConfig {
	if strings.TrimSpace(raw) == "" {
		return DefaultNoticeConfig()
	}
	var n NoticeConfig
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		return DefaultNoticeConfig()
	}
	return NormalizeNoticeConfig(n)
}
