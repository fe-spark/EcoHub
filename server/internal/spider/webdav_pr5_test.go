package spider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	filmrepo "server/internal/repository/film"
	"server/internal/repository/support"
)

func TestWebDAV_PR5_BindAndCascadeDelete(t *testing.T) {
	gdb := setupWebDAVScanTestDB(t)

	// 1. Mock TMDB Server
	mockTMDB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/3/movie/550" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           550,
				"title":        "搏击俱乐部",
				"release_date": "1999-10-15",
				"genres":       []map[string]any{{"id": 18, "name": "剧情"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer mockTMDB.Close()

	// 2. 初始化分类与主站电影
	cat := model.Category{Id: 1, Name: "电影", Pid: 0}
	gdb.Create(&cat)
	support.RefreshCategoryCache()

	master := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid: 888,
		},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid:   1,
			Cid:   1,
			CName: "电影",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "搏击俱乐部",
			Year: 1999,
		},
	}
	gdb.Create(&master)

	keys := filmrepo.BuildMovieMatchKeysWithCategory(0, "搏击俱乐部", 1)
	for _, k := range keys {
		gdb.Create(&model.MovieMatchKey{Mid: 888, MatchKey: k})
	}

	// 3. 初始化 WebDAV 源
	sourceCfg := model.WebdavConfig{
		ServerURL:       "http://127.0.0.1:8080/dav",
		RootPath:        "/media/movies",
		MediaType:       "movie",
		TmdbApiKey:      "test_key",
		TmdbBaseURL:     mockTMDB.URL,
		ScanIntervalMin: 60,
	}
	cfgBytes, _ := json.Marshal(sourceCfg)
	source := model.FilmSource{
		Id:           "src_webdav_pr5",
		Name:         "我的网盘电影",
		SourceType:   model.SourceTypeWebdav,
		Grade:        model.SlaveCollect,
		State:        true,
		Uri:          "webdav://127.0.0.1:8080/dav?root=%2Fmedia%2Fmovies",
		WebdavConfig: string(cfgBytes),
	}
	gdb.Create(&source)

	// 4. 创建未匹配扫描项
	item := model.WebdavScanItem{
		SourceId:  source.Id,
		PathHash:  "hash_fight_club",
		RelPath:   "Movies/Fight.Club.1999.mkv",
		Size:      1024 * 1024 * 500,
		Status:    "unmatched",
		GroupKey:  "unmatched_old_group",
		Title:     "Fight Club",
		Year:      1999,
		LastError: "未找到匹配影视",
	}
	gdb.Create(&item)

	// 5. 执行手动绑定
	bindReq := model.WebdavBindRequest{
		SourceId:  source.Id,
		ItemIds:   []uint64{item.ID},
		TmdbId:    550,
		MediaType: "movie",
	}
	mediaGroup, affectedMids, err := BindWebDAVItems(bindReq)
	if err != nil {
		t.Fatalf("BindWebDAVItems failed: %v", err)
	}
	if mediaGroup == nil || mediaGroup.GlobalMid != 888 {
		t.Fatalf("expected mediaGroup GlobalMid = 888, got %+v", mediaGroup)
	}
	if len(affectedMids) == 0 || affectedMids[0] != 888 {
		t.Fatalf("expected affectedMids contains 888, got %v", affectedMids)
	}

	// 验证 item 状态变为 bound
	var updatedItem model.WebdavScanItem
	gdb.First(&updatedItem, item.ID)
	if updatedItem.Status != "bound" || updatedItem.TmdbId != 550 {
		t.Fatalf("expected item status bound with tmdb 550, got %+v", updatedItem)
	}
	if updatedItem.GroupKey != "unmatched_old_group" {
		t.Fatalf("bind must keep scan group_key, got %s", updatedItem.GroupKey)
	}
	if mediaGroup.GroupKey != "unmatched_old_group" {
		t.Fatalf("media group must use scan group_key, got %s", mediaGroup.GroupKey)
	}

	// 验证生成了 SlaveMoviePlaylist
	var playlists []model.SlaveMoviePlaylist
	gdb.Where("source_id = ?", source.Id).Find(&playlists)
	if len(playlists) == 0 {
		t.Fatalf("expected SlaveMoviePlaylist generated for source %s", source.Id)
	}

	// 6. 验证 DelFilmSearch 级联清理
	delErr := filmrepo.DelFilmSearch(888)
	if delErr != nil {
		t.Fatalf("DelFilmSearch(888) failed: %v", delErr)
	}

	// 验证 WebdavMediaGroup 已删除
	var groupCount int64
	gdb.Model(&model.WebdavMediaGroup{}).Where("global_mid = ?", 888).Count(&groupCount)
	if groupCount != 0 {
		t.Fatalf("expected WebdavMediaGroup for global_mid 888 deleted, got count %d", groupCount)
	}

	// 验证关联的 WebdavScanItem 状态重置为 unmatched
	var resetItem model.WebdavScanItem
	gdb.First(&resetItem, item.ID)
	if resetItem.Status != "unmatched" || resetItem.LastError != "关联的主站影片已删除" {
		t.Fatalf("expected item reset to unmatched, got %+v", resetItem)
	}
}

func TestWebDAV_PR5_CronCollectIntegration(t *testing.T) {
	setupWebDAVScanTestDB(t)

	sourceCfg := model.WebdavConfig{
		ServerURL: "http://127.0.0.1:8080/dav",
		RootPath:  "/media/movies",
		MediaType: "movie",
	}
	cfgBytes, _ := json.Marshal(sourceCfg)
	s := model.FilmSource{
		Id:           "src_cron_test",
		Name:         "计划任务WebDAV测试站",
		SourceType:   model.SourceTypeWebdav,
		Grade:        model.SlaveCollect,
		State:        true,
		Uri:          "webdav://127.0.0.1:8080/dav",
		WebdavConfig: string(cfgBytes),
	}
	db.Mdb.Create(&s)

	// 验证 BatchCollectPrepared 能正常处理 WebDAV 源
	BatchCollectPrepared(model.NotifyTriggerCron, 24, []model.FilmSource{s})
}
