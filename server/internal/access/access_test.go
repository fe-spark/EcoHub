package access

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"server/internal/config"
	"server/internal/model"

	"github.com/gin-gonic/gin"
)

func TestShouldSkip(t *testing.T) {
	cases := []struct {
		method, path string
		status       int
		skip         bool
	}{
		{"OPTIONS", "/api/index", 204, true},
		{"GET", "/api/config/basic", 200, true},
		{"GET", "/api/config/basic", 500, false},
		{"GET", "/api/health", 200, true},
		{"HEAD", "/api/health", 200, true},
		{"POST", "/api/health", 200, false},
		{"GET", "/api/upload/pic/poster/a.jpg", 200, true},
		// 业务管理与前台接口均不应被跳过（必须正常打印日志）
		{"GET", "/api/manage", 200, false},
		// 运行日志增量接口自轮询，需跳过以免刷屏
		{"GET", "/api/manage/system/logs/delta", 200, true},
		{"GET", "/api/manage/collect/list", 200, false},
		{"POST", "/api/manage/film/add", 200, false},
		{"GET", "/api/dailyUpdates", 200, false},
		{"GET", "/api/index/dailyUpdates", 200, false},
		{"POST", "/api/stat/view", 200, false},
		{"GET", "/api/index", 200, false},
		{"GET", "/api/provide/vod", 200, false},
	}
	for _, c := range cases {
		got := ShouldSkip(c.method, c.path, c.status)
		if got != c.skip {
			t.Fatalf("%s %s %d skip=%v want %v", c.method, c.path, c.status, got, c.skip)
		}
	}
}
