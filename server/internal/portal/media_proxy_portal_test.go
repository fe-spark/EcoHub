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
