package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDBForMediaHandler(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}
	if err := testDB.AutoMigrate(&model.FilmSource{}); err != nil {
		t.Fatalf("failed to auto migrate: %v", err)
	}
	db.Mdb = testDB
}

func TestMediaHandler_Stream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTestDBForMediaHandler(t)

	os.Setenv("MEDIA_STREAM_SECRET", "test_secret_for_media_handler")
	defer os.Unsetenv("MEDIA_STREAM_SECRET")

	// 启动一个 mock WebDAV 服务器
	mockWebDAV := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "1024")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodGet {
			rangeHeader := r.Header.Get("Range")
			if rangeHeader == "bytes=0-10" {
				w.Header().Set("Content-Range", "bytes 0-10/1024")
				w.Header().Set("Content-Length", "11")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write([]byte("01234567890"))
				return
			}
			w.Header().Set("Content-Length", "4")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("test"))
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer mockWebDAV.Close()

	// 写入一个测试 WebDAV 源
	source := model.FilmSource{
		Id:         "99",
		Name:       "测试 WebDAV",
		SourceType: model.SourceTypeWebdav,
		State:      true,
	}
	rawCfg, _ := json.Marshal(model.WebdavConfig{
		ServerURL: mockWebDAV.URL,
		RootPath:  "/dav",
		Username:  "user",
		Password:  "password",
	})
	source.WebdavConfig = string(rawCfg)
	if err := db.Mdb.Create(&source).Error; err != nil {
		t.Fatalf("failed to create film source: %v", err)
	}

	r := gin.New()
	r.GET("/api/media/stream", MediaHd.Stream)
	r.HEAD("/api/media/stream", MediaHd.Stream)

	relPath := "movies/avatar.mkv"
	sign := service.GenerateMediaStreamSign(99, relPath)
	pEncoded := base64.RawURLEncoding.EncodeToString([]byte(relPath))

	// 1. 测试未传参数 -> 400
	req400, _ := http.NewRequest(http.MethodGet, "/api/media/stream", nil)
	w400 := httptest.NewRecorder()
	r.ServeHTTP(w400, req400)
	if w400.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing params, got %d", w400.Code)
	}

	// 2. 测试错误签名 -> 403
	req403, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api/media/stream?sid=99&p=%s&sign=invalidsign", pEncoded), nil)
	w403 := httptest.NewRecorder()
	r.ServeHTTP(w403, req403)
	if w403.Code != http.StatusForbidden {
		t.Errorf("expected 403 for invalid sign, got %d", w403.Code)
	}

	// 3. 测试 HEAD 请求 -> 200 & Content-Type & Range 头部
	reqHead, _ := http.NewRequest(http.MethodHead, fmt.Sprintf("/api/media/stream?sid=99&p=%s&ext=mkv&sign=%s", pEncoded, sign), nil)
	wHead := httptest.NewRecorder()
	r.ServeHTTP(wHead, reqHead)
	if wHead.Code != http.StatusOK {
		t.Errorf("expected 200 for HEAD, got %d", wHead.Code)
	}
	if ct := wHead.Header().Get("Content-Type"); ct != "video/x-matroska" {
		t.Errorf("expected video/x-matroska, got %s", ct)
	}
	if ar := wHead.Header().Get("Accept-Ranges"); ar != "bytes" {
		t.Errorf("expected Accept-Ranges: bytes, got %s", ar)
	}

	// 4. 测试 Range 请求 -> 206 Partial Content
	reqRange, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api/media/stream?sid=99&p=%s&ext=mkv&sign=%s", pEncoded, sign), nil)
	reqRange.Header.Set("Range", "bytes=0-10")
	wRange := httptest.NewRecorder()
	r.ServeHTTP(wRange, reqRange)
	if wRange.Code != http.StatusPartialContent {
		t.Errorf("expected 206 for Range request, got %d", wRange.Code)
	}
	if wRange.Body.String() != "01234567890" {
		t.Errorf("expected body '01234567890', got %s", wRange.Body.String())
	}
}

