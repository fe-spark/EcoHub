package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"server/internal/infra/db"
	"server/internal/model"
	filmrepo "server/internal/repository/film"
)

func TestNormalizeMediaURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		baseURL string
		want    string
	}{
		{
			name:    "empty raw",
			raw:     "",
			baseURL: "https://example.com",
			want:    "",
		},
		{
			name:    "empty baseURL",
			raw:     "/path/to/pic.jpg",
			baseURL: "",
			want:    "/path/to/pic.jpg",
		},
		{
			name:    "absolute http url",
			raw:     "http://img.test.com/pic.jpg",
			baseURL: "https://example.com",
			want:    "http://img.test.com/pic.jpg",
		},
		{
			name:    "absolute https url",
			raw:     "https://img.test.com/pic.jpg",
			baseURL: "https://example.com",
			want:    "https://img.test.com/pic.jpg",
		},
		{
			name:    "protocol-relative url with https base",
			raw:     "//img.doubanio.com/view/photo/p123.jpg",
			baseURL: "https://example.com",
			want:    "https://img.doubanio.com/view/photo/p123.jpg",
		},
		{
			name:    "protocol-relative url with http base",
			raw:     "//img.doubanio.com/view/photo/p123.jpg",
			baseURL: "http://example.com",
			want:    "http://img.doubanio.com/view/photo/p123.jpg",
		},
		{
			name:    "root-relative url",
			raw:     "/api/upload/pic/poster/abc.jpg",
			baseURL: "https://example.com",
			want:    "https://example.com/api/upload/pic/poster/abc.jpg",
		},
		{
			name:    "relative url without leading slash",
			raw:     "api/upload/pic/poster/abc.jpg",
			baseURL: "https://example.com",
			want:    "https://example.com/api/upload/pic/poster/abc.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeMediaURL(tt.raw, tt.baseURL)
			if got != tt.want {
				t.Errorf("normalizeMediaURL(%q, %q) = %q, want %q", tt.raw, tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestHandleProvideConfig_RedisNilSafety(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/provide/config", nil)
	c.Request.Host = "127.0.0.1:8080"

	ProvideHd.HandleProvideConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := res["sites"]; !ok {
		t.Fatalf("expected sites in response")
	}
}

func setupProvideTestDB(t *testing.T) (*gorm.DB, *miniredis.Miniredis) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared&_busy_timeout=10000", t.Name(), time.Now().UnixNano())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("migrate models: %v", err)
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("run miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	origMdb := db.Mdb
	origRdb := db.Rdb
	db.Mdb = gdb
	db.Rdb = client
	filmrepo.ClearActiveFilmReadModel()

	t.Cleanup(func() {
		filmrepo.WaitActiveFilmSearchIndexBuilt()
		_ = client.Close()
		mr.Close()
		db.Mdb = origMdb
		db.Rdb = origRdb
		filmrepo.ClearActiveFilmReadModel()
	})

	return gdb, mr
}

func TestHandleProvide_FullPipeline(t *testing.T) {
	gdb, mr := setupProvideTestDB(t)
	const version = "v_tvbox_pipeline"

	// 1. 初始化分类数据
	catRoot := model.Category{Id: 1, Pid: 0, Name: "电影", Show: true, Sort: 1, StableKey: "movie"}
	catSub := model.Category{Id: 10, Pid: 1, Name: "科幻片", Show: true, Sort: 1, StableKey: "scifi"}
	if err := gdb.Create(&catRoot).Error; err != nil {
		t.Fatalf("create catRoot: %v", err)
	}
	if err := gdb.Create(&catSub).Error; err != nil {
		t.Fatalf("create catSub: %v", err)
	}

	// 2. 初始化影片快照与详情
	// 影片 201: 两集，相对路径海报
	snap201 := model.FilmListSnapshot{
		SnapshotVersion: version,
		Mid:             201,
		Name:            "流浪地球",
		Pid:             1,
		Cid:             10,
		Hits:            500,
		Year:            2021,
		Area:            "中国大陆",
		Language:        "汉语普通话",
		Remarks:         "HD中字",
		Picture:         "/upload/pic/201.jpg",
		UpdateStamp:     time.Now().Unix(),
	}
	if err := gdb.Create(&snap201).Error; err != nil {
		t.Fatalf("create snap201: %v", err)
	}
	detail201 := model.MovieDetail{
		Id:       201,
		Name:     "流浪地球",
		Picture:  "/upload/pic/201.jpg",
		PlayFrom: []string{"默认主源"},
		PlayList: [][]model.MovieUrlInfo{
			{
				{Episode: "01", Link: "http://video/201_01.m3u8"},
				{Episode: "02", Link: "http://video/201_02.m3u8"},
			},
		},
		MovieDescriptor: model.MovieDescriptor{
			Year:     "2021",
			Area:     "中国大陆",
			Language: "汉语普通话",
			Remarks:  "HD中字",
		},
	}
	raw201, _ := json.Marshal(detail201)
	if err := gdb.Create(&model.MovieDetailInfo{Mid: 201, Content: string(raw201)}).Error; err != nil {
		t.Fatalf("create detail201: %v", err)
	}

	// 影片 202: 绝对路径海报
	snap202 := model.FilmListSnapshot{
		SnapshotVersion: version,
		Mid:             202,
		Name:            "星际穿越",
		Pid:             1,
		Cid:             10,
		Hits:            800,
		Year:            2014,
		Area:            "美国",
		Language:        "英语",
		Remarks:         "超清",
		Picture:         "https://img.test.com/202.jpg",
		UpdateStamp:     time.Now().Unix(),
	}
	if err := gdb.Create(&snap202).Error; err != nil {
		t.Fatalf("create snap202: %v", err)
	}
	detail202 := model.MovieDetail{
		Id:       202,
		Name:     "星际穿越",
		Picture:  "https://img.test.com/202.jpg",
		PlayFrom: []string{"默认主源"},
		PlayList: [][]model.MovieUrlInfo{
			{
				{Episode: "HD", Link: "http://video/202_hd.m3u8"},
			},
		},
		MovieDescriptor: model.MovieDescriptor{
			Year:     "2014",
			Area:     "美国",
			Language: "英语",
			Remarks:  "超清",
		},
	}
	raw202, _ := json.Marshal(detail202)
	if err := gdb.Create(&model.MovieDetailInfo{Mid: 202, Content: string(raw202)}).Error; err != nil {
		t.Fatalf("create detail202: %v", err)
	}

	_ = filmrepo.SetActiveSnapshotVersion(version)
	_ = filmrepo.LoadActiveFilmReadModel(version)
	filmrepo.WaitActiveFilmSearchIndexBuilt()

	gin.SetMode(gin.TestMode)

	// A. 验证 GET /api/provide/config
	t.Run("ProvideConfig", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/api/provide/config", nil)
		c.Request.Host = "127.0.0.1:8080"

		ProvideHd.HandleProvideConfig(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var res map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		sites, ok := res["sites"].([]any)
		if !ok || len(sites) == 0 {
			t.Fatalf("expected non-empty sites")
		}
		firstSite := sites[0].(map[string]any)
		if firstSite["key"] != "EcoHub" {
			t.Fatalf("expected EcoHub site key, got %v", firstSite["key"])
		}
	})

	// B. 验证 GET /api/provide/vod?ac=list
	t.Run("ProvideVodList", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/api/provide/vod?ac=list&t=1", nil)
		c.Request.Host = "127.0.0.1:8080"

		ProvideHd.HandleProvide(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var res struct {
			Code    int               `json:"code"`
			Msg     string            `json:"msg"`
			Page    int               `json:"page"`
			Total   int               `json:"total"`
			List    []model.FilmList  `json:"list"`
			Class   []model.FilmClass `json:"class"`
			Filters map[string]any    `json:"filters"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if res.Code != 1 {
			t.Fatalf("expected code=1, got %d", res.Code)
		}
		if len(res.List) != 2 {
			t.Fatalf("expected 2 items in list, got %d", len(res.List))
		}
		// 校验海报 URL 是否补全了 baseURL
		foundNormalizedPic := false
		for _, item := range res.List {
			if item.VodID == 201 && strings.HasPrefix(item.VodPic, "http://127.0.0.1:8080/upload/pic/201.jpg") {
				foundNormalizedPic = true
			}
		}
		if !foundNormalizedPic {
			t.Fatalf("expected vod_pic to be normalized with base URL, got: %+v", res.List)
		}
	})

	// C. 验证 GET /api/provide/vod?ac=detail&ids=201,202 (带 ids 详情批量拉取)
	t.Run("ProvideVodDetailWithIds", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/api/provide/vod?ac=detail&ids=201,202", nil)
		c.Request.Host = "127.0.0.1:8080"

		ProvideHd.HandleProvide(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var res struct {
			Code  int                `json:"code"`
			Msg   string             `json:"msg"`
			Total int                `json:"total"`
			List  []model.FilmDetail `json:"list"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if res.Code != 1 || len(res.List) != 2 {
			t.Fatalf("expected code=1 and 2 details, got code=%d, count=%d", res.Code, len(res.List))
		}

		// 验证 TVBox 标准格式：
		// 1. VodPlayFrom 以 $$$ 分隔
		// 2. VodPlayURL 格式为 "集数$链接#集数$链接$$$..."
		var detail201Res *model.FilmDetail
		for i := range res.List {
			if res.List[i].VodID == 201 {
				detail201Res = &res.List[i]
				break
			}
		}
		if detail201Res == nil {
			t.Fatalf("detail 201 not found")
		}
		if detail201Res.VodPlayFrom != "默认主源" {
			t.Fatalf("expected VodPlayFrom '默认主源', got %q", detail201Res.VodPlayFrom)
		}
		expectedPlayURL := "01$http://video/201_01.m3u8#02$http://video/201_02.m3u8"
		if detail201Res.VodPlayURL != expectedPlayURL {
			t.Fatalf("expected VodPlayURL %q, got %q", expectedPlayURL, detail201Res.VodPlayURL)
		}
		if !strings.HasPrefix(detail201Res.VodPic, "http://127.0.0.1:8080/upload/pic/201.jpg") {
			t.Fatalf("expected normalized VodPic, got %q", detail201Res.VodPic)
		}

		// 验证缓存命中：执行完毕后 Redis 中应有缓存
		cachedVal, err := mr.Get(fmt.Sprintf("EcoHub:filmPlayInfo:%d", 201))
		if err != nil || cachedVal == "" {
			t.Fatalf("expected Redis cache to be populated for film 201")
		}
	})

	// D. 验证 GET /api/provide/vod?ac=detail&t=1&pg=1 (无 ids 分页详情拉取)
	t.Run("ProvideVodDetailPaginationWithoutIds", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/api/provide/vod?ac=detail&t=1&pg=1", nil)
		c.Request.Host = "127.0.0.1:8080"

		ProvideHd.HandleProvide(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var res struct {
			Code        int                `json:"code"`
			Page        int                `json:"page"`
			Total       int                `json:"total"`
			RecordCount int                `json:"recordcount"`
			List        []model.FilmDetail `json:"list"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if res.Code != 1 || len(res.List) != 2 {
			t.Fatalf("expected code=1 and 2 items, got code=%d, len=%d", res.Code, len(res.List))
		}
	})

	// E. 验证 GET /api/provide/vod?ac=videolist&ids=202 (ac=videolist 别名兼容)
	t.Run("ProvideVodVideoListAlias", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/api/provide/vod?ac=videolist&ids=202", nil)
		c.Request.Host = "127.0.0.1:8080"

		ProvideHd.HandleProvide(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var res struct {
			Code int                `json:"code"`
			List []model.FilmDetail `json:"list"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if res.Code != 1 || len(res.List) != 1 || res.List[0].VodID != 202 {
			t.Fatalf("expected code=1 and item 202, got %+v", res)
		}
	})

	// F. 验证 GET /api/provide/vod?wd=星际 (关键字搜索)
	t.Run("ProvideVodKeywordSearch", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/api/provide/vod?wd=星际", nil)
		c.Request.Host = "127.0.0.1:8080"

		ProvideHd.HandleProvide(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var res struct {
			Code int              `json:"code"`
			List []model.FilmList `json:"list"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if res.Code != 1 || len(res.List) != 1 || res.List[0].VodID != 202 {
			t.Fatalf("expected search match 202, got %+v", res)
		}
	})

	// G. 验证异常与边缘用例：不存在的 ID
	t.Run("ProvideVodNonExistentId", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/api/provide/vod?ac=detail&ids=99999", nil)
		c.Request.Host = "127.0.0.1:8080"

		ProvideHd.HandleProvide(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var res struct {
			Code int                `json:"code"`
			List []model.FilmDetail `json:"list"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if res.Code != 1 || len(res.List) != 0 {
			t.Fatalf("expected empty list for non-existent id, got %+v", res)
		}
	})

	// H. 验证反向代理请求头 (X-Forwarded-Proto / X-Forwarded-Host) 下海报 URL 补全
	t.Run("ProvideVodProxyHeaderNormalization", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/api/provide/vod?ac=detail&ids=201", nil)
		c.Request.Host = "internal-backend:8080"
		c.Request.Header.Set("X-Forwarded-Proto", "https")
		c.Request.Header.Set("X-Forwarded-Host", "tvbox.ecohub.com")

		ProvideHd.HandleProvide(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var res struct {
			Code int                `json:"code"`
			List []model.FilmDetail `json:"list"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if res.Code != 1 || len(res.List) != 1 {
			t.Fatalf("expected 1 detail, got %+v", res)
		}
		expectedPicPrefix := "https://tvbox.ecohub.com/upload/pic/201.jpg"
		if res.List[0].VodPic != expectedPicPrefix {
			t.Fatalf("expected proxy normalized pic %q, got %q", expectedPicPrefix, res.List[0].VodPic)
		}
	})
}
