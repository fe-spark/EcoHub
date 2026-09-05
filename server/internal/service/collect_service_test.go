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
	"server/internal/notify"

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
		&model.MoviePlaylist{},
		&model.FailureRecord{},
		&model.CollectSourceStats{},
		&model.Category{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
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
	gdb.Create(&model.MoviePlaylist{
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
	var legacyCount int64
	gdb.Model(&model.MoviePlaylist{}).Where("source_id = ?", "src_new").Count(&legacyCount)
	if legacyCount != 0 {
		t.Fatalf("expected 0 residual legacy playlists for new master, got %d", legacyCount)
	}
}

func TestCollectService_UpdateFilmSource_MasterDowngradeCleansOldMasterFailures(t *testing.T) {
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

	// 插入旧主站的失败记录
	if err := gdb.Create(&model.FailureRecord{
		OriginId:   "master_old",
		PageNumber: 1,
		Hour:       24,
	}).Error; err != nil {
		t.Fatalf("create failure record: %v", err)
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

	// 验证降级主站的关联失败记录已被清空
	var failureCount int64
	gdb.Model(&model.FailureRecord{}).Where("origin_id = ?", "master_old").Count(&failureCount)
	if failureCount != 0 {
		t.Fatalf("expected 0 failure records for downgraded master, got %d", failureCount)
	}

	// 排空在途异步通知协程，确保测试结束前完全执行完毕
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer drainCancel()
	_ = notify.WaitPendingPublishes(drainCtx)
}
