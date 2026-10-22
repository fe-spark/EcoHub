package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/router"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupIntegrationContext(t *testing.T) (*gin.Engine, string, uint) {
	t.Helper()
	gdb := newTestDB(t)
	_ = useRedis(t)

	adminUserID := uint(config.UserIdInitialVal)
	adminToken, err := utils.GenToken(adminUserID, "admin", model.UserRoleAdmin)
	if err != nil {
		t.Fatalf("生成管理员 Token 失败: %v", err)
	}
	if err := repository.SaveUserToken(adminToken, adminUserID); err != nil {
		t.Fatalf("保存管理员 Token 失败: %v", err)
	}

	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 2, Pid: 1, Name: "动作片", Show: true, Sort: 1})
	gdb.Create(&model.FilmSource{
		Id:    "src_default_main",
		Name:  "默认主站源",
		Uri:   "http://default.source.com/api.json",
		State: true,
	})

	r := router.SetupRouter()
	return r, adminToken, adminUserID
}

func doJSONRequest(r *gin.Engine, method, path, token string, body any) *httptest.ResponseRecorder {
	var bodyReader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(raw)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.AddCookie(&http.Cookie{
			Name:  config.AuthCookieName,
			Value: token,
		})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func parseResp(t *testing.T, w *httptest.ResponseRecorder) dto.Response {
	t.Helper()
	var resp dto.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body=%s", err, w.Body.String())
	}
	return resp
}

func assertSuccess(t *testing.T, w *httptest.ResponseRecorder, action string) dto.Response {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("%s HTTP状态码异常: %d, body=%s", action, w.Code, w.Body.String())
	}
	resp := parseResp(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("%s 业务Code异常: %d, msg=%s", action, resp.Code, resp.Msg)
	}
	return resp
}

