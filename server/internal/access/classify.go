package access

import (
	"net/http"
	"strings"

	"server/internal/config"
)

// ShouldRecordApiLog 接口审计写入过滤：
// - 后台与分析侧噪声路径（海报、探活、埋点）一律排除；
// - TVBox provide 成功流量已由访问分析 Redis 统计，不再重复入库，仅保留 4xx/5xx 错误审计。
func ShouldRecordApiLog(method, path string, status int) bool {
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "/manage") || strings.HasPrefix(path, "/api/manage") {
		return false
	}
	if isProvidePath(path) && status < 400 {
		return false
	}
	return !ShouldSkip(method, path, status)
}

// ShouldSkip 仅过滤日志噪声与自采样，不参与 PV。
func ShouldSkip(method, path string, status int) bool {
	if strings.EqualFold(method, http.MethodOptions) {
		return true
	}
	if strings.HasPrefix(path, "/api/manage") {
		return true
	}
	switch path {
	case "/api/health":
		return method == http.MethodGet || method == http.MethodHead
	case "/api/config/basic":
		return status < 400
	case "/api/stat/view":
		return true
	case "/api/index/dailyUpdates", "/api/dailyUpdates":
		return true
	}
	return strings.HasPrefix(path, config.FilmPictureAccess)
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
