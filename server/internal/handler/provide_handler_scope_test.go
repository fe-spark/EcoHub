package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"
)

func TestHandleProvide_AllEntryAndSourceType(t *testing.T) {
	gdb, _ := setupProvideTestDB(t)
	const version = "v_tvbox_scope"

	if err := gdb.Create(&[]model.FilmSource{
		{Id: "src_a", Name: "甲站", Uri: "https://src-a.example/api", State: true, Sort: 0},
		{Id: "src_b", Name: "乙站", Uri: "https://src-b.example/api", State: true, Sort: 1},
	}).Error; err != nil {
		t.Fatalf("sources: %v", err)
	}
	if err := gdb.Create(&[]model.Category{
		{Id: 11, Pid: 0, Name: "电影", Show: true, Sort: 1, StableKey: "source:src_a:1"},
		{Id: 21, Pid: 0, Name: "电影", Show: true, Sort: 1, StableKey: "source:src_b:1"},
	}).Error; err != nil {
		t.Fatalf("categories: %v", err)
	}
	if err := gdb.Create(&[]model.SourceCategory{
		{SourceId: "src_a", SourceTypeId: 1, RawName: "电影", Sort: 1, Depth: 0},
		{SourceId: "src_b", SourceTypeId: 1, RawName: "电影", Sort: 1, Depth: 0},
	}).Error; err != nil {
		t.Fatalf("source categories: %v", err)
	}
	if err := gdb.Create(&[]model.CategoryMapping{
		{SourceId: "src_a", SourceTypeId: 1, CategoryId: 11},
		{SourceId: "src_b", SourceTypeId: 1, CategoryId: 21},
	}).Error; err != nil {
		t.Fatalf("mappings: %v", err)
	}

	snaps := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 1, Pid: 11, Name: "甲片", CategoryKey: "source:src_a:1", RootCategoryKey: "source:src_a:1", UpdateStamp: 10},
		{SnapshotVersion: version, Mid: 2, Pid: 21, Name: "乙片", CategoryKey: "source:src_b:1", RootCategoryKey: "source:src_b:1", UpdateStamp: 20},
	}
	if err := filmsnapshot.WriteLiveFilmsFromSnapshots(snaps); err != nil {
		t.Fatalf("films: %v", err)
	}
	for _, row := range []model.FilmSourcePlaylist{
		{Mid: 1, SourceId: "src_a", LineKind: "play", GroupIndex: 0},
		{Mid: 2, SourceId: "src_b", LineKind: "play", GroupIndex: 0},
	} {
		if err := gdb.Create(&row).Error; err != nil {
			t.Fatalf("playlist: %v", err)
		}
	}
	if err := filmsnapshot.SetActiveSnapshotVersion(version); err != nil {
		t.Fatalf("version: %v", err)
	}

	gin.SetMode(gin.TestMode)

	all := provideJSON(t, "/api/provide/vod?ac=list")
	classNames := provideClassNames(t, all)
	if len(classNames) != 1 || classNames[1] != "电影" {
		t.Fatalf("未传 source 应为主站分类 = %+v", classNames)
	}
	allIDs := provideVodIDs(t, all)
	if !allIDs[1] || allIDs[2] {
		t.Fatalf("未传 source 应只列出主站影片, got %+v", allIDs)
	}
	if provideFirstTypeID(t, all) != 1 {
		t.Fatalf("主站返回的 type_id = %v", provideFirstTypeID(t, all))
	}

	scoped := provideJSON(t, "/api/provide/vod?ac=list&source=src_b&t=1")
	sourceClass := provideClassNames(t, scoped)
	if len(sourceClass) != 1 || sourceClass[1] != "电影" {
		t.Fatalf("订阅源分类 = %+v", sourceClass)
	}
	scopedIDs := provideVodIDs(t, scoped)
	if scopedIDs[1] || !scopedIDs[2] {
		t.Fatalf("订阅源 type 1 = %+v", scopedIDs)
	}
	if provideFirstTypeID(t, scoped) != 1 {
		t.Fatalf("订阅源返回的 type_id = %v", provideFirstTypeID(t, scoped))
	}

	miss := provideJSON(t, "/api/provide/vod?ac=list&source=src_b&t=21")
	if len(provideVodIDs(t, miss)) != 0 {
		t.Fatalf("订阅源不认本地分类 id, got %+v", miss["list"])
	}

	local := provideJSON(t, "/api/provide/vod?ac=list&t=21")
	if len(provideVodIDs(t, local)) != 0 {
		t.Fatalf("主站不认本地分类 id, got %+v", local["list"])
	}
}

func provideJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = "127.0.0.1:8080"
	c.Request = req
	ProvideHd.HandleProvide(c)
	if w.Code != http.StatusOK {
		t.Fatalf("%s status %d body %s", raw, w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return body
}

func provideClassNames(t *testing.T, body map[string]any) map[float64]string {
	t.Helper()
	raw, ok := body["class"].([]any)
	if !ok {
		t.Fatalf("class missing: %+v", body["class"])
	}
	out := make(map[float64]string, len(raw))
	for _, item := range raw {
		row := item.(map[string]any)
		out[row["type_id"].(float64)] = row["type_name"].(string)
	}
	return out
}

func provideVodIDs(t *testing.T, body map[string]any) map[float64]bool {
	t.Helper()
	raw, ok := body["list"].([]any)
	if !ok {
		t.Fatalf("list missing: %+v", body["list"])
	}
	out := make(map[float64]bool, len(raw))
	for _, item := range raw {
		row := item.(map[string]any)
		out[row["vod_id"].(float64)] = true
	}
	return out
}

func provideFirstTypeID(t *testing.T, body map[string]any) float64 {
	t.Helper()
	raw := body["list"].([]any)
	if len(raw) == 0 {
		t.Fatal("empty list")
	}
	return raw[0].(map[string]any)["type_id"].(float64)
}
