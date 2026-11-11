package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/model"

	"github.com/gin-gonic/gin"
)

// 模拟兼容弹弹play协议的自建 danmu-api，路径里带 token
func newFakeDanmuAPI(t *testing.T, search, comment func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/secret-token/api/v2/search/episodes", func(w http.ResponseWriter, r *http.Request) { search(w) })
	mux.HandleFunc("/secret-token/api/v2/comment/", func(w http.ResponseWriter, r *http.Request) { comment(w) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const fakeSearchHit = `{"success":true,"animes":[{"animeTitle":"测试动画","episodes":[{"episodeId":101,"episodeTitle":"第1话"},{"episodeId":102,"episodeTitle":"第2话"}]}]}`

func sniffCfg(srv *httptest.Server) model.DanmakuConfig {
	return model.DanmakuConfig{Enabled: true, AutoSniff: true, ApiURL: srv.URL + "/secret-token/api/v2"}
}

func assertNoSecret(t *testing.T, srv *httptest.Server, reason string) {
	t.Helper()
	if strings.Contains(reason, "secret-token") || strings.Contains(reason, srv.URL) {
		t.Fatalf("reason 泄露了接口地址或 token: %q", reason)
	}
}

func TestFetchSniffedDanmaku_Statuses(t *testing.T) {
	setupDanmakuProxyDB(t, nil) // 没有代理配置：按默认直连
	writeJSON := func(body string) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
	}
	writeStatus := func(code int) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) { w.WriteHeader(code) }
	}
	cases := []struct {
		name       string
		search     func(http.ResponseWriter)
		comment    func(http.ResponseWriter)
		wantStatus string
		wantCount  int
		wantReason string
	}{
		{"ok", writeJSON(fakeSearchHit), writeJSON(`{"count":2,"comments":[{"p":"1.5,1,16777215,u","m":"你好"},{"p":"3,5,0,u","m":"顶部黑色"}]}`), networkOK, 2, ""},
		{"no match", writeJSON(`{"success":true,"animes":[]}`), writeStatus(500), networkEmpty, 0, "没有匹配到这部片"},
		{"episode empty", writeJSON(fakeSearchHit), writeJSON(`{"count":0,"comments":[]}`), networkEmpty, 0, "这一集没有弹幕"},
		{"search 403", writeStatus(http.StatusForbidden), writeStatus(500), networkError, 0, "HTTP 403"},
		{"comment 502", writeJSON(fakeSearchHit), writeStatus(http.StatusBadGateway), networkError, 0, "HTTP 502"},
		{"bad json", writeJSON(`<html>`), writeStatus(500), networkError, 0, "无法解析"},
		{"business error", writeJSON(`{"success":false,"errorCode":1,"errorMessage":"服务器内部错误"}`), writeStatus(500), networkError, 0, "服务器内部错误"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeDanmuAPI(t, tc.search, tc.comment)
			res := fetchSniffedDanmaku(sniffCfg(srv), "测试动画", 2, false)
			if res.Status != tc.wantStatus || len(res.Items) != tc.wantCount {
				t.Fatalf("status=%s count=%d reason=%q, want %s/%d", res.Status, len(res.Items), res.Reason, tc.wantStatus, tc.wantCount)
			}
			if tc.wantReason != "" && !strings.Contains(res.Reason, tc.wantReason) {
				t.Fatalf("reason=%q, want contains %q", res.Reason, tc.wantReason)
			}
			assertNoSecret(t, srv, res.Reason)
			if tc.wantStatus == networkOK && (res.AnimeTitle != "测试动画" || res.EpisodeTitle != "第2话") {
				t.Fatalf("match = %q/%q", res.AnimeTitle, res.EpisodeTitle)
			}
		})
	}
}

func TestFetchSniffedDanmaku_Unreachable(t *testing.T) {
	setupDanmakuProxyDB(t, nil) // 没有代理配置：按默认直连
	srv := httptest.NewServer(http.NotFoundHandler())
	cfg := sniffCfg(srv)
	srv.Close()
	res := fetchSniffedDanmaku(cfg, "测试动画", 1, false)
	if res.Status != networkError || res.Reason != "无法连接弹幕接口" {
		t.Fatalf("got %s %q", res.Status, res.Reason)
	}
	assertNoSecret(t, srv, res.Reason)
}

func TestFetchSniffedDanmaku_NotConfigured(t *testing.T) {
	res := fetchSniffedDanmaku(model.DanmakuConfig{Enabled: true, AutoSniff: true}, "测试动画", 1, false)
	if res.Status != networkError || !strings.Contains(res.Reason, "AppId") {
		t.Fatalf("got %s %q", res.Status, res.Reason)
	}
}

func TestStatusFailure_OfficialAuthHint(t *testing.T) {
	if got := statusFailure(http.StatusForbidden, true).Error(); !strings.Contains(got, "AppId / AppSecret") {
		t.Fatalf("official 403 reason = %q", got)
	}
	if got := statusFailure(http.StatusForbidden, false).Error(); !strings.Contains(got, "token") {
		t.Fatalf("custom 403 reason = %q", got)
	}
}

// 响应顶层带 meta，data 仍是数组（鸿蒙端按数组解析）
func TestRespondDanmaku_KeepsDataArray(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	respondDanmaku(c, []model.DanmakuItem{{Text: "a", Time: 1}}, danmakuMeta{Local: 1, NetworkStatus: networkEmpty, NetworkReason: "没有匹配到这部片"}, "获取弹幕成功")

	var resp struct {
		Code int                 `json:"code"`
		Data []model.DanmakuItem `json:"data"`
		Meta danmakuMeta         `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, w.Body.String())
	}
	if resp.Code != 0 || len(resp.Data) != 1 || resp.Meta.Local != 1 || resp.Meta.NetworkStatus != networkEmpty {
		t.Fatalf("unexpected response %s", w.Body.String())
	}
}
