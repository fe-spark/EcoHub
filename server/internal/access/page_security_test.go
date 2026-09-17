package access

import (
	"fmt"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"server/internal/infra/db"
	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"
)

func TestIsSafePagePath(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"/play?id=1024", true},
		{"/search?keyword=x", true},
		{"/a/b/c", true},
		{"HomePage", true},
		{"detail/1024", true},
		{"browse", true},
		{"  /safe/path  ", true},
		// scheme / 协议相对注入
		{"javascript:alert(1)", false},
		{"javascript://x", false},
		{"https://evil.com/phish", false},
		{"http://evil.com", false},
		{"//evil.com/x", false},
		{"data:text/html,<script>alert(1)</script>", false},
		{"vbscript:msgbox(1)", false},
		{"mailto:admin@evil.com", false},
	}
	for _, c := range cases {
		if got := isSafePagePath(c.in); got != c.want {
			t.Errorf("isSafePagePath(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBuildPageEventPayload_RejectsSchemePaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetPageHit()
	ctx := testPageCtx("Mozilla/5.0", "192.168.1.100")

	for _, p := range []string{"javascript:alert(1)", "//evil.com/x", "https://evil.com/phish"} {
		evt := buildPageEventPayload(ctx, TrackViewPayload{
			Action: "browse",
			Source: "web",
			Path:   p,
		})
		if evt != nil {
			t.Fatalf("expected scheme page %q to be dropped, got %+v", p, evt)
		}
	}

	// 合法站内路径与 App 屏名不受影响
	for _, ok := range []struct{ page, path string }{
		{page: "", path: "/play?id=1024"},
		{page: "HomePage", path: ""},
		{page: "SearchScreen", path: ""},
	} {
		evt := buildPageEventPayload(ctx, TrackViewPayload{
			Action: "browse",
			Source: "web",
			Page:   ok.page,
			Path:   ok.path,
		})
		if evt == nil {
			t.Fatalf("expected safe payload page=%q path=%q accepted", ok.page, ok.path)
		}
	}
}

func TestPageClientFromUA(t *testing.T) {
	if got := pageClientFromUA("Mozilla/5.0 (Linux; Android 14; Pixel) Chrome/120"); got != "web" {
		t.Fatalf("mobile browser must stay web, got %q", got)
	}
	if got := pageClientFromUA("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Safari/604.1"); got != "web" {
		t.Fatalf("iPhone browser must stay web, got %q", got)
	}
	if got := pageClientFromUA("Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) Safari/604.1"); got != "web" {
		t.Fatalf("iPad browser must stay web, got %q", got)
	}
	if got := pageClientFromUA("EcoHub-Android/1.2.0"); got != "android" {
		t.Fatalf("EcoHub-Android got %q", got)
	}
	if got := pageClientFromUA("EcoHub-iOS/2.0.0"); got != "ios" {
		t.Fatalf("EcoHub-iOS got %q", got)
	}
	if got := pageClientFromUA("EcoHub-IOS/2.0.0"); got != "ios" {
		t.Fatalf("EcoHub-IOS got %q", got)
	}
	if got := pageClientFromUA("EcoHub-OHOS/1.0.5"); got != "harmony" {
		t.Fatalf("EcoHub-OHOS got %q", got)
	}
}

func TestBuildPageEventPayload_MobileBrowserWithoutSourceIsWeb(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetPageHit()
	ctx := testPageCtx("Mozilla/5.0 (Linux; Android 14) Chrome/120", "192.168.1.100")
	evt := buildPageEventPayload(ctx, TrackViewPayload{Action: "browse", Path: "/play?id=1"})
	if evt == nil || evt.ClientType != "web" {
		t.Fatalf("mobile web without source must be web, got %+v", evt)
	}

	resetPageHit()
	appCtx := testPageCtx("EcoHub-Android/1.2.0", "192.168.1.101")
	appEvt := buildPageEventPayload(appCtx, TrackViewPayload{Action: "browse", Page: "HomePage"})
	if appEvt == nil || appEvt.ClientType != "android" {
		t.Fatalf("EcoHub-Android without source must be android, got %+v", appEvt)
	}
}

func TestSanitizePosterURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://cdn.example.com/a.jpg", "https://cdn.example.com/a.jpg"},
		{"http://cdn.example.com/a.jpg", "http://cdn.example.com/a.jpg"},
		{"javascript:alert(1)", ""},
		{"data:image/png;base64,xxx", ""},
		{"//evil.com/x.jpg", ""},
		{"https://cdn.example.com/a.jpg\nhttps://evil.com/x", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := sanitizePosterURL(c.in); got != c.want {
			t.Errorf("sanitizePosterURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildPageEventPayload_SanitizesPosterAndSkipsLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetPageHit()
	ctx := testPageCtx("Mozilla/5.0", "192.168.1.120")
	evt := buildPageEventPayload(ctx, TrackViewPayload{
		Action:         "play",
		Resource:       "1024",
		ResourceTitle:  "客户端伪造片名",
		ResourcePoster: "javascript:alert(1)",
		ResourceCat:    "科幻",
		Source:         "web",
		Path:           "/play?id=1024",
	})
	if evt == nil {
		t.Fatal("expected event")
	}
	if evt.ResourcePoster != "" {
		t.Fatalf("javascript poster must be dropped, got %q", evt.ResourcePoster)
	}
	if evt.ResourceTitle != "客户端伪造片名" {
		t.Fatalf("request path should keep client title as hint, got %q", evt.ResourceTitle)
	}
}

func TestSnapshotAccessEvent_DropsUnsafePosterOnCatalogMiss(t *testing.T) {
	evt := &AccessEvent{
		Action:         ActionPlay,
		Resource:       "1024",
		ResourceTitle:  "客户端伪造片名",
		ResourcePoster: "javascript:alert(1)",
		ResourceCat:    "伪造分类",
	}
	snapshotAccessEvent(evt)
	if evt.ResourcePoster != "" {
		t.Fatalf("unsafe poster must be dropped, got %q", evt.ResourcePoster)
	}
	if evt.ResourceTitle != "客户端伪造片名" {
		t.Fatalf("catalog miss should keep client title hint, got %q", evt.ResourceTitle)
	}
}

func TestSnapshotAccessEvent_NoSQL_FastShortCircuit(t *testing.T) {
	// 1. 验证存在已有片名时，立即短路返回
	evtWithTitle := &AccessEvent{
		Action:        ActionPlay,
		Resource:      "8888",
		ResourceTitle: "已知片名-流浪地球",
	}
	snapshotAccessEvent(evtWithTitle)
	if evtWithTitle.ResourceTitle != "已知片名-流浪地球" {
		t.Fatalf("expected title preserved, got %q", evtWithTitle.ResourceTitle)
	}

	// 2. 验证内存缓存中存在时能正确填充，但绝对不发起任何 SQL
	filmMetaCacheMu.Lock()
	filmMetaCache[9999] = filmMetaCacheItem{
		Title:    "内存片名-黑客帝国",
		Category: "科幻",
		Poster:   "https://cdn.example.com/matrix.jpg",
		CachedAt: time.Now(),
	}
	filmMetaCacheMu.Unlock()

	evtFromMemory := &AccessEvent{
		Action:   ActionPlay,
		Resource: "9999",
	}
	snapshotAccessEvent(evtFromMemory)
	if evtFromMemory.ResourceTitle != "内存片名-黑客帝国" {
		t.Fatalf("expected title from memory '内存片名-黑客帝国', got %q", evtFromMemory.ResourceTitle)
	}
	if evtFromMemory.ResourcePoster != "https://cdn.example.com/matrix.jpg" {
		t.Fatalf("expected poster from memory, got %q", evtFromMemory.ResourcePoster)
	}
	if evtFromMemory.ResourceCat != "科幻" {
		t.Fatalf("expected cat from memory, got %q", evtFromMemory.ResourceCat)
	}

	// 3. 验证内存未命中时，绝不穿透阻塞，安全保留未设置状态留待查询端懒补齐
	evtCold := &AccessEvent{
		Action:   ActionPlay,
		Resource: "7777777",
	}
	snapshotAccessEvent(evtCold)
	if evtCold.ResourceTitle != "" {
		t.Fatalf("expected cold title to remain empty on write path, got %q", evtCold.ResourceTitle)
	}
}

func TestSnapshotAccessEvent_NoSQL_EvenWhenDBConnected(t *testing.T) {
	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmListSnapshot{}, &model.MovieDetailInfo{}, &model.Category{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	const testVersion = "v_nosql_test"
	_ = filmsnapshot.SetActiveSnapshotVersion(testVersion)

	// 库中存在影片 8888888 与分类 555
	_ = gdb.Create(&model.FilmListSnapshot{
		SnapshotVersion: testVersion,
		Mid:             8888888,
		Name:            "库中影片-流浪地球2",
		CName:           "科幻",
		Picture:         "https://cdn.example.com/earth2.jpg",
	}).Error
	_ = gdb.Create(&model.Category{
		Id:   555,
		Name: "库中分类-动作片",
	}).Error

	// 1. 验证写入路径（snapshotAccessEvent）面对冷缓存时，绝不查库，保留空字段防阻塞采集 worker
	evtFilm := &AccessEvent{
		Action:   ActionPlay,
		Resource: "8888888",
	}
	snapshotAccessEvent(evtFilm)
	if evtFilm.ResourceTitle != "" {
		t.Fatalf("write path must NOT query DB, expected empty title, got %q", evtFilm.ResourceTitle)
	}

	evtCat := &AccessEvent{
		Action:   ActionClassify,
		Resource: "555",
	}
	snapshotAccessEvent(evtCat)
	if evtCat.ResourceCat != "" {
		t.Fatalf("write path must NOT query DB, expected empty category, got %q", evtCat.ResourceCat)
	}

	// 2. 验证读取路径（enrichLogEvents）仍可批量懒补齐库中元数据
	enriched := enrichLogEvents([]AccessEvent{*evtFilm, *evtCat})
	if len(enriched) != 2 {
		t.Fatalf("expected 2 enriched events, got %d", len(enriched))
	}
	if enriched[0].ResourceTitle != "库中影片-流浪地球2" {
		t.Fatalf("query path should enrich title, got %q", enriched[0].ResourceTitle)
	}
	if enriched[1].ResourceCat != "库中分类-动作片" {
		t.Fatalf("query path should enrich category, got %q", enriched[1].ResourceCat)
	}
}
