package banner

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/sourcecatalog"
	"server/internal/testutil"
	"testing"
)

func setupManageConfigDB(t *testing.T) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.PrimaryState{}, &model.FilmIndex{}, &model.FilmSourcePlaylist{}, &model.BannerSlot{}, &model.Category{}, &model.CategoryMapping{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
}

func TestBannerHandlers(t *testing.T) {
	setupManageConfigDB(t)
	h := &BannerHandler{}
	db.Mdb.Create(&model.FilmSource{Id: "src1", Name: "站一", Uri: "http://src1.test", State: true, Sort: 1})
	db.Mdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101, FirstSourceId: "src1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "测试影片", Area: "美国", Actor: "演员"},
	})
	db.Mdb.Create(&model.FilmSourcePlaylist{Mid: 101, SourceId: "src1", LineKind: model.LineKindPlay})

	type listData struct {
		SourceId string `json:"sourceId"`
		Revision string `json:"revision"`
		Items    []struct {
			Id    string `json:"id"`
			Mid   int64  `json:"mid"`
			Area  string `json:"area"`
			Blurb string `json:"blurb"`
		} `json:"items"`
	}
	list := func() listData {
		t.Helper()
		c, w := testutil.NewTestContext(http.MethodGet, "/api/manage/banners?sourceId=src1")
		h.BannerList(c)
		resp := testutil.DecodeResponse(t, w)
		if resp.Code != dto.SUCCESS {
			t.Fatalf("list code=%d msg=%s", resp.Code, resp.Msg)
		}
		raw, _ := json.Marshal(resp.Data)
		var out listData
		_ = json.Unmarshal(raw, &out)
		return out
	}

	// 按指定采集站读取：src2 应返回 src2 自身轮播列表（空），并带出所有站点选项。
	db.Mdb.Create(&model.FilmSource{Id: "src2", Name: "站二", Uri: "http://src2.test", State: true, Sort: 2})
	c, w := testutil.NewTestContext(http.MethodGet, "/api/manage/banners?sourceId=src2")
	h.BannerList(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.SUCCESS {
		t.Fatalf("src2 list failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	raw, _ := json.Marshal(testutil.DecodeResponse(t, w).Data)
	var src2List listData
	_ = json.Unmarshal(raw, &src2List)
	if src2List.SourceId != "src2" || len(src2List.Items) != 0 {
		t.Fatalf("read must target requested sourceId=src2, got source=%s items=%d", src2List.SourceId, len(src2List.Items))
	}

	// 非主站（src2）选片直查与轮播写入均按 src2 隔离。
	db.Mdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 102, FirstSourceId: "src2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "站二影片", Area: "日本"},
	})
	db.Mdb.Create(&model.FilmSourcePlaylist{Mid: 102, SourceId: "src2", LineKind: model.LineKindPlay})
	c, w = testutil.NewTestContext(http.MethodGet, "/api/manage/banners/films?sourceId=src2&name=站二")
	h.BannerFilms(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.SUCCESS {
		t.Fatalf("src2 banner films failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	c, w = testutil.NewJSONContext(http.MethodPost, "/api/manage/banners", `{"sourceId":"src2","mid":102}`)
	h.BannerCreate(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.SUCCESS {
		t.Fatalf("non-primary create failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	// 跨站影片仍拒绝
	c, w = testutil.NewJSONContext(http.MethodPost, "/api/manage/banners", `{"sourceId":"src2","mid":101}`)
	h.BannerCreate(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.FAILED {
		t.Fatalf("cross-site create want FAILED, got code=%d body=%s", resp.Code, w.Body.String())
	}

	// 新增
	c, w = testutil.NewJSONContext(http.MethodPost, "/api/manage/banners", `{"sourceId":"src1","mid":101}`)
	h.BannerCreate(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.SUCCESS {
		t.Fatalf("create code=%d msg=%s", resp.Code, resp.Msg)
	}
	before := list()
	if len(before.Items) != 1 || before.Items[0].Area != "美国" {
		t.Fatalf("list after create: %+v", before)
	}
	id := before.Items[0].Id

	// 部分更新：只改简介，地区仍来自片库。
	c, w = testutil.NewJSONContext(http.MethodPatch, "/api/manage/banners/"+id, `{"blurb":"新简介"}`)
	c.Params = gin.Params{{Key: "id", Value: id}}
	h.BannerUpdate(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.SUCCESS {
		t.Fatalf("patch code=%d msg=%s", resp.Code, resp.Msg)
	}
	after := list()
	if after.Items[0].Blurb != "新简介" || after.Items[0].Area != "美国" {
		t.Fatalf("patch result: %+v", after.Items)
	}

	// 过期的 revision 保存顺序返回 409。
	body, _ := json.Marshal(map[string]any{"sourceId": "src1", "revision": before.Revision, "ids": []string{id}})
	c, w = testutil.NewJSONContext(http.MethodPut, "/api/manage/banners/order", string(body))
	h.BannerOrder(c)
	if w.Code != http.StatusConflict || testutil.DecodeResponse(t, w).Code != http.StatusConflict {
		t.Fatalf("stale order want 409, got status=%d body=%s", w.Code, w.Body.String())
	}
	body, _ = json.Marshal(map[string]any{"sourceId": "src1", "revision": after.Revision, "ids": []string{id}})
	c, w = testutil.NewJSONContext(http.MethodPut, "/api/manage/banners/order", string(body))
	h.BannerOrder(c)
	if w.Code != http.StatusOK || testutil.DecodeResponse(t, w).Code != dto.SUCCESS {
		t.Fatalf("order want success, got status=%d body=%s", w.Code, w.Body.String())
	}

	// 发布时 revision 过期也返回 409。
	body, _ = json.Marshal(map[string]any{"sourceId": "src1", "revision": before.Revision, "items": []map[string]any{{"mid": 101}}})
	c, w = testutil.NewJSONContext(http.MethodPut, "/api/manage/banners/deck", string(body))
	h.BannerDeck(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("stale deck want 409, got status=%d body=%s", w.Code, w.Body.String())
	}

	// 一键排片参数不合法时返回业务错误。
	c, w = testutil.NewJSONContext(http.MethodPost, "/api/manage/banners/generate", `{"sourceId":"src1","strategy":"hot","quotas":[]}`)
	h.BannerGenerate(c)
	if resp := testutil.DecodeResponse(t, w); w.Code != http.StatusOK || resp.Code != dto.FAILED {
		t.Fatalf("invalid generate want FAILED, got status=%d body=%s", w.Code, w.Body.String())
	}

	// 删除
	c, w = testutil.NewTestContext(http.MethodDelete, "/api/manage/banners/"+id)
	c.Params = gin.Params{{Key: "id", Value: id}}
	h.BannerDelete(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.SUCCESS {
		t.Fatalf("delete code=%d msg=%s", resp.Code, resp.Msg)
	}
	if len(list().Items) != 0 {
		t.Fatalf("banner should be deleted")
	}

	// 切换主站后，非主站轮播仍可通过显式 sourceId 管理，但传错 sourceId 时拒绝。
	if err := sourcecatalog.SetPrimarySourceID("src2"); err != nil {
		t.Fatal(err)
	}
	legacy := model.BannerSlot{Id: "legacy-src1", SourceId: "src1", Mid: 101, ImageMode: model.BannerImageFollow, Sort: 1}
	if err := db.Mdb.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	c, w = testutil.NewJSONContext(http.MethodPatch, "/api/manage/banners/legacy-src1?sourceId=src2", `{"blurb":"x"}`)
	c.Params = gin.Params{{Key: "id", Value: "legacy-src1"}}
	h.BannerUpdate(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.FAILED {
		t.Fatalf("cross-source patch want FAILED, got status=%d body=%s", w.Code, w.Body.String())
	}
	c, w = testutil.NewJSONContext(http.MethodPatch, "/api/manage/banners/legacy-src1?sourceId=src1", `{"blurb":"x"}`)
	c.Params = gin.Params{{Key: "id", Value: "legacy-src1"}}
	h.BannerUpdate(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.SUCCESS {
		t.Fatalf("source-scoped patch want SUCCESS, got status=%d body=%s", w.Code, w.Body.String())
	}
	c, w = testutil.NewTestContext(http.MethodDelete, "/api/manage/banners/legacy-src1?sourceId=src2")
	c.Params = gin.Params{{Key: "id", Value: "legacy-src1"}}
	h.BannerDelete(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.FAILED {
		t.Fatalf("cross-source delete want FAILED, got status=%d body=%s", w.Code, w.Body.String())
	}
	c, w = testutil.NewTestContext(http.MethodDelete, "/api/manage/banners/legacy-src1?sourceId=src1")
	c.Params = gin.Params{{Key: "id", Value: "legacy-src1"}}
	h.BannerDelete(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.SUCCESS {
		t.Fatalf("source-scoped delete want SUCCESS, got status=%d body=%s", w.Code, w.Body.String())
	}
}
