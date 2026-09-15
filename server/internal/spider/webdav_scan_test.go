package spider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	filmrepo "server/internal/repository/film"
	"server/internal/utils"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupWebDAVScanTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	db.Mdb = gdb
	return gdb
}

func TestWebDAVMatcher_ChineseNumAndCandidates(t *testing.T) {
	if got := chineseNum(1); got != "一" {
		t.Errorf("chineseNum(1) = %s, want 一", got)
	}
	if got := chineseNum(2); got != "二" {
		t.Errorf("chineseNum(2) = %s, want 二", got)
	}
	if got := chineseNum(10); got != "十" {
		t.Errorf("chineseNum(10) = %s, want 十", got)
	}
	if got := chineseNum(12); got != "十二" {
		t.Errorf("chineseNum(12) = %s, want 十二", got)
	}

	cands := buildCandidateNames("庆余年", true, 2)
	expected := []string{
		"庆余年",
		"庆余年 第二季",
		"庆余年第二季",
		"庆余年 第2季",
		"庆余年第2季",
		"庆余年 2",
		"庆余年2",
	}
	candMap := make(map[string]bool)
	for _, c := range cands {
		candMap[c] = true
	}
	for _, exp := range expected {
		if !candMap[exp] {
			t.Errorf("expected candidate %q in %v", exp, cands)
		}
	}
}

