package access

import (
	"net/http"
	"strings"

	"server/internal/config"
)

// ShouldSkip 仅过滤日志噪声与自采样，不参与 PV。
func ShouldSkip(method, path string, status int) bool {
	if strings.EqualFold(method, http.MethodOptions) {
		return true
	}
	switch path {
	case "/api/health":
		return method == http.MethodGet || method == http.MethodHead
	case "/api/stat/view":
		return true
	case "/api/manage/system/logs/delta":
		return true
	case "/api/manage/collect/list":
		return status < 400
	case "/api/index/dailyUpdates", "/api/dailyUpdates":
		return true
	}
	if path == "/api/manage/access" || strings.HasPrefix(path, "/api/manage/access/") {
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
