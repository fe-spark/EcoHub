package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/model"
	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

func TestClampSearchFilmPageSize(t *testing.T) {
	if got := clampSearchFilmPageSize(20, false); got != 12 {
		t.Fatalf("unspecified should default to 12, got %d", got)
	}
	if got := clampSearchFilmPageSize(0, true); got != 12 {
		t.Fatalf("invalid specified size should default to 12, got %d", got)
	}
	if got := clampSearchFilmPageSize(24, true); got != 24 {
		t.Fatalf("explicit size should be kept, got %d", got)
	}
	if got := clampSearchFilmPageSize(500, true); got != 50 {
		t.Fatalf("oversize should cap at 50, got %d", got)
	}
}

func TestHasSearchOptions(t *testing.T) {
	if hasSearchOptions(nil) {
		t.Fatal("nil should be false")
	}
	if hasSearchOptions(map[string]any{}) {
		t.Fatal("empty should be false")
	}
	if hasSearchOptions(map[string]any{"sortList": []string{"Sort"}, "tags": map[string]any{}}) {
		t.Fatal("sortList without tags should be false")
	}
	if hasSearchOptions(map[string]any{
		"tags": map[string]any{
			"Category": []map[string]string{{"Name": "全部", "Value": ""}},
		},
	}) {
		t.Fatal("Category 仅全部 should be false")
	}

	sortOnly := map[string]any{
		"sortList": []string{"Sort"},
		"tags": map[string]any{
			"Sort": []map[string]string{
				{"Name": "最近更新", "Value": "update_stamp"},
			},
		},
	}
	if !hasSearchOptions(sortOnly) {
		t.Fatal("仅 Sort 应展示筛选面板")
	}

	jsonLike := map[string]any{
		"sortList": []any{"Sort"},
		"tags": map[string]any{
			"Sort": []any{map[string]any{"Name": "最近更新", "Value": "update_stamp"}},
		},
	}
	if !hasSearchOptions(jsonLike) {
		t.Fatal("JSON 反序列化后的仅 Sort 应展示筛选面板")
	}

	if !hasSearchOptions(map[string]any{
		"tags": map[string]any{
			"Plot": []map[string]string{{"Name": "甜宠", "Value": "甜宠"}},
		},
	}) {
		t.Fatal("真实 Plot 标签应展示筛选面板")
	}
}

func TestParseOptionalQueryInt(t *testing.T) {
	if v, ok := parseOptionalQueryInt(""); !ok || v != 0 {
		t.Fatalf("empty should be 0, got %d ok=%v", v, ok)
	}
	if v, ok := parseOptionalQueryInt("12"); !ok || v != 12 {
		t.Fatalf("12 should parse, got %d ok=%v", v, ok)
	}
	if _, ok := parseOptionalQueryInt("abc"); ok {
		t.Fatal("invalid should fail")
	}
}

func TestHasPlayableFilmDetail(t *testing.T) {
	if hasPlayableFilmDetail(8, model.MovieDetailVo{}) {
		t.Fatal("local missing mid should fail")
	}
	if !hasPlayableFilmDetail(8, model.MovieDetailVo{MovieDetail: model.MovieDetail{Id: 8}}) {
		t.Fatal("local mid should pass")
	}
	live := model.MovieDetailVo{
		MovieDetail: model.MovieDetail{Name: "仙逆"},
		List:        []model.PlayLinkVo{{Id: "src", LinkList: []model.MovieUrlInfo{{Episode: "1", Link: "http://x"}}}},
	}
	if !hasPlayableFilmDetail(0, live) {
		t.Fatal("live detail should pass")
	}
	if hasPlayableFilmDetail(0, model.MovieDetailVo{MovieDetail: model.MovieDetail{Name: "仙逆"}}) {
		t.Fatal("live without playlist should fail")
	}
}

func TestParseOptionalQueryInt64(t *testing.T) {
	if v, ok := parseOptionalQueryInt64(""); !ok || v != 0 {
		t.Fatalf("empty should be 0, got %d ok=%v", v, ok)
	}
	if v, ok := parseOptionalQueryInt64("1002"); !ok || v != 1002 {
		t.Fatalf("1002 should parse, got %d ok=%v", v, ok)
	}
	if _, ok := parseOptionalQueryInt64("xyz"); ok {
		t.Fatal("invalid should fail")
	}
}

func TestResolvePlayableSourceID(t *testing.T) {
	sources := []model.PlayLinkVo{
		{Id: "group1", SourceId: "s1", LinkList: []model.MovieUrlInfo{{Episode: "1", Link: "http://1"}}},
		{Id: "group2", SourceId: "s2", LinkList: []model.MovieUrlInfo{{Episode: "1", Link: "http://2"}}},
	}
	if got := resolvePlayableSourceID(sources, "group2"); got != "group2" {
		t.Fatalf("preferred id failed: %s", got)
	}
	if got := resolvePlayableSourceID(sources, "s2"); got != "group2" {
		t.Fatalf("preferred sourceId failed: %s", got)
	}
	if got := resolvePlayableSourceID(sources, "unknown"); got != "group1" {
		t.Fatalf("fallback first failed: %s", got)
	}
}

func TestFilmPlayInfo_ZeroIDsFailFast(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("GET", "/filmPlayInfo?id=0&sid=0", nil)
	c.Request = req

	IndexHd.FilmPlayInfo(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 wrapper, got %d", w.Code)
	}
	var resp dto.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Code != dto.FAILED || resp.Msg != "请求异常,暂无影片信息!!!" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestFilmPlayInfo_NegativeEpisodeAndPlayFromEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("GET", "/filmPlayInfo?id=100&source=src1&playFrom=&episode=-1", nil)
	c.Request = req

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("FilmPlayInfo panicked with negative episode: %v", r)
		}
	}()
	IndexHd.FilmPlayInfo(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 wrapper, got %d", w.Code)
	}
}

func TestLiveFilmPlayInfo_NegativeEpisodeAndPlayFromEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("GET", "/liveFilmPlayInfo?sid=100&source=src1&episode=-1", nil)
	c.Request = req

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LiveFilmPlayInfo panicked with negative episode: %v", r)
		}
	}()
	IndexHd.LiveFilmPlayInfo(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 wrapper, got %d", w.Code)
	}
}
