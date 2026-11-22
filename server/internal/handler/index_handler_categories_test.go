package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// /categories 带 sourceId 时返回该站一级分类，不带时用首选站。
func TestCategoriesInfo_SourceID(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.Category{}, &model.CategoryMapping{}, &model.SourceCategory{}, &model.MappingRule{}); err != nil {
		t.Fatal(err)
	}
	origMdb, origRdb := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	t.Cleanup(func() { db.Mdb, db.Rdb = origMdb, origRdb })

	gdb.Create(&[]model.FilmSource{
		{Id: "src1", Uri: "http://1.test", State: true, Sort: 1},
		{Id: "src2", Uri: "http://2.test", State: true, Sort: 2},
	})
	gdb.Create(&[]model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 1, RawName: "电影", Sort: 1},
		{SourceId: "src2", SourceTypeId: 2, RawName: "电视剧", Sort: 1},
	})

	names := func(path string) []string {
		t.Helper()
		c, w := testContext(http.MethodGet, path)
		(&IndexHandler{}).CategoriesInfo(c)
		resp := decodeResponse(t, w)
		if resp.Code != dto.SUCCESS {
			t.Fatalf("%s code=%d msg=%s", path, resp.Code, resp.Msg)
		}
		raw, _ := json.Marshal(resp.Data)
		var list []struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &list)
		out := make([]string, 0, len(list))
		for _, it := range list {
			out = append(out, it.Name)
		}
		return out
	}
	if got := fmt.Sprint(names("/api/categories")); got != "[电影]" {
		t.Fatalf("default categories = %s", got)
	}
	if got := fmt.Sprint(names("/api/categories?sourceId=src2")); got != "[电视剧]" {
		t.Fatalf("src2 categories = %s", got)
	}
}

func TestCategoryPages_EmptySearchIndexState(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.Category{}, &model.CategoryMapping{}, &model.SourceCategory{}, &model.MappingRule{}); err != nil {
		t.Fatal(err)
	}
	origMdb, origRdb := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	filmsnapshot.ClearActiveFilmReadModel()
	filmsnapshot.ResetActiveSnapshotFallbackForTest()
	t.Cleanup(func() {
		filmsnapshot.ClearActiveFilmReadModel()
		filmsnapshot.ResetActiveSnapshotFallbackForTest()
		repository.MarkCategoryChanged()
		db.Mdb, db.Rdb = origMdb, origRdb
	})

	gdb.Create(&model.FilmSource{Id: "src1", Uri: "http://1.test", State: true, Sort: 1})
	gdb.Create(&model.Category{Id: 17, Pid: 0, Name: "动漫", Show: true, Sort: 1})
	gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 17, RawName: "动漫", Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 17, CategoryId: 17})
	repository.ReloadMappingRules()
	repository.MarkCategoryChanged()

	// 1. 有效分类在检索索引未就绪时：overview 返回成功且标记 indexReady=false
	c1, w1 := testContext(http.MethodGet, "/api/categories/overview?pid=17")
	(&IndexHandler{}).FilmClassify(c1)
	r1 := decodeResponse(t, w1)
	if r1.Code != dto.SUCCESS {
		t.Fatalf("valid pid overview expected success, got code=%d msg=%s", r1.Code, r1.Msg)
	}
	d1, ok := r1.Data.(map[string]any)
	if !ok || d1["title"] == nil || d1["indexReady"] != false {
		t.Fatalf("unexpected overview payload: %+v", r1.Data)
	}

	// 2. 不存在的分类：overview 仍返回失败
	c2, w2 := testContext(http.MethodGet, "/api/categories/overview?pid=9999")
	(&IndexHandler{}).FilmClassify(c2)
	r2 := decodeResponse(t, w2)
	if r2.Code != dto.FAILED {
		t.Fatalf("invalid pid overview expected failed, got code=%d", r2.Code)
	}

	// 3. 有效分类在检索索引未就绪时：filter 返回成功且携带 title 和 indexReady=false
	c3, w3 := testContext(http.MethodGet, "/api/films/filter?pid=17")
	(&IndexHandler{}).FilmTagSearch(c3)
	r3 := decodeResponse(t, w3)
	if r3.Code != dto.SUCCESS {
		t.Fatalf("valid pid filter expected success, got code=%d msg=%s", r3.Code, r3.Msg)
	}
	d3, ok := r3.Data.(map[string]any)
	if !ok || d3["title"] == nil || d3["indexReady"] != false {
		t.Fatalf("unexpected filter payload: %+v", r3.Data)
	}

	// 4. 不存在的分类：filter 返回 title=nil 以便前台识别分类失效
	c4, w4 := testContext(http.MethodGet, "/api/films/filter?pid=9999")
	(&IndexHandler{}).FilmTagSearch(c4)
	r4 := decodeResponse(t, w4)
	if r4.Code != dto.SUCCESS {
		t.Fatalf("invalid pid filter expected wrapper success, got code=%d msg=%s", r4.Code, r4.Msg)
	}
	d4, ok := r4.Data.(map[string]any)
	if !ok || d4["title"] != nil {
		t.Fatalf("invalid pid filter expected nil title, got: %+v", r4.Data)
	}
}
