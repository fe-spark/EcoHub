package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// /categories 带 sourceId 时返回该站一级分类，不带时用首选站。
func TestCategoriesInfo_SourceID(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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
