package model

import (
	"encoding/json"
	"errors"
	"fmt"
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
	Enabled    bool   `json:"enabled"`    // 公告总开关
	Title      string `json:"title"`      // 公告标题
	Content    string `json:"content"`    // 公告正文
	ShowInWeb  bool   `json:"showInWeb"`  // 是否在 Web 端展示
	ShowInApp  bool   `json:"showInApp"`  // 是否在 App 移动端展示
	AppVersion string `json:"appVersion"` // App 目标版本（留空所有版本都弹；多个版本逗号分隔，如 "1.0.2, 1.0.3"）
}

type noticeConfigDTO struct {
	Enabled    bool   `json:"enabled"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	ShowInWeb  *bool  `json:"showInWeb"`
	ShowInApp  *bool  `json:"showInApp"`
	AppVersion string `json:"appVersion"`
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

	return NoticeConfig{
		Enabled:    n.Enabled,
		Title:      title,
		Content:    content,
		ShowInWeb:  n.ShowInWeb,
		ShowInApp:  n.ShowInApp,
		AppVersion: truncateRunes(strings.TrimSpace(n.AppVersion), MaxNoticeAppVersionLen),
	}
}

// EncodeNoticeJSON 将站点公告配置编码为持久化 JSON
func EncodeNoticeJSON(n NoticeConfig) (string, error) {
	raw, err := json.Marshal(NormalizeNoticeConfig(n))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// DecodeNoticeJSON 解析站点公告配置。空字符串是未配置。缺开关或 JSON 非法返回错误。
func DecodeNoticeJSON(raw string) (NoticeConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultNoticeConfig(), nil
	}
	var dto noticeConfigDTO
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		return NoticeConfig{}, fmt.Errorf("公告配置无法解析: %w", err)
	}
	if dto.ShowInWeb == nil || dto.ShowInApp == nil {
		return NoticeConfig{}, errors.New("公告配置缺少展示开关")
	}
	return NormalizeNoticeConfig(NoticeConfig{
		Enabled:    dto.Enabled,
		Title:      dto.Title,
		Content:    dto.Content,
		ShowInWeb:  *dto.ShowInWeb,
		ShowInApp:  *dto.ShowInApp,
		AppVersion: dto.AppVersion,
	}), nil
}
