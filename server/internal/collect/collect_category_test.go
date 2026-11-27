package collect

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestEnsureMasterCategoriesReady_RequiresSavedCategories(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CategoryMapping{}, &model.Category{}, &model.SourceCategory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb

	source := &model.FilmSource{Id: "src_new", Name: "樱花", Uri: "http://example.invalid/api"}
	err = ensureMasterCategoriesReady(source)
	if err == nil || !strings.Contains(err.Error(), "还没有分类") {
		t.Fatalf("expected missing category error, got %v", err)
	}

	if err := gdb.Create(&model.CategoryMapping{SourceId: source.Id, SourceTypeId: 13, CategoryId: 90}).Error; err != nil {
		t.Fatalf("seed mapping: %v", err)
	}
	if err := ensureMasterCategoriesReady(source); err != nil {
		t.Fatalf("saved mapping should allow collect, got %v", err)
	}
}

func TestFilterSourcesReadyForCollect_SkipsMissingCategory(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CategoryMapping{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	if err := gdb.Create(&model.CategoryMapping{SourceId: "ready", SourceTypeId: 1, CategoryId: 9}).Error; err != nil {
		t.Fatalf("seed mapping: %v", err)
	}

	got := filterSourcesReadyForCollect([]model.FilmSource{
		{Id: "ready", Name: "有分类"},
		{Id: "missing", Name: "缺分类"},
	})
	if len(got) != 1 || got[0].Id != "ready" {
		t.Fatalf("expected only the source with categories, got %+v", got)
	}
}

func TestSyncAllSourceCategories_CancelsOnTimeout(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.Category{}, &model.CategoryMapping{}, &model.SourceCategory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	origDB := db.Mdb
	db.Mdb = gdb
	sourcecatalog.InvalidateCollectSourceCatalog()
	t.Cleanup(func() {
		db.Mdb = origDB
		sourcecatalog.InvalidateCollectSourceCatalog()
	})

	reqCanceled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(reqCanceled)
	}))
	defer srv.Close()

	if err := gdb.Create(&model.FilmSource{Id: "slow_sync", Name: "慢站", Uri: srv.URL, State: true}).Error; err != nil {
		t.Fatalf("seed source: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	err = SpiderSvc.SyncAllSourceCategories(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	select {
	case <-reqCanceled:
	case <-time.After(time.Second):
		t.Fatal("超时后分类拉取请求未随 ctx 取消")
	}
	var count int64
	gdb.Model(&model.SourceCategory{}).Count(&count)
	if count != 0 {
		t.Fatalf("超时取消后不应写入分类，count=%d", count)
	}
}

func TestCollectCategory_SkipsSaveWhenSourceRemovedDuringFetch(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.Category{}, &model.CategoryMapping{}, &model.SourceCategory{}, &model.CollectSourceStats{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	origDB := db.Mdb
	db.Mdb = gdb
	sourcecatalog.InvalidateCollectSourceCatalog()
	t.Cleanup(func() {
		db.Mdb = origDB
		sourcecatalog.InvalidateCollectSourceCatalog()
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟拉取分类期间站点被重置/替换删除
		_ = gdb.Where("id = ?", "vanished_src").Delete(&model.FilmSource{}).Error
		sourcecatalog.InvalidateCollectSourceCatalog()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"pagecount":1,"class":[{"type_id":1,"type_pid":0,"type_name":"电影"}]}`))
	}))
	defer srv.Close()

	src := &model.FilmSource{Id: "vanished_src", Name: "已删站", Uri: srv.URL, State: true}
	if err := gdb.Create(src).Error; err != nil {
		t.Fatalf("seed source: %v", err)
	}

	if _, err := CollectCategory(context.Background(), src); err == nil {
		t.Fatal("拉取期间站点已被删除时，CollectCategory 应拒绝落库")
	}
	var count int64
	gdb.Model(&model.SourceCategory{}).Count(&count)
	if count != 0 {
		t.Fatalf("站点已删除后不应写入残留分类，count=%d", count)
	}
}
