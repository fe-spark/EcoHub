package mapping

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func init() { gin.SetMode(gin.TestMode) }

func setupManageConfigDB(t *testing.T) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.SourceCategory{}, &model.MappingRule{}, &model.Category{}, &model.CategoryMapping{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
}

func testJSONContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func testContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	return c, w
}

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) dto.Response {
	t.Helper()
	var resp dto.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	return resp
}

func TestMappingRule_SaveAndDelete_BatchContract(t *testing.T) {
	setupManageConfigDB(t)
	h := &MappingHandler{}
	var (
		c    *gin.Context
		w    *httptest.ResponseRecorder
		resp dto.Response
	)

	// 3. MappingRuleSave (新增，ID=0)
	ruleAddBody := `{"raw":"大陆","target":"中国大陆","matchType":"exact","group":"Area"}`
	c, w = testJSONContext(http.MethodPost, "/api/manage/mapping/rule/save", ruleAddBody)
	h.MappingRuleSave(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected MappingRuleSave success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	// 4. MappingRuleDelete 批量删除
	delRuleBody, _ := json.Marshal(map[string][]uint{"ids": {1}})
	c, w = testJSONContext(http.MethodPost, "/api/manage/mapping/rule/delete", string(delRuleBody))
	h.MappingRuleDelete(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected MappingRuleDelete success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	// 5. 校验：非 Filter 分组不允许空 target；非分类分组不允许 regex；Filter 允许空 target
	c, w = testJSONContext(http.MethodPost, "/api/manage/mapping/rule/save", `{"sourceId":"s1","raw":"电影","target":"","matchType":"exact","group":"CategoryRoot"}`)
	h.MappingRuleSave(c)
	if resp = decodeResponse(t, w); resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty target for CategoryRoot, got code=%d", resp.Code)
	}
	c, w = testJSONContext(http.MethodPost, "/api/manage/mapping/rule/save", `{"raw":"中国.*","target":"中国","matchType":"regex","group":"Area"}`)
	h.MappingRuleSave(c)
	if resp = decodeResponse(t, w); resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on regex for Area group, got code=%d", resp.Code)
	}
	c, w = testJSONContext(http.MethodPost, "/api/manage/mapping/rule/save", `{"raw":"忽略词","target":"","matchType":"exact","group":"Filter"}`)
	h.MappingRuleSave(c)
	if resp = decodeResponse(t, w); resp.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS on Filter rule with empty target, got code=%d msg=%s", resp.Code, resp.Msg)
	}
}

func TestMappingRule_ListCategoryAllIncludesRootAndSub(t *testing.T) {
	setupManageConfigDB(t)
	gdb := db.Mdb
	rules := []model.MappingRule{
		{SourceId: "src_cat", Group: "CategoryRoot", Raw: "电影", Target: "电影", MatchType: "exact"},
		{SourceId: "src_cat", Group: "CategorySub", Raw: "动作片", Target: "动作", MatchType: "exact"},
		{SourceId: "src_other", Group: "CategorySub", Raw: "他站子类", Target: "动作", MatchType: "exact"},
		{SourceId: "", Group: "Area", Raw: "中国大陆", Target: "大陆", MatchType: "exact"},
	}
	if err := gdb.Create(&rules).Error; err != nil {
		t.Fatalf("seed rules: %v", err)
	}

	h := &MappingHandler{}
	c, w := testContext(http.MethodGet, "/api/manage/mapping/rule/list?group=CategoryAll&sourceId=src_cat")
	h.MappingRuleList(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("MappingRuleList failed: %+v", resp)
	}
	raw, _ := json.Marshal(resp.Data)
	var data struct {
		List   []model.MappingRule `json:"list"`
		Totals map[string]int      `json:"totals"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if len(data.List) != 2 || data.List[0].Group != "CategoryRoot" || data.List[1].Group != "CategorySub" {
		t.Fatalf("CategoryAll list = %+v, want root + sub for src_cat only", data.List)
	}
	if data.Totals["CategoryRoot"] != 1 || data.Totals["CategorySub"] != 1 {
		t.Fatalf("CategoryAll totals = %+v, want 1/1", data.Totals)
	}
}

func TestMappingRule_ContextReturnsSourceCategoriesAndRules(t *testing.T) {
	setupManageConfigDB(t)
	gdb := db.Mdb
	if err := gdb.AutoMigrate(&model.SourceCategory{}, &model.Category{}, &model.CategoryMapping{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src_ctx", Name: "上下文站", Uri: "http://ctx.test", State: true}).Error; err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if err := gdb.Create(&model.SourceCategory{SourceId: "src_ctx", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0}).Error; err != nil {
		t.Fatalf("seed source category: %v", err)
	}
	if err := gdb.Create(&model.MappingRule{SourceId: "src_ctx", Group: "CategoryRoot", Raw: "电影", Target: "电影", MatchType: "exact"}).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	h := &MappingHandler{}
	c, w := testContext(http.MethodGet, "/api/manage/mapping/rule/context?sourceId=src_ctx")
	h.MappingRuleContext(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("MappingRuleContext failed: %+v", resp)
	}
	raw, _ := json.Marshal(resp.Data)
	var data struct {
		SourceName    string `json:"sourceName"`
		RawCategories []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"rawCategories"`
		Rules []model.MappingRule `json:"rules"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal context: %v", err)
	}
	if data.SourceName != "上下文站" || len(data.RawCategories) != 1 || len(data.Rules) != 1 {
		t.Fatalf("unexpected context data: %+v", data)
	}
}