func TestRunWebDAVScan_Success(t *testing.T) {
	gdb := setupWebDAVScanTestDB(t)

	// 1. 植入主站大类
	catTV := model.Category{Id: 1, Pid: 0, Name: "电视剧"}
	catMovie := model.Category{Id: 2, Pid: 0, Name: "电影"}
	gdb.Create(&catTV)
	gdb.Create(&catMovie)

	// 2. 植入主站影片及 match key
	masterFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid: 101,
		},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid:   1,
			Cid:   10,
			CName: "国产剧",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "庆余年",
			Year: 2019,
		},
	}
	gdb.Create(&masterFilm)

	keys := filmrepo.BuildMovieMatchKeysWithCategory(0, "庆余年", 1)
	for _, k := range keys {
		gdb.Create(&model.MovieMatchKey{Mid: 101, MatchKey: k})
	}

	// 3. Mock WebDAV 服务端
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/dav/media/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/%E5%BA%86%E4%BD%99%E5%B9%B4.2019.S01E01.1080p.mkv</D:href>
    <D:propstat>
      <D:prop>
        <D:getcontentlength>104857600</D:getcontentlength>
        <D:getetag>"etag_e01"</D:getetag>
        <D:getlastmodified>Mon, 12 Jan 2026 10:00:00 GMT</D:getlastmodified>
        <D:resourcetype/>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/%E5%BA%86%E4%BD%99%E5%B9%B4.2019.S01E02.1080p.mkv</D:href>
    <D:propstat>
      <D:prop>
        <D:getcontentlength>104857600</D:getcontentlength>
        <D:getetag>"etag_e02"</D:getetag>
        <D:getlastmodified>Mon, 12 Jan 2026 10:05:00 GMT</D:getlastmodified>
        <D:resourcetype/>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`))
	}))
	defer ts.Close()

	cfg := model.WebdavConfig{
		ServerURL:    ts.URL,
		RootPath:     "/dav/media",
		MediaType:    "tv",
		PlayFromName: "极速NAS",
		MinFileBytes: 50 * 1024 * 1024,
	}
	cfgJSON, _ := json.Marshal(cfg)

	source := model.FilmSource{
		Id:           "src_wdv_1",
		Name:         "我的WebDAV",
		SourceType:   model.SourceTypeWebdav,
		Grade:        model.SlaveCollect,
		State:        true,
		WebdavConfig: string(cfgJSON),
	}
	gdb.Create(&source)

	// 4. 执行扫描
	affected, err := RunWebDAVScan(context.Background(), &source)
	if err != nil {
		t.Fatalf("RunWebDAVScan failed: %v", err)
	}

	if len(affected) != 1 || affected[0] != 101 {
		t.Fatalf("expected affected mids [101], got %v", affected)
	}

	// 5. 验证播放列表
	var playlists []model.SlaveMoviePlaylist
	gdb.Where("source_id = ?", source.Id).Find(&playlists)
	if len(playlists) != 1 {
		t.Fatalf("expected 1 slave playlist, got %d", len(playlists))
	}
	pl := playlists[0]
	if pl.GroupName != "极速NAS" {
		t.Errorf("expected GroupName 极速NAS, got %s", pl.GroupName)
	}
	if !strings.Contains(pl.Content, "wdv://src_wdv_1/") {
		t.Errorf("expected content to have wdv:// link, got %s", pl.Content)
	}
	if !strings.Contains(pl.Content, "第1集") || !strings.Contains(pl.Content, "第2集") {
		t.Errorf("expected content to have 第1集 and 第2集, got %s", pl.Content)
	}

	// 6. 验证 Group 和 Item
	var group model.WebdavMediaGroup
	if err := gdb.Where("source_id = ?", source.Id).First(&group).Error; err != nil {
		t.Fatalf("media group not found: %v", err)
	}
	if group.GlobalMid != 101 {
		t.Errorf("expected GlobalMid 101, got %d", group.GlobalMid)
	}

	var items []model.WebdavScanItem
	gdb.Where("source_id = ?", source.Id).Find(&items)
	if len(items) != 2 {
		t.Fatalf("expected 2 scan items, got %d", len(items))
	}
	for _, it := range items {
		if it.Status != "scraped" {
			t.Errorf("item status expected scraped, got %s", it.Status)
		}
	}

	// 7. 验证报告
	var report model.WebdavScanReport
	if err := gdb.Where("source_id = ?", source.Id).First(&report).Error; err != nil {
		t.Fatalf("scan report not found: %v", err)
	}
	if report.Status != "done" || report.Found != 2 || report.Saved != 1 {
		t.Errorf("unexpected report: %+v", report)
	}
}

func TestRunWebDAVScan_CircuitBreaker(t *testing.T) {
	gdb := setupWebDAVScanTestDB(t)

	// 植入已有 scan item
	gdb.Create(&model.WebdavScanItem{
		SourceId: "src_cb",
		PathHash: "h1",
		RelPath:  "foo.mkv",
		Status:   "scraped",
	})
	gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "src_cb",
		MovieKey: "mk1",
		Content:  "content",
	})

	// Mock WebDAV 返回空列表（0 个文件）
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/dav/media/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
</D:multistatus>`))
	}))
	defer ts.Close()

	cfg := model.WebdavConfig{
		ServerURL: ts.URL,
		RootPath:  "/dav/media",
		MediaType: "movie",
	}
	cfgJSON, _ := json.Marshal(cfg)
	source := model.FilmSource{
		Id:           "src_cb",
		Name:         "熔断测试",
		SourceType:   model.SourceTypeWebdav,
		Grade:        model.SlaveCollect,
		WebdavConfig: string(cfgJSON),
	}
	gdb.Create(&source)

	_, err := RunWebDAVScan(context.Background(), &source)
	if err == nil || !strings.Contains(err.Error(), "熔断") {
		t.Fatalf("expected circuit breaker error, got %v", err)
	}

	// 验证已有播放列表未被清空
	var plCount int64
	gdb.Model(&model.SlaveMoviePlaylist{}).Where("source_id = ?", source.Id).Count(&plCount)
	if plCount != 1 {
		t.Errorf("playlist should be preserved during circuit breaker, got count %d", plCount)
	}

	// 验证报告状态为 storage_unmounted
	var report model.WebdavScanReport
	gdb.Where("source_id = ?", source.Id).Order("id DESC").First(&report)
	if report.Status != "storage_unmounted" {
		t.Errorf("expected report status storage_unmounted, got %s", report.Status)
	}
}

func TestRunWebDAVScan_MultiSeasonMerge(t *testing.T) {
	gdb := setupWebDAVScanTestDB(t)

	catTV := model.Category{Id: 1, Pid: 0, Name: "电视剧"}
	gdb.Create(&catTV)

	masterFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid: 202,
		},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid:   1,
			Cid:   10,
			CName: "国产剧",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "庆余年",
			Year: 2019,
		},
	}
	gdb.Create(&masterFilm)

	keys := filmrepo.BuildMovieMatchKeysWithCategory(0, "庆余年", 1)
	for _, k := range keys {
		gdb.Create(&model.MovieMatchKey{Mid: 202, MatchKey: k})
	}

	// Mock WebDAV 返回第一季和第二季
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/dav/media/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/%E5%BA%86%E4%BD%99%E5%B9%B4.S01E01.mkv</D:href>
    <D:propstat><D:prop><D:getcontentlength>104857600</D:getcontentlength><D:resourcetype/></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/%E5%BA%86%E4%BD%99%E5%B9%B4.S02E01.mkv</D:href>
    <D:propstat><D:prop><D:getcontentlength>104857600</D:getcontentlength><D:resourcetype/></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
