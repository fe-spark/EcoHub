package portal

import (
	"encoding/json"
	"server/internal/mediaproxy"
	"strings"
	"testing"

	"server/internal/model"
)

func TestWrapHomeData_DynamicMaps(t *testing.T) {
	mediaproxy.UpdateMediaProxyMemoryCache(model.MediaProxyConfig{Enabled: true})
	defer mediaproxy.InvalidateMediaProxyMemoryCache()

	jsonHome := `{
		"banners": [{"poster": "https://ddmf.net/banner.jpg"}],
		"recommends": [
			{
				"id": "g_cat",
				"kind": "category",
				"title": "电影",
				"items": [{"picture": "https://ddmf.net/movie.jpg"}],
				"hot": [{"picture": "https://ddmf.net/hot.jpg"}]
			}
		]
	}`
	var dynamicHome map[string]any
	if err := json.Unmarshal([]byte(jsonHome), &dynamicHome); err != nil {
		t.Fatalf("json unmarshal home: %v", err)
	}

	wrappedHome := WrapHomeData(dynamicHome)
	bannersAny, ok := wrappedHome["banners"].([]any)
	if !ok || len(bannersAny) == 0 {
		t.Fatalf("expected wrapped banners as []any, got %T", wrappedHome["banners"])
	}
	bannerMap, ok := bannersAny[0].(map[string]any)
	if !ok || !strings.Contains(bannerMap["poster"].(string), ".wp.com/ddmf.net/banner.jpg") {
		t.Errorf("expected wrapped banner poster, got %v", bannerMap["poster"])
	}

	recAny, ok := wrappedHome["recommends"].([]any)
	if !ok || len(recAny) == 0 {
		t.Fatalf("expected wrapped recommends as []any, got %T", wrappedHome["recommends"])
	}
	secMap, ok := recAny[0].(map[string]any)
	if !ok {
		t.Fatalf("expected secMap, got %T", recAny[0])
	}
	moviesAny, ok := secMap["items"].([]any)
	if !ok || len(moviesAny) == 0 {
		t.Fatalf("expected items as []any, got %T", secMap["items"])
	}
	movieMap, ok := moviesAny[0].(map[string]any)
	if !ok || !strings.Contains(movieMap["picture"].(string), ".wp.com/ddmf.net/movie.jpg") {
		t.Errorf("expected wrapped item picture, got %v", movieMap["picture"])
	}
	hotAny, ok := secMap["hot"].([]any)
	if !ok || len(hotAny) == 0 {
		t.Fatalf("expected hot as []any, got %T", secMap["hot"])
	}
	hotMap, ok := hotAny[0].(map[string]any)
	if !ok || !strings.Contains(hotMap["picture"].(string), ".wp.com/ddmf.net/hot.jpg") {
		t.Errorf("expected wrapped hot picture, got %v", hotMap["picture"])
	}
}

func TestWrapHomeData_Recommends(t *testing.T) {
	mediaproxy.UpdateMediaProxyMemoryCache(model.MediaProxyConfig{Enabled: true})
	defer mediaproxy.InvalidateMediaProxyMemoryCache()

	rec := model.HomeRecommendSection{
		Id:    "g1",
		Kind:  "recommend",
		Title: "院线热映",
		Items: []model.HomeRecommendPublicItem{
			{
				MovieBasicInfo: model.MovieBasicInfo{
					Id:      101,
					Name:    "测试片",
					Picture: "https://ddmf.net/rec.jpg",
				},
				Rank: 1,
			},
		},
		Hot: []model.MovieBasicInfo{
			{
				Id:      102,
				Name:    "热播片",
				Picture: "https://ddmf.net/rec-hot.jpg",
			},
		},
	}
	data := map[string]any{
		"recommends": []model.HomeRecommendSection{rec},
	}

	wrapped := WrapHomeData(data)
	recs := wrapped["recommends"].([]model.HomeRecommendSection)
	if !strings.Contains(recs[0].Items[0].Picture, ".wp.com/ddmf.net/rec.jpg") {
		t.Fatalf("expected recommends picture wrapped, got %q", recs[0].Items[0].Picture)
	}
	if !strings.Contains(recs[0].Hot[0].Picture, ".wp.com/ddmf.net/rec-hot.jpg") {
		t.Fatalf("expected recommends hot picture wrapped, got %q", recs[0].Hot[0].Picture)
	}
}

