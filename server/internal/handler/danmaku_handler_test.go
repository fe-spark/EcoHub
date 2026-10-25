package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/model"
	"server/internal/repository"

	"github.com/gin-gonic/gin"
)

func TestDanmakuHandler_GetAndPost(t *testing.T) {
	gin.SetMode(gin.TestMode)

	_ = repository.SaveSiteBasic(model.BasicConfig{
		Danmaku: model.DanmakuConfig{
			Enabled:   true,
			AutoSniff: false, // 禁用外网嗅探，避免单测走外网
		},
	})

	r := gin.New()
	r.GET("/api/film/danmaku", DanmakuHd.GetDanmaku)
	r.POST("/api/film/danmaku", DanmakuHd.PostDanmaku)

	// 1. 发送空内容应当报错
	wPost := httptest.NewRecorder()
	reqPost, _ := http.NewRequest(http.MethodPost, "/api/film/danmaku", bytes.NewBufferString(`{"mid":1,"text":""}`))
	reqPost.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wPost, reqPost)

	var errResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(wPost.Body.Bytes(), &errResp)
	if errResp.Code == 0 {
		t.Fatalf("expected error for empty danmaku text, got code=0")
	}

	// 2. 获取空弹幕列表
	wGet := httptest.NewRecorder()
	reqGet, _ := http.NewRequest(http.MethodGet, "/api/film/danmaku?mid=1&episode=1", nil)
	r.ServeHTTP(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", wGet.Code)
	}

	var getResp struct {
		Code int                 `json:"code"`
		Data []model.DanmakuItem `json:"data"`
	}
	if err := json.Unmarshal(wGet.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if getResp.Code != 0 {
		t.Fatalf("code = %d, want 0", getResp.Code)
	}

	// 3. 测试集数智能识别解析
	if ep := parseEpisodeNumber("【qq】 庆余年第二季_05"); ep != 5 {
		t.Fatalf("parseEpisodeNumber want 5, got %d", ep)
	}
	if ep := parseEpisodeNumber("HD1080P第12话4K"); ep != 12 {
		t.Fatalf("parseEpisodeNumber want 12, got %d", ep)
	}
}