// TestBatchEndpoints_AllModulesIntegration 批量测试所有重构接口端到端链路
func TestBatchEndpoints_AllModulesIntegration(t *testing.T) {
	r, token, adminUID := setupIntegrationContext(t)

	// 1. 系统配置接口（统一无 /update 路由）
	t.Run("ConfigEndpoints", func(t *testing.T) {
		assertSuccess(t, doJSONRequest(r, http.MethodGet, "/api/manage/config/basic", token, nil), "GET basic")
		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/config/basic", token, map[string]any{
			"siteName": "自动化集成测试站点",
			"siteUrl":  "http://example.com",
		}), "POST basic")

		assertSuccess(t, doJSONRequest(r, http.MethodGet, "/api/manage/config/tip", token, nil), "GET tip")
		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/config/tip", token, map[string]any{
			"enabled": true,
			"title":   "测试弹窗",
			"message": "这是一条测试弹窗消息",
		}), "POST tip")

		assertSuccess(t, doJSONRequest(r, http.MethodGet, "/api/manage/config/notice", token, nil), "GET notice")
		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/config/notice", token, map[string]any{
			"enabled":   true,
			"title":     "测试公告",
			"content":   "系统维护通知",
			"showInWeb": true,
		}), "POST notice")

		assertSuccess(t, doJSONRequest(r, http.MethodGet, "/api/manage/tmdb/config", token, nil), "GET tmdb")
		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/tmdb/config", token, map[string]any{
			"enabled":     true,
			"apiKey":      "test_tmdb_key",
			"language":    "zh-CN",
			"imageDomain": "https://image.tmdb.org",
		}), "POST tmdb")

		assertSuccess(t, doJSONRequest(r, http.MethodGet, "/api/manage/proxy/config", token, nil), "GET proxy")
		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/proxy/config", token, map[string]any{
			"enabled":  false,
			"proxyUrl": "http://127.0.0.1:7890",
		}), "POST proxy")

		assertSuccess(t, doJSONRequest(r, http.MethodGet, "/api/manage/notify/config", token, nil), "GET notify")
		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/notify/config", token, map[string]any{
			"enabled":  false,
			"botToken": "123456:ABC-DEF",
		}), "POST notify")
	})

	// 2. 轮播图模块（/save 新增/编辑 + /delete 批量删除）
	t.Run("BannerEndpoints", func(t *testing.T) {
		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/banner/save", token, map[string]any{
			"title":  "集成测试轮播",
			"mid":    9001,
			"poster": "http://img.com/p.jpg",
		}), "POST banner add")

		w := doJSONRequest(r, http.MethodGet, "/api/manage/banner/list", token, nil)
		resp := assertSuccess(t, w, "GET banner list")
		rawList, _ := json.Marshal(resp.Data)
		var list []model.Banner
		_ = json.Unmarshal(rawList, &list)
		if len(list) == 0 {
			t.Fatalf("未能检索到刚创建的轮播图")
		}
		bannerID := list[0].Id

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/banner/save", token, map[string]any{
			"id":     bannerID,
			"title":  "集成测试轮播(改)",
			"mid":    9001,
			"poster": "http://img.com/p2.jpg",
		}), "POST banner update")

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/banner/delete", token, map[string]any{
			"ids": []string{bannerID},
		}), "POST banner delete batch")
	})

	// 3. 映射规则模块（/rule/save + /rule/delete 批量删除）
	t.Run("MappingEndpoints", func(t *testing.T) {
		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/mapping/rule/save", token, map[string]any{
			"group":     "Area",
			"matchType": "exact",
			"raw":       "内地",
			"target":    "中国大陆",
		}), "POST mapping rule save add")

		w := doJSONRequest(r, http.MethodGet, "/api/manage/mapping/rule/list", token, nil)
		resp := assertSuccess(t, w, "GET mapping rule list")
		rawList, _ := json.Marshal(resp.Data)
		var result struct {
			List []model.MappingRule `json:"list"`
		}
		_ = json.Unmarshal(rawList, &result)
		if len(result.List) == 0 {
			t.Fatalf("未能检索到刚创建的映射规则")
		}
		ruleID := result.List[0].ID

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/mapping/rule/save", token, map[string]any{
			"id":        ruleID,
			"group":     "Area",
			"matchType": "exact",
			"raw":       "内地",
			"target":    "大陆",
		}), "POST mapping rule save update")

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/mapping/rule/delete", token, map[string]any{
			"ids": []uint{ruleID},
		}), "POST mapping rule delete batch")
	})

	// 4. 用户模块（/user/save + /user/delete 批量删除）
	t.Run("UserEndpoints", func(t *testing.T) {
		userName := fmt.Sprintf("it_user_%d", adminUID)
		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/user/save", token, map[string]any{
			"userName": userName,
			"password": "Password123",
			"nickName": "集成用户",
			"role":     model.UserRoleNormal,
		}), "POST user save add")

		w := doJSONRequest(r, http.MethodGet, "/api/manage/user/list?page=1&pageSize=10", token, nil)
		resp := assertSuccess(t, w, "GET user list")
		rawUsers, _ := json.Marshal(resp.Data)
		var userListResp struct {
			List []model.User `json:"list"`
		}
		_ = json.Unmarshal(rawUsers, &userListResp)
		var createdUID uint
		for _, u := range userListResp.List {
			if u.UserName == userName {
				createdUID = u.ID
				break
			}
		}
		if createdUID == 0 {
			t.Fatalf("未能检索到新创建的用户")
		}

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/user/save", token, map[string]any{
			"id":       createdUID,
			"userName": userName,
			"nickName": "集成用户(改)",
			"role":     model.UserRoleNormal,
		}), "POST user save update")

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/user/delete", token, map[string]any{
			"ids": []uint{createdUID},
		}), "POST user delete batch")
	})

	// 5. 采集中心模块（/collect/save 统一保存 + /collect/state + /collect/delete 批量操作）
	t.Run("CollectEndpoints", func(t *testing.T) {
		srcID := "src_it_001"
		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/collect/save", token, map[string]any{
			"id":      srcID,
			"name":    "测试采集站点",
			"uri":     "http://test.api.com/inc/api.json",
			"bindPid": 1,
			"state":   true,
			"opt":     1,
		}), "POST collect save add")

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/collect/save", token, map[string]any{
			"id":      srcID,
			"name":    "测试采集站(改)",
			"uri":     "http://test.api.com/inc/api.json",
			"bindPid": 1,
			"state":   true,
			"opt":     1,
		}), "POST collect save update")

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/collect/state", token, map[string]any{
			"ids":   []string{srcID},
			"state": false,
		}), "POST collect state batch")

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/collect/delete", token, map[string]any{
			"ids": []string{srcID},
		}), "POST collect delete batch")

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/collect/record/retry", token, map[string]any{
			"ids": []uint{},
			"all": true,
		}), "POST collect record retry")

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/collect/record/clear", token, map[string]any{
			"scope": "retried",
		}), "POST collect record clear")
	})

	// 6. 定时任务模块（/cron/save 保存 + /cron/state 批量启停）
	t.Run("CronEndpoints", func(t *testing.T) {
		w := doJSONRequest(r, http.MethodGet, "/api/manage/cron/list", token, nil)
		resp := assertSuccess(t, w, "GET cron list")
		rawTasks, _ := json.Marshal(resp.Data)
		var tasks []model.FilmCollectTask
		_ = json.Unmarshal(rawTasks, &tasks)
		if len(tasks) > 0 {
			taskID := tasks[0].Id
			assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/cron/save", token, map[string]any{
				"id":     taskID,
				"spec":   "0 0 * * * *",
				"remark": "每小时整点自动采集",
			}), "POST cron save")

			assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/cron/state", token, map[string]any{
				"ids":   []string{taskID},
				"state": true,
			}), "POST cron state batch")
		}
	})

	// 7. 文件管理模块（/file/rename + /file/delete 批量删除）
	t.Run("FileEndpoints", func(t *testing.T) {
		fileRec := model.FileInfo{
			Fid:      "it_file_test",
			FileType: "png",
			Name:     "集成测试图片",
			Link:     "/api/upload/pic/poster/it_file_test.png",
			Uid:      int(adminUID),
		}
		if err := db.Mdb.Create(&fileRec).Error; err != nil || fileRec.ID == 0 {
			t.Fatalf("初始化测试文件记录失败: %v", err)
		}

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/file/rename", token, map[string]any{
			"id":   fileRec.ID,
			"name": "集成测试图片(改)",
		}), "POST file rename")

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/file/delete", token, map[string]any{
			"ids": []uint{fileRec.ID},
		}), "POST file delete batch")
	})

	// 8. 影视管理模块（/film/save 统一保存 + /film/delete 批量删除）
	t.Run("FilmEndpoints", func(t *testing.T) {
		w := doJSONRequest(r, http.MethodPost, "/api/manage/film/save", token, map[string]any{})
		if w.Code != http.StatusOK || parseResp(t, w).Code == dto.SUCCESS {
			t.Fatalf("POST /api/manage/film/save 空参数应校验失败: %s", w.Body.String())
		}

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/film/delete", token, map[string]any{
			"ids": []uint{888888},
		}), "POST film delete batch")

		assertSuccess(t, doJSONRequest(r, http.MethodPost, "/api/manage/film/refresh", token, map[string]any{
			"ids": []int64{888888},
		}), "POST film refresh batch")
	})

	// 9. 访问监控与前台基础端点
	t.Run("AccessAndFrontEndpoints", func(t *testing.T) {
		assertSuccess(t, doJSONRequest(r, http.MethodGet, "/api/config/basic", "", nil), "GET front basic")
		assertSuccess(t, doJSONRequest(r, http.MethodGet, "/api/manage/index", token, nil), "GET manage index")
		assertSuccess(t, doJSONRequest(r, http.MethodGet, "/api/manage/version", token, nil), "GET manage version")
		assertSuccess(t, doJSONRequest(r, http.MethodGet, "/api/manage/access/status", token, nil), "GET access status")
	})
}