func TestBuildLegacyHomeContent_AndWrap(t *testing.T) {
	mediaproxy.UpdateMediaProxyMemoryCache(model.MediaProxyConfig{Enabled: true})
	defer mediaproxy.InvalidateMediaProxyMemoryCache()

	catNav := &model.CategoryTree{Id: 1, Name: "电影", Show: true}
	sections := []model.HomeRecommendSection{
		{
			Id:    "g_rec",
			Kind:  "recommend",
			Title: "院线热映",
			Items: []model.HomeRecommendPublicItem{
				{MovieBasicInfo: model.MovieBasicInfo{Id: 10, Name: "推荐片", Picture: "https://ddmf.net/r.jpg"}, Rank: 1},
			},
		},
		{
			Id:         "g_cat",
			Kind:       "category",
			Title:      "电影",
			CategoryId: 1,
			Nav:        catNav,
			Items: []model.HomeRecommendPublicItem{
				{MovieBasicInfo: model.MovieBasicInfo{Id: 11, Name: "分类片", Picture: "https://ddmf.net/c.jpg"}, Rank: 1},
			},
			Hot: []model.MovieBasicInfo{
				{Id: 12, Name: "热播片", Picture: "https://ddmf.net/h.jpg"},
			},
		},
	}

	content := buildLegacyHomeContent(sections)
	if len(content) != 1 {
		t.Fatalf("expected 1 legacy content section, got %d", len(content))
	}
	wrapped := WrapHomeData(map[string]any{"content": content})
	wrappedList := wrapped["content"].([]map[string]any)
	movies := wrappedList[0]["movies"].([]model.MovieBasicInfo)
	hot := wrappedList[0]["hot"].([]model.MovieBasicInfo)
	if !strings.Contains(movies[0].Picture, ".wp.com/ddmf.net/c.jpg") {
		t.Fatalf("expected legacy content movie picture wrapped, got %q", movies[0].Picture)
	}
	if !strings.Contains(hot[0].Picture, ".wp.com/ddmf.net/h.jpg") {
		t.Fatalf("expected legacy content hot picture wrapped, got %q", hot[0].Picture)
	}

	// 同时验证 Redis 缓存反序列化后的弱类型 []any 路径
	rawBytes, err := json.Marshal(map[string]any{"content": content})
	if err != nil {
		t.Fatalf("marshal legacy content: %v", err)
	}
	var cached map[string]any
	if err := json.Unmarshal(rawBytes, &cached); err != nil {
		t.Fatalf("unmarshal legacy content: %v", err)
	}
	wrappedCached := WrapHomeData(cached)
	cachedList, ok := wrappedCached["content"].([]any)
	if !ok || len(cachedList) != 1 {
		t.Fatalf("expected wrapped cached content []any, got %T", wrappedCached["content"])
	}
	secMap := cachedList[0].(map[string]any)
	cachedMovies := secMap["movies"].([]any)
	cachedHot := secMap["hot"].([]any)
	if !strings.Contains(cachedMovies[0].(map[string]any)["picture"].(string), ".wp.com/ddmf.net/c.jpg") {
		t.Fatalf("expected cached legacy content movie picture wrapped, got %v", cachedMovies[0])
	}
	if !strings.Contains(cachedHot[0].(map[string]any)["picture"].(string), ".wp.com/ddmf.net/h.jpg") {
		t.Fatalf("expected cached legacy content hot picture wrapped, got %v", cachedHot[0])
	}
}
