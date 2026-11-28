package ingest

import (
	"net/http"
	"strings"

	appcfg "server/internal/config"
)

// ShouldSkip 过滤纯基础设施噪声（健康探活成功请求、海报静态资源、OPTIONS 预检及日志订阅流）
func ShouldSkip(method, path string, status int) bool {
	if strings.EqualFold(method, http.MethodOptions) {
		return true
	}
	if path == "/api/config/basic" && status < 400 {
		return true
	}
	if path == "/api/health" && (method == http.MethodGet || method == http.MethodHead) {
		return true
	}
	if method == http.MethodGet && (path == "/api/manage/system/logs/delta" || path == "/api/manage/system/logs/stream") {
		return true
	}
	return strings.HasPrefix(path, appcfg.FilmPictureAccess)
}

func isProvidePath(path string) bool {
	return strings.HasPrefix(path, "/api/provide/")
}

func isManagePath(path string) bool {
	return strings.HasPrefix(path, "/api/manage/")
}

func httpKind(path string) string {
	switch {
	case isProvidePath(path):
		return "provide"
	case isManagePath(path):
		return "manage"
	default:
		return "http"
	}
}

func clientFromUA(ua string) string {
	switch {
	case strings.Contains(ua, "EcoHub-iOS") || strings.Contains(ua, "EcoHub-IOS") ||
		strings.Contains(ua, "EcoHub-OHOS") || strings.Contains(ua, "EcoHub-Android") ||
		strings.Contains(ua, "EcoHub-App") || strings.Contains(ua, "EcoHubApp") ||
		strings.Contains(ua, "EcoHub/"):
		return "app"
	case isCrawlerUA(ua):
		return "crawler"
	default:
		return "web"
	}
}

// ClassifyHTTPClient 根据请求路径与 UA 识别客户端类型
func ClassifyHTTPClient(path, ua string) string {
	if c := clientFromUA(ua); c != "web" {
		return c
	}
	if isProvidePath(path) {
		return "tvbox"
	}
	if isManagePath(path) {
		return "manage"
	}
	return "web"
}

func isCrawlerUA(ua string) bool {
	if strings.Contains(ua, "EcoHub-SSR") {
		return false
	}
	lower := strings.ToLower(ua)
	for _, key := range []string{"bot", "spider", "crawler", "curl", "wget"} {
		if strings.Contains(lower, key) {
			return true
		}
	}
	return false
}

func uaFamily(path, ua string) string {
	switch {
	case strings.Contains(ua, "EcoHub-SSR"):
		return "ecohub-ssr"
	case strings.Contains(ua, "EcoHub-iOS") || strings.Contains(ua, "EcoHub-IOS"):
		return "ecohub-ios"
	case strings.Contains(ua, "EcoHub-OHOS"):
		return "ecohub-ohos"
	case strings.Contains(ua, "EcoHub-Android"):
		return "ecohub-android"
	case isProvidePath(path):
		return "tvbox"
	case isCrawlerUA(ua):
		return "bot"
	}
	lower := strings.ToLower(ua)
	switch {
	case strings.Contains(lower, "edg/"):
		return "edge"
	case strings.Contains(lower, "chrome/"):
		return "chrome"
	case strings.Contains(lower, "safari/") && strings.Contains(lower, "version/"):
		return "safari"
	case strings.Contains(lower, "firefox/"):
		return "firefox"
	default:
		return "other"
	}
}

func detectOS(ua string) string {
	lower := strings.ToLower(ua)
	switch {
	case strings.Contains(lower, "windows"):
		return "Windows"
	case strings.Contains(lower, "macintosh") || strings.Contains(lower, "mac os x"):
		return "macOS"
	case strings.Contains(lower, "android"):
		return "Android"
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") || strings.Contains(lower, "ios"):
		return "iOS"
	case strings.Contains(lower, "openharmony") || strings.Contains(lower, "harmony"):
		return "HarmonyOS"
	case strings.Contains(lower, "linux"):
		return "Linux"
	default:
		return "Other"
	}
}
