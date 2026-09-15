package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/service"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFilmPlayInfo_WebDAVSignAndPriority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	os.Setenv("MEDIA_STREAM_PUBLIC_BASE", "http://192.168.1.100:18080")
	defer os.Unsetenv("MEDIA_STREAM_PUBLIC_BASE")

	// 1. 初始化 SQLite 与 Miniredis
	testDB, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	db.Mdb = testDB

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis run: %v", err)
	}
	defer mr.Close()
	db.Rdb = redis.NewClient(&redis.Options{Addr: mr.Addr()})

	// 2. 模拟 Redis 中缓存的 FilmPlayInfo 详情数据（包含原始 wdv:// 链路与 MacCMS m3u8 链路）
	relPath := "Season 1/S01E01.mkv"
	encodedRel := base64.RawURLEncoding.EncodeToString([]byte(relPath))
	wdvLink := service.WdvSchemePrefix + "55/" + encodedRel
	m3u8Link := "https://cdn.example.com/play/master.m3u8"

	detailVo := model.MovieDetailVo{
		MovieDetail: model.MovieDetail{
			Id:   888,
			Name: "流浪地球",
			MovieDescriptor: model.MovieDescriptor{
				DbId:  888,
				CName: "流浪地球",
			},
		},
		List: []model.PlayLinkVo{
			{
				Id:       "webdav_source",
				SourceId: "55",
				Name:     "WebDAV 4K 原盘",
				LinkList: []model.MovieUrlInfo{
					{Episode: "第1集", Link: wdvLink},
				},
			},
			{
				Id:       "maccms_source",
				SourceId: "2",
				Name:     "普通线路",
				LinkList: []model.MovieUrlInfo{
					{Episode: "第1集", Link: m3u8Link},
				},
			},
		},
	}
	cachedJSON, _ := json.Marshal(detailVo)
	mr.Set(config.FilmPlayInfoKey+":888", string(cachedJSON))

	// 3. 发送 FilmPlayInfo 请求（无 playFrom 偏好）
	r := gin.New()
	r.GET("/api/filmPlayInfo", IndexHd.FilmPlayInfo)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/filmPlayInfo?id=888&episode=0", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Current         model.MovieUrlInfo `json:"current"`
			CurrentPlayFrom string             `json:"currentPlayFrom"`
			Detail          model.MovieDetailVo `json:"detail"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// 4. 验证 WebDAV 线路无偏好时被优先选为起播线路
	if resp.Data.CurrentPlayFrom != "webdav_source" {
		t.Errorf("expected currentPlayFrom 'webdav_source', got %q", resp.Data.CurrentPlayFrom)
	}

	// 5. 验证 current.link 已被签名转换为网关地址，且绝对不包含 wdv://
	currentLink := resp.Data.Current.Link
	if strings.Contains(currentLink, "wdv://") {
		t.Errorf("current.link must NOT contain wdv://, got: %s", currentLink)
	}
	if !strings.HasPrefix(currentLink, "http://192.168.1.100:18080/api/media/stream?") {
		t.Errorf("expected current.link to start with MEDIA_STREAM_PUBLIC_BASE /api/media/stream, got: %s", currentLink)
	}
	if !strings.Contains(currentLink, "sid=55") || !strings.Contains(currentLink, "sign=") || !strings.Contains(currentLink, "ext=mkv") {
		t.Errorf("current.link missing query parameters: %s", currentLink)
	}

	// 6. 验证 detail.list 中 WebDAV 线路也全量签名，普通 m3u8 原样保留
	for _, group := range resp.Data.Detail.List {
		for _, ep := range group.LinkList {
			if strings.Contains(ep.Link, "wdv://") {
				t.Errorf("detail.list links must NOT contain wdv://, got: %s", ep.Link)
			}
			if group.Id == "maccms_source" && ep.Link != m3u8Link {
				t.Errorf("maccms link should remain untouched, got: %s", ep.Link)
			}
		}
	}
}
