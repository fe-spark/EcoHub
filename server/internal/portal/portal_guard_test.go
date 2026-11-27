package portal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	filmsnapshot "server/internal/film/snapshot"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/sourcecatalog"

	"github.com/gin-gonic/gin"
)

func TestProvideHandler_RejectsExcessiveIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parts := make([]string, 105)
	for i := range parts {
		parts[i] = "1"
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/provide/vod?ac=detail&ids="+strings.Join(parts, ","), nil)
	ProvideHd.HandleProvide(c)
	if !strings.Contains(w.Body.String(), "不能超过 100") {
		t.Fatalf("expected excessive ids rejection, got %s", w.Body.String())
	}
}

func TestRelateMovie_StripsPlayFromSuffixAndRejectsDisabledSource(t *testing.T) {
	gdb, _ := setupProvideTestDB(t)
	now := time.Now().Unix()

	if err := gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", Show: true, Sort: 1, StableKey: "movie"}).Error; err != nil {
		t.Fatalf("create cat root: %v", err)
	}
	if err := gdb.Create(&model.Category{Id: 10, Pid: 1, Name: "科幻", Show: true, Sort: 1, StableKey: "scifi"}).Error; err != nil {
		t.Fatalf("create cat sub: %v", err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src_primary", Name: "主站", Uri: "https://primary.test", State: true, Sort: 0}).Error; err != nil {
		t.Fatalf("create primary source: %v", err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src_disabled", Name: "停用站", Uri: "https://disabled.test", State: false, Sort: 1}).Error; err != nil {
		t.Fatalf("create disabled source: %v", err)
	}
	if err := sourcecatalog.SetPrimarySourceID("src_primary"); err != nil {
		t.Fatalf("set primary source: %v", err)
	}

	snaps := []model.FilmListSnapshot{
		{
			SnapshotVersion: "v_rel",
			Mid:             201,
			SourceId:        "src_primary",
			Name:            "片A",
			Pid:             1,
			Cid:             10,
			CategoryKey:     "source:src_primary:10",
			RootCategoryKey: "source:src_primary:1",
			UpdateStamp:     now,
		},
		{
			SnapshotVersion: "v_rel",
			Mid:             202,
			SourceId:        "src_primary",
			Name:            "片B",
			Pid:             1,
			Cid:             10,
			CategoryKey:     "source:src_primary:10",
			RootCategoryKey: "source:src_primary:1",
			UpdateStamp:     now,
		},
		{
			SnapshotVersion: "v_rel",
			Mid:             301,
			SourceId:        "src_disabled",
			Name:            "停用片",
			Pid:             1,
			Cid:             10,
			CategoryKey:     "source:src_disabled:10",
			RootCategoryKey: "source:src_disabled:1",
			UpdateStamp:     now,
		},
	}
	if err := filmsnapshot.WriteLiveFilmsFromSnapshots(snaps); err != nil {
		t.Fatalf("write snapshots: %v", err)
	}
	for _, s := range snaps {
		if err := gdb.Create(&model.FilmSourcePlaylist{
			Mid:          s.Mid,
			SourceId:     s.SourceId,
			LineKind:     "play",
			GroupIndex:   0,
			GroupName:    s.SourceId,
			EpisodeCount: 1,
			Content:      `[{"episode":"01","link":"http://video/01.m3u8"}]`,
		}).Error; err != nil {
			t.Fatalf("create playlist %d: %v", s.Mid, err)
		}
	}
	_ = filmsnapshot.SetActiveSnapshotVersion("v_rel")
	_ = filmsnapshot.LoadActiveFilmReadModel("v_rel")
	filmsnapshot.WaitActiveFilmSearchIndexBuilt()

	// 1. 传入带 #0 后缀的 source 应剥离后缀并匹配到 src_primary
	list1, err := IndexSvc.RelateMovie(201, &dto.Page{Current: 1, PageSize: 10}, "src_primary#0")
	if err != nil {
		t.Fatalf("RelateMovie with group suffix failed: %v", err)
	}
	if len(list1) != 1 || list1[0].Id != 202 {
		t.Fatalf("expected related movie [202] for src_primary#0, got %+v", list1)
	}

	// 2. 传入停用源应回落到主站 (src_primary)，不得返回停用源影片
	list2, err := IndexSvc.RelateMovie(201, &dto.Page{Current: 1, PageSize: 10}, "src_disabled#1")
	if err != nil {
		t.Fatalf("RelateMovie with disabled source failed: %v", err)
	}
	if len(list2) != 1 || list2[0].Id != 202 {
		t.Fatalf("expected fallback to primary [202] when source disabled, got %+v", list2)
	}
}