func TestMediaHandler_HeadFallbackFromRangeProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTestDBForMediaHandler(t)

	os.Setenv("MEDIA_STREAM_SECRET", "test_secret_for_media_handler")
	defer os.Unsetenv("MEDIA_STREAM_SECRET")

	mockWebDAV := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodGet && r.Header.Get("Range") == "bytes=0-0" {
			w.Header().Set("Content-Range", "bytes 0-0/4096")
			w.Header().Set("Content-Length", "1")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("x"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer mockWebDAV.Close()

	source := model.FilmSource{
		Id:         "88",
		Name:       "HEAD 回退 WebDAV",
		Uri:        "webdav|head-fallback|/dav",
		SourceType: model.SourceTypeWebdav,
		State:      true,
	}
	rawCfg, _ := json.Marshal(model.WebdavConfig{
		ServerURL: mockWebDAV.URL,
		RootPath:  "/dav",
		Username:  "user",
		Password:  "password",
	})
	source.WebdavConfig = string(rawCfg)
	if err := db.Mdb.Create(&source).Error; err != nil {
		t.Fatalf("failed to create film source: %v", err)
	}

	r := gin.New()
	r.HEAD("/api/media/stream", MediaHd.Stream)

	relPath := "movies/avatar.mkv"
	sign := service.GenerateMediaStreamSign(88, relPath)
	pEncoded := base64.RawURLEncoding.EncodeToString([]byte(relPath))
	reqHead, _ := http.NewRequest(http.MethodHead, fmt.Sprintf("/api/media/stream?sid=88&p=%s&ext=mkv&sign=%s", pEncoded, sign), nil)
	wHead := httptest.NewRecorder()
	r.ServeHTTP(wHead, reqHead)
	if wHead.Code != http.StatusOK {
		t.Fatalf("expected 200 for HEAD fallback, got %d", wHead.Code)
	}
	if cl := wHead.Header().Get("Content-Length"); cl != "4096" {
		t.Fatalf("expected Content-Length 4096 from Content-Range total, got %s", cl)
	}
	if cr := wHead.Header().Get("Content-Range"); cr != "" {
		t.Fatalf("HEAD without Range should not expose probe Content-Range, got %s", cr)
	}
}

func TestMediaHandler_HeadForbiddenFallsBackToRangeProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTestDBForMediaHandler(t)

	os.Setenv("MEDIA_STREAM_SECRET", "test_secret_for_media_handler")
	defer os.Unsetenv("MEDIA_STREAM_SECRET")

	mockWebDAV := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Method == http.MethodGet && r.Header.Get("Range") == "bytes=0-0" {
			w.Header().Set("Content-Range", "bytes 0-0/2048")
			w.Header().Set("Content-Length", "1")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("x"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer mockWebDAV.Close()

	source := model.FilmSource{
		Id:         "87",
		Name:       "HEAD 403 回退",
		Uri:        "webdav|head-403|/dav",
		SourceType: model.SourceTypeWebdav,
		State:      true,
	}
	rawCfg, _ := json.Marshal(model.WebdavConfig{
		ServerURL: mockWebDAV.URL,
		RootPath:  "/dav",
		Username:  "user",
		Password:  "password",
	})
	source.WebdavConfig = string(rawCfg)
	if err := db.Mdb.Create(&source).Error; err != nil {
		t.Fatalf("failed to create film source: %v", err)
	}

	r := gin.New()
	r.HEAD("/api/media/stream", MediaHd.Stream)
	relPath := "movies/fate.mkv"
	sign := service.GenerateMediaStreamSign(87, relPath)
	pEncoded := base64.RawURLEncoding.EncodeToString([]byte(relPath))
	reqHead, _ := http.NewRequest(http.MethodHead, fmt.Sprintf("/api/media/stream?sid=87&p=%s&ext=mkv&sign=%s", pEncoded, sign), nil)
	wHead := httptest.NewRecorder()
	r.ServeHTTP(wHead, reqHead)
	if wHead.Code != http.StatusOK {
		t.Fatalf("HEAD 403 from upstream should fall back to Range probe, got %d", wHead.Code)
	}
	if cl := wHead.Header().Get("Content-Length"); cl != "2048" {
		t.Fatalf("expected Content-Length 2048, got %s", cl)
	}
}

func TestMediaHandler_RedirectsAliyunDrive307(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTestDBForMediaHandler(t)
	os.Setenv("MEDIA_STREAM_SECRET", "test_secret_for_media_handler")
	defer os.Unsetenv("MEDIA_STREAM_SECRET")

	ossURL := "https://dl1-v6.aliyundrive.cloud/file?sign=abc"
	mockWebDAV := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", ossURL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer mockWebDAV.Close()

	source := model.FilmSource{
		Id:         "86",
		Name:       "Alist 307",
		Uri:        "webdav|alist-307|/dav",
		SourceType: model.SourceTypeWebdav,
		State:      true,
	}
	rawCfg, _ := json.Marshal(model.WebdavConfig{
		ServerURL: mockWebDAV.URL,
		RootPath:  "/dav",
		Username:  "user",
		Password:  "password",
	})
	source.WebdavConfig = string(rawCfg)
	if err := db.Mdb.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}

	r := gin.New()
	r.GET("/api/media/stream", MediaHd.Stream)
	relPath := "movie.mkv"
	sign := service.GenerateMediaStreamSign(86, relPath)
	pEncoded := base64.RawURLEncoding.EncodeToString([]byte(relPath))
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api/media/stream?sid=86&p=%s&ext=mkv&sign=%s", pEncoded, sign), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307, got %d body=%s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != ossURL {
		t.Fatalf("expected Location %s, got %s", ossURL, loc)
	}
}

func TestResolveStreamBase_RewritesNextPort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	os.Unsetenv("MEDIA_STREAM_PUBLIC_BASE")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/filmPlayInfo", nil)
	c.Request.Host = "192.168.1.10:3000"
	c.Request.Header.Set("X-Forwarded-Host", "192.168.1.10:3000")

	got := ResolveStreamBase(c, false)
	if got != "http://192.168.1.10:18080" {
		t.Fatalf("expected :3000 rewritten to :18080, got %q", got)
	}

	c.Request.Host = "server:8080"
	c.Request.Header.Del("X-Forwarded-Host")
	got = ResolveStreamBase(c, true)
	if got != "" {
		t.Fatalf("docker internal host should be discarded, got %q", got)
	}
}
