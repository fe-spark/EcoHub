package model

import (
	"testing"
)

func TestNormalizeNoticeConfig(t *testing.T) {
	// 测试默认留空
	n := NormalizeNoticeConfig(NoticeConfig{})
	if n.Title != DefaultNoticeTitle {
		t.Errorf("expected default title %s, got %s", DefaultNoticeTitle, n.Title)
	}
	if n.AppVersion != "" || n.Version != "" {
		t.Errorf("expected default version empty, got appVersion=%s, version=%s", n.AppVersion, n.Version)
	}

	// 测试自定义赋值（支持多版本逗号分隔，并自动同步 AppVersion 与 Version）
	custom := NoticeConfig{
		Enabled:    true,
		Title:      "  新版本更新公告  ",
		Content:    "  修复了已知问题并优化了播放体验  ",
		ShowInWeb:  true,
		ShowInApp:  false,
		AppVersion: "  1.0.2, 1.0.3  ",
	}
	norm := NormalizeNoticeConfig(custom)
	if norm.Title != "新版本更新公告" {
		t.Errorf("unexpected title: %s", norm.Title)
	}
	if norm.Content != "修复了已知问题并优化了播放体验" {
		t.Errorf("unexpected content: %s", norm.Content)
	}
	if !norm.ShowInWeb || norm.ShowInApp {
		t.Errorf("unexpected platforms: web=%v, app=%v", norm.ShowInWeb, norm.ShowInApp)
	}
	if norm.AppVersion != "1.0.2, 1.0.3" || norm.Version != "1.0.2, 1.0.3" {
		t.Errorf("unexpected version: appVersion=%s, version=%s", norm.AppVersion, norm.Version)
	}
}

func TestEncodeDecodeNoticeJSON(t *testing.T) {
	origin := NoticeConfig{
		Enabled:    true,
		Title:      "全服维护通知",
		Content:    "今晚 0:00 进行服务器网络升级",
		ShowInWeb:  true,
		ShowInApp:  true,
		AppVersion: "1.0.2, 1.0.3",
	}
	encoded := EncodeNoticeJSON(origin)
	if encoded == "" {
		t.Fatal("encoded JSON should not be empty")
	}

	decoded := DecodeNoticeJSON(encoded)
	if !decoded.Enabled || decoded.Title != origin.Title || decoded.Content != origin.Content ||
		!decoded.ShowInWeb || !decoded.ShowInApp || decoded.AppVersion != origin.AppVersion || decoded.Version != origin.AppVersion {
		t.Fatalf("decoded notice mismatch: %+v", decoded)
	}

	// 兼容旧格式（无 showInWeb / showInApp，使用 version 字段）
	legacyJSON := `{"enabled":true,"title":"旧版公告","content":"旧版内容","version":"1.0.0"}`
	legacyDecoded := DecodeNoticeJSON(legacyJSON)
	if !legacyDecoded.Enabled || legacyDecoded.Title != "旧版公告" || legacyDecoded.Content != "旧版内容" {
		t.Fatalf("legacy decoded basic info mismatch: %+v", legacyDecoded)
	}
	if !legacyDecoded.ShowInWeb || !legacyDecoded.ShowInApp {
		t.Fatalf("legacy decoded should default platforms to true: %+v", legacyDecoded)
	}
	if legacyDecoded.AppVersion != "1.0.0" || legacyDecoded.Version != "1.0.0" {
		t.Fatalf("legacy decoded version mismatch: appVersion=%s, version=%s", legacyDecoded.AppVersion, legacyDecoded.Version)
	}
}
