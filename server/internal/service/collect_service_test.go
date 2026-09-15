package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/notify"
	"server/internal/spider"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupCollectServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.SlaveMoviePlaylist{},
		&model.CollectSourceStats{},
		&model.Category{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
		&model.FailureRecord{},
		&model.WebdavScanItem{},
		&model.WebdavMediaGroup{},
		&model.WebdavScanReport{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	db.Mdb = gdb
	return gdb
}

func mockCollectServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"ok","page":1,"pagecount":1,"limit":"20","total":0,"list":[],"class":[{"type_id":1,"type_name":"电影"}]}`))
	}))
}

func TestCollectService_SaveFilmSource_MasterCleansSlavePlaylists(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)

	ts := mockCollectServer()
	defer ts.Close()

	// 先前已有主站
	gdb.Create(&model.FilmSource{
		Id:    "master_old",
		Name:  "旧主站",
		Uri:   ts.URL + "/old",
		Grade: model.MasterCollect,
		State: true,
	})

	// 新站（ID: src_new），但在附属表中存在历史残留
	gdb.Create(&model.SlaveMoviePlaylist{
		SourceId:   "src_new",
		MovieKey:   "k1",
		GroupIndex: 0,
		GroupName:  "线路1",
		Content:    "[]",
	})

	srv := &CollectService{}
	newMaster := model.FilmSource{
		Id:    "src_new",
		Name:  "新主站",
		Uri:   ts.URL + "/new",
		Grade: model.MasterCollect,
		State: true,
	}

	// 触发 SaveFilmSource
	err := srv.SaveFilmSource(newMaster)
	if err != nil {
		t.Fatalf("SaveFilmSource failed: %v", err)
	}

	// 1. 旧主站应被自动降级为附属站 (SlaveCollect)
	var oldMaster model.FilmSource
	if err := gdb.First(&oldMaster, "id = ?", "master_old").Error; err != nil {
		t.Fatalf("query old master: %v", err)
	}
	if oldMaster.Grade != model.SlaveCollect {
		t.Fatalf("expected old master to be demoted to SlaveCollect, got %v", oldMaster.Grade)
	}

	// 2. 新主站历史残留必须被物理清空
	var slaveCount int64
	gdb.Model(&model.SlaveMoviePlaylist{}).Where("source_id = ?", "src_new").Count(&slaveCount)
	if slaveCount != 0 {
		t.Fatalf("expected 0 residual slave playlists for new master, got %d", slaveCount)
	}
}

func TestCollectService_UpdateFilmSource_MasterDowngrade(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)

	// 原主站
	if err := gdb.Create(&model.FilmSource{
		Id:    "master_old",
		Name:  "旧主站",
		Uri:   "http://master.old/json",
		Grade: model.MasterCollect,
		State: true,
	}).Error; err != nil {
		t.Fatalf("create master_old failed: %v", err)
	}
	// 原附属站
	if err := gdb.Create(&model.FilmSource{
		Id:    "slave_1",
		Name:  "附属站1",
		Uri:   "http://slave1.old/json",
		Grade: model.SlaveCollect,
		State: true,
	}).Error; err != nil {
		t.Fatalf("create slave_1 failed: %v", err)
	}

	srv := &CollectService{}
	ts := mockCollectServer()
	defer ts.Close()

	// 将 master_old 降级为附属站（触发 masterDowngrade=true）
	demoted := model.FilmSource{
		Id:    "master_old",
		Name:  "旧主站降级",
		Uri:   "http://master.old/json",
		Grade: model.SlaveCollect,
		State: true,
	}

	err := srv.UpdateFilmSource(demoted)
	if err != nil {
		t.Fatalf("UpdateFilmSource failed: %v", err)
	}

	var updated model.FilmSource
	if err := gdb.First(&updated, "id = ?", "master_old").Error; err != nil {
		t.Fatalf("query updated master_old: %v", err)
	}
	if updated.Grade != model.SlaveCollect {
		t.Fatalf("expected master_old grade to be SlaveCollect, got %v", updated.Grade)
	}

	// 排空在途异步通知协程，确保测试结束前完全执行完毕
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer drainCancel()
	_ = notify.WaitPendingPublishes(drainCtx)
}

func TestCollectService_UpdateSlaveUriWhileOtherTaskRunning(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	srv := &CollectService{}
	t.Cleanup(spider.InjectCollectTaskForTest("other_running"))

	slave := model.FilmSource{
		Id:    "slave_idle",
		Name:  "空闲附属站",
		Uri:   "http://slave.old/json",
		Grade: model.SlaveCollect,
		State: true,
	}
	if err := gdb.Create(&slave).Error; err != nil {
		t.Fatalf("create slave: %v", err)
	}
	updated := slave
	updated.Uri = "http://slave.new/json"
	if err := srv.UpdateFilmSource(updated); err != nil {
		t.Fatalf("idle slave URI change should succeed while other source is collecting: %v", err)
	}
}

func TestCollectService_UpdateMasterUriBlockedWhileAnyTaskRunning(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	srv := &CollectService{}
	t.Cleanup(spider.InjectCollectTaskForTest("other_running"))

	master := model.FilmSource{
		Id:    "master_1",
		Name:  "主站",
		Uri:   "http://master.old/json",
		Grade: model.MasterCollect,
		State: true,
	}
	if err := gdb.Create(&master).Error; err != nil {
		t.Fatalf("create master: %v", err)
	}
	updated := master
	updated.Uri = "http://master.new/json"
	err := srv.UpdateFilmSource(updated)
	if err == nil {
		t.Fatal("expected master URI change to be blocked while any collect task is running")
	}
}

func TestCollectService_UpdateSelfBlockedWhenQueued(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	srv := &CollectService{}
	t.Cleanup(spider.InjectCollectTaskForTest("slave_busy"))

	slave := model.FilmSource{
		Id:    "slave_busy",
		Name:  "采集中的附属站",
		Uri:   "http://slave.busy/json",
		Grade: model.SlaveCollect,
		State: true,
	}
	if err := gdb.Create(&slave).Error; err != nil {
		t.Fatalf("create slave: %v", err)
	}
	updated := slave
	updated.Name = "改名"
	err := srv.UpdateFilmSource(updated)
	if err == nil {
		t.Fatal("expected edit to be blocked while this source is in the collect queue")
	}
}

func TestCollectService_UpdateWebDAVUriClearsScanState(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	srv := &CollectService{}

	source := model.FilmSource{
		Id:         "wdv_uri",
		Name:       "WebDAV 源",
		Uri:        "webdav|http://nas/old|/media",
		Grade:      model.SlaveCollect,
		State:      true,
		SourceType: model.SourceTypeWebDAV,
	}
	if err := gdb.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	gdb.Create(&model.WebdavScanItem{SourceId: source.Id, PathHash: "h1", RelPath: "old.mkv", Status: "scraped"})
	gdb.Create(&model.WebdavMediaGroup{SourceId: source.Id, GroupKey: "g1", GlobalMid: 1})
	gdb.Create(&model.SlaveMoviePlaylist{SourceId: source.Id, MovieKey: "mk1", Content: "wdv://wdv_uri/old"})
	gdb.Create(&model.WebdavScanReport{SourceId: source.Id, Status: "done"})

	updated := source
	updated.Uri = "webdav|http://nas/new|/other"
	if err := srv.UpdateFilmSource(updated); err != nil {
		t.Fatalf("UpdateFilmSource: %v", err)
	}

	var itemCount, groupCount, plCount, reportCount int64
	gdb.Model(&model.WebdavScanItem{}).Where("source_id = ?", source.Id).Count(&itemCount)
	gdb.Model(&model.WebdavMediaGroup{}).Where("source_id = ?", source.Id).Count(&groupCount)
	gdb.Model(&model.SlaveMoviePlaylist{}).Where("source_id = ?", source.Id).Count(&plCount)
	gdb.Model(&model.WebdavScanReport{}).Where("source_id = ?", source.Id).Count(&reportCount)
	if itemCount != 0 || groupCount != 0 || plCount != 0 || reportCount != 0 {
		t.Fatalf("uri change must clear webdav state, items=%d groups=%d playlists=%d reports=%d", itemCount, groupCount, plCount, reportCount)
	}
}

func TestCollectService_FailureRecords(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	srv := &CollectService{}

	// 1. 插入测试失败记录
	rec1 := model.FailureRecord{
		OriginId:   "src_1",
		OriginName: "源站1",
		Uri:        "http://src1/api",
		PageNumber: 1,
		Hour:       24,
		Cause:      "timeout",
		Status:     model.FailureRecordStatusPending,
		RetryCount: 1,
	}
	rec2 := model.FailureRecord{
		OriginId:   "src_1",
		OriginName: "源站1",
		Uri:        "http://src1/api",
		PageNumber: 2,
		Hour:       24,
		Cause:      "rate limit",
		Status:     model.FailureRecordStatusSuccess,
		RetryCount: 2,
	}
	rec3 := model.FailureRecord{
		OriginId:   "src_2",
		OriginName: "源站2",
		Uri:        "http://src2/api",
		PageNumber: 3,
		Hour:       24,
		Cause:      "server error",
		Status:     model.FailureRecordStatusFailed,
		RetryCount: 5,
	}
	gdb.Create(&rec1)
	gdb.Create(&rec2)
	gdb.Create(&rec3)

	// 2. 测试 GetRecordList
	params := model.RecordRequestVo{
		Paging: &dto.Page{Current: 1, PageSize: 10},
		Status: -1,
	}
	list := srv.GetRecordList(params)
	if len(list) != 3 {
		t.Fatalf("expected 3 failure records, got %d", len(list))
	}

	// 3. 测试 GetRecordOptions
	opts := srv.GetRecordOptions()
	if len(opts["status"]) == 0 {
		t.Fatalf("expected non-empty status options")
	}

	// 4. 测试 ClearRetriedRecords (应清理 success 和 failed，保留 pending)
	srv.ClearRetriedRecords()
	listAfterClear := srv.GetRecordList(params)
	if len(listAfterClear) != 1 {
		t.Fatalf("expected 1 record after ClearRetriedRecords, got %d", len(listAfterClear))
	}
	if listAfterClear[0].Status != model.FailureRecordStatusPending {
		t.Fatalf("expected remaining record to be pending, got %d", listAfterClear[0].Status)
	}

	// 5. 测试 ClearAllRecord (truncate table)
	srv.ClearAllRecord()
	listAfterTruncate := srv.GetRecordList(params)
	if len(listAfterTruncate) != 0 {
		t.Fatalf("expected 0 records after ClearAllRecord, got %d", len(listAfterTruncate))
	}
}