</D:multistatus>`))
	}))
	defer ts.Close()

	cfg := model.WebdavConfig{
		ServerURL:    ts.URL,
		RootPath:     "/dav/media",
		MediaType:    "tv",
		PlayFromName: "我的线路",
	}
	cfgJSON, _ := json.Marshal(cfg)
	source := model.FilmSource{
		Id:           "src_multi_season",
		Name:         "多季合并源",
		SourceType:   model.SourceTypeWebdav,
		Grade:        model.SlaveCollect,
		WebdavConfig: string(cfgJSON),
	}
	gdb.Create(&source)

	affected, err := RunWebDAVScan(context.Background(), &source)
	if err != nil {
		t.Fatalf("RunWebDAVScan failed: %v", err)
	}
	if len(affected) != 1 || affected[0] != 202 {
		t.Fatalf("expected affected [202], got %v", affected)
	}

	var playlists []model.SlaveMoviePlaylist
	gdb.Where("source_id = ?", source.Id).Find(&playlists)
	if len(playlists) != 1 {
		t.Fatalf("expected 1 merged playlist, got %d", len(playlists))
	}
	// 多季时集数格式应为 S01E01 与 S02E01
	if !strings.Contains(playlists[0].Content, "S01E01") || !strings.Contains(playlists[0].Content, "S02E01") {
		t.Errorf("expected multi-season episode names S01E01 and S02E01, got %s", playlists[0].Content)
	}
}

func TestRunWebDAVScan_IncrementalSkip(t *testing.T) {
	gdb := setupWebDAVScanTestDB(t)

	catMovie := model.Category{Id: 2, Pid: 0, Name: "电影"}
	gdb.Create(&catMovie)

	masterFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid: 303,
		},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid:   2,
			Cid:   20,
			CName: "科幻片",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "流浪地球",
			Year: 2019,
		},
	}
	gdb.Create(&masterFilm)

	keys := filmrepo.BuildMovieMatchKeysWithCategory(0, "流浪地球", 2)
	for _, k := range keys {
		gdb.Create(&model.MovieMatchKey{Mid: 303, MatchKey: k})
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/dav/media/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/%E6%B5%81%E6%B5%AA%E5%9C%B0%E7%90%83.2019.1080p.mkv</D:href>
    <D:propstat>
      <D:prop>
        <D:getcontentlength>104857600</D:getcontentlength>
        <D:getetag>"etag_fixed"</D:getetag>
        <D:getlastmodified>Mon, 12 Jan 2026 10:00:00 GMT</D:getlastmodified>
        <D:resourcetype/>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`))
	}))
	defer ts.Close()

	cfg := model.WebdavConfig{
		ServerURL: ts.URL,
		RootPath:  "/dav/media",
		MediaType: "movie",
	}
	cfgJSON, _ := json.Marshal(cfg)
	source := model.FilmSource{
		Id:           "src_inc",
		Name:         "增量测试源",
		SourceType:   model.SourceTypeWebdav,
		Grade:        model.SlaveCollect,
		WebdavConfig: string(cfgJSON),
	}
	gdb.Create(&source)

	// 第一次扫描：全量入库
	_, err := RunWebDAVScan(context.Background(), &source)
	if err != nil {
		t.Fatalf("first scan failed: %v", err)
	}

	// 第二次扫描：指纹相同，应跳过
	_, err = RunWebDAVScan(context.Background(), &source)
	if err != nil {
		t.Fatalf("second scan failed: %v", err)
	}

	var latestReport model.WebdavScanReport
	gdb.Where("source_id = ?", source.Id).Order("id DESC").First(&latestReport)
	if latestReport.Skipped != 1 {
		t.Errorf("expected 1 skipped item in second scan report, got %d", latestReport.Skipped)
	}
}

