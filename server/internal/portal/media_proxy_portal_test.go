package portal

import (
	"server/internal/mediaproxy"
	"encoding/json"
	"strings"
	"testing"

	"server/internal/model"
)

func TestWrapHomeData_DynamicMaps(t *testing.T) {
	mediaproxy.UpdateMediaProxyMemoryCache(model.MediaProxyConfig{Enabled: true})
	defer mediaproxy.InvalidateMediaProxyMemoryCache()

	jsonHome := `{
		"banners": [{"poster": "https://ddmf.net/banner.jpg"}],
		"content": [
			{
				"nav": {"name": "电影"},
				"movies": [{"picture": "https://ddmf.net/movie.jpg"}],
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

	contentAny, ok := wrappedHome["content"].([]any)
	if !ok || len(contentAny) == 0 {
		t.Fatalf("expected wrapped content as []any, got %T", wrappedHome["content"])
	}
	secMap, ok := contentAny[0].(map[string]any)
	if !ok {
		t.Fatalf("expected secMap, got %T", contentAny[0])
	}
	moviesAny, ok := secMap["movies"].([]any)
	if !ok || len(moviesAny) == 0 {
		t.Fatalf("expected movies as []any, got %T", secMap["movies"])
	}
	movieMap, ok := moviesAny[0].(map[string]any)
	if !ok || !strings.Contains(movieMap["picture"].(string), ".wp.com/ddmf.net/movie.jpg") {
		t.Errorf("expected wrapped movie picture, got %v", movieMap["picture"])
	}
}