func TestRunWebDAVScan_TruncatedDoesNotOffline(t *testing.T) {
	gdb := setupWebDAVScanTestDB(t)
	prev := utils.WebDAVListFileLimit
	utils.WebDAVListFileLimit = 1
	t.Cleanup(func() { utils.WebDAVListFileLimit = prev })

	gdb.Create(&model.WebdavScanItem{
		SourceId: "src_trunc",
		PathHash: "old_unseen",
		RelPath:  "old/unseen.mkv",
		Status:   "scraped",
		GroupKey: "old_group",
	})
	gdb.Create(&model.SlaveMoviePlaylist{
		SourceId: "src_trunc",
		MovieKey: "mk_old",
		Content:  "wdv://src_trunc/old",
	})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/dav/media/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/a.mkv</D:href>
    <D:propstat><D:prop><D:getcontentlength>104857600</D:getcontentlength><D:resourcetype/></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/b.mkv</D:href>
    <D:propstat><D:prop><D:getcontentlength>104857600</D:getcontentlength><D:resourcetype/></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
</D:multistatus>`))
	}))
	defer ts.Close()

	cfg := model.WebdavConfig{ServerURL: ts.URL, RootPath: "/dav/media", MediaType: "movie"}
	cfgJSON, _ := json.Marshal(cfg)
	source := model.FilmSource{
		Id:           "src_trunc",
		Name:         "截断测试",
		SourceType:   model.SourceTypeWebdav,
		Grade:        model.SlaveCollect,
		WebdavConfig: string(cfgJSON),
	}
	gdb.Create(&source)

	_, err := RunWebDAVScan(context.Background(), &source)
	if err != nil {
		t.Fatalf("RunWebDAVScan failed: %v", err)
	}

	var oldItem model.WebdavScanItem
	gdb.Where("source_id = ? AND path_hash = ?", source.Id, "old_unseen").First(&oldItem)
	if oldItem.Status == "missing" {
		t.Fatalf("truncated scan must not mark unseen files missing")
	}
	var plCount int64
	gdb.Model(&model.SlaveMoviePlaylist{}).Where("source_id = ?", source.Id).Count(&plCount)
	if plCount != 1 {
		t.Fatalf("truncated scan must not delete existing playlists, got %d", plCount)
	}
	var report model.WebdavScanReport
	gdb.Where("source_id = ?", source.Id).Order("id DESC").First(&report)
	if !report.Truncated {
		t.Fatalf("expected truncated report")
	}
}

func TestRunWebDAVScan_BoundSurvivesDivergedGroupKey(t *testing.T) {
	gdb := setupWebDAVScanTestDB(t)

	catMovie := model.Category{Id: 2, Pid: 0, Name: "电影"}
	gdb.Create(&catMovie)
	masterFilm := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 404},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 2, Cid: 20, CName: "科幻片"},
		FilmIndexContent:  model.FilmIndexContent{Name: "流浪地球", Year: 2019},
	}
	gdb.Create(&masterFilm)
	for _, k := range filmrepo.BuildMovieMatchKeysWithCategory(0, "流浪地球", 2) {
		gdb.Create(&model.MovieMatchKey{Mid: 404, MatchKey: k})
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/dav/media/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/%E6%B5%81%E6%B5%AA%E5%9C%B0%E7%90%83.2019.1080p.mkv</D:href>
    <D:propstat>
      <D:prop>
        <D:getcontentlength>104857600</D:getcontentlength>
        <D:getlastmodified>Mon, 12 Jan 2026 10:00:00 GMT</D:getlastmodified>
        <D:resourcetype/>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`))
	}))
	defer ts.Close()

	cfg := model.WebdavConfig{ServerURL: ts.URL, RootPath: "/dav/media", MediaType: "movie"}
	cfgJSON, _ := json.Marshal(cfg)
	source := model.FilmSource{
		Id:           "src_bound",
		Name:         "绑定保活",
		SourceType:   model.SourceTypeWebdav,
		Grade:        model.SlaveCollect,
		WebdavConfig: string(cfgJSON),
	}
	gdb.Create(&source)

	if _, err := RunWebDAVScan(context.Background(), &source); err != nil {
		t.Fatalf("first scan failed: %v", err)
	}

	var item model.WebdavScanItem
	gdb.Where("source_id = ?", source.Id).First(&item)
	oldKey := item.GroupKey
	gdb.Model(&item).Updates(map[string]any{"status": "bound", "group_key": "legacy_tmdb_key"})
	gdb.Model(&model.WebdavMediaGroup{}).Where("source_id = ? AND group_key = ?", source.Id, oldKey).
		Update("group_key", "legacy_tmdb_key")

	if _, err := RunWebDAVScan(context.Background(), &source); err != nil {
		t.Fatalf("second scan failed: %v", err)
	}

	gdb.Where("source_id = ?", source.Id).First(&item)
	if item.Status != "bound" || item.GroupKey != "legacy_tmdb_key" {
		t.Fatalf("bound item must survive rescan, got %+v", item)
	}
}
