package service

import (
	"encoding/json"
	"strings"
	"testing"

	"server/internal/model"
)

func TestWrapMediaURL(t *testing.T) {
	// 确保测试隔离
	UpdateMediaProxyMemoryCache(model.MediaProxyConfig{Enabled: true})
	defer InvalidateMediaProxyMemoryCache()

	t.Run("wrap public remote image url with default photon cdn", func(t *testing.T) {
		raw := "https://ddmf.net/upload/vod/20260912-1/fa7791be8d2ab1ac1e604a5cc9a8de6e.png"
		got := WrapMediaURL(raw)
		if !strings.HasPrefix(got, "https://i") || !strings.Contains(got, ".wp.com/ddmf.net/upload/vod/20260912-1/fa7791be8d2ab1ac1e604a5cc9a8de6e.png") {
			t.Errorf("unexpected wrapped url: %s", got)
		}
	})

	t.Run("wrap with custom proxy template", func(t *testing.T) {
		UpdateMediaProxyMemoryCache(model.MediaProxyConfig{
			Enabled:  true,
			Template: "https://my-worker.workers.dev/{url}",
		})
		defer UpdateMediaProxyMemoryCache(model.MediaProxyConfig{Enabled: true})

		raw := "https://ddmf.net/upload/vod/test.png"
		got := WrapMediaURL(raw)
		expected := "https://my-worker.workers.dev/ddmf.net/upload/vod/test.png"
		if got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("disabled proxy returns original url", func(t *testing.T) {
		UpdateMediaProxyMemoryCache(model.MediaProxyConfig{Enabled: false})
		defer UpdateMediaProxyMemoryCache(model.MediaProxyConfig{Enabled: true})

		raw := "https://ddmf.net/upload/vod/test.png"
		if got := WrapMediaURL(raw); got != raw {
			t.Errorf("expected original url %q, got %q", raw, got)
		}
	})

	t.Run("private LAN hosts are not proxied", func(t *testing.T) {
		lanURLs := []string{
			"http://192.168.1.100:8080/pic.jpg",
			"http://10.0.0.1/pic.jpg",
			"http://127.0.0.1:8080/pic.jpg",
			"http://localhost:8080/pic.jpg",
			"http://172.20.10.5:8080/pic.jpg",
		}
		for _, u := range lanURLs {
			if got := WrapMediaURL(u); got != u {
				t.Errorf("private IP should not be proxied: %s -> %s", u, got)
			}
		}
	})

	t.Run("relative path, data uri and empty paths are preserved", func(t *testing.T) {
		if got := WrapMediaURL(""); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
		if got := WrapMediaURL("   "); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
		rel := "/api/upload/pic/123.jpg"
		if got := WrapMediaURL(rel); got != rel {
			t.Errorf("expected relative path unchanged, got %q", got)
		}
		dataURI := "data:image/svg+xml;base64,abc"
		if got := WrapMediaURL(dataURI); got != dataURI {
			t.Errorf("expected data uri unchanged, got %q", got)
		}
	})

	t.Run("wrap composite models", func(t *testing.T) {
		info := model.MovieBasicInfo{
			Picture:      "https://ddmf.net/a.jpg",
			PictureSlide: "https://ddmf.net/b.jpg",
		}
		WrapMovieBasicInfo(&info)
		if !strings.Contains(info.Picture, ".wp.com/ddmf.net/a.jpg") {
			t.Errorf("expected wrapped picture, got %s", info.Picture)
		}
		if !strings.Contains(info.PictureSlide, ".wp.com/ddmf.net/b.jpg") {
			t.Errorf("expected wrapped slide, got %s", info.PictureSlide)
		}

		banners := model.Banners{
			{Poster: "https://ddmf.net/p.jpg", Picture: "https://ddmf.net/c.jpg"},
		}
		wrappedBanners := WrapBanners(banners)
		if !strings.Contains(wrappedBanners[0].Poster, ".wp.com/ddmf.net/p.jpg") {
			t.Errorf("expected wrapped banner poster, got %s", wrappedBanners[0].Poster)
		}
	})

	t.Run("defensive copy prevents server data and memory pollution", func(t *testing.T) {
		origURL := "https://ddmf.net/original_poster.jpg"
		origList := []model.MovieBasicInfo{
			{Id: 100, Picture: origURL},
		}

		wrapped := WrapMovieBasicInfos(origList)
		if !strings.Contains(wrapped[0].Picture, ".wp.com/ddmf.net/original_poster.jpg") {
			t.Errorf("wrapped output must have cdn proxy url, got %s", wrapped[0].Picture)
		}
		// 严苛验证：原始输入切片与服务端底层数据未被原地篡改
		if origList[0].Picture != origURL {
			t.Fatalf("CRITICAL: original input slice was mutated in-place! expected %q, got %q", origURL, origList[0].Picture)
		}

		origBanners := model.Banners{
			{Id: "1", Poster: origURL},
		}
		wrappedB := WrapBanners(origBanners)
		if !strings.Contains(wrappedB[0].Poster, ".wp.com/") {
			t.Errorf("wrapped banner must have cdn proxy url, got %s", wrappedB[0].Poster)
		}
		if origBanners[0].Poster != origURL {
			t.Fatalf("CRITICAL: original banners slice was mutated in-place! expected %q, got %q", origURL, origBanners[0].Poster)
		}
	})

	t.Run("wrap home data and classify with dynamic unmarshaled maps (Redis cache compatibility)", func(t *testing.T) {
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
	})

	t.Run("validate media proxy config", func(t *testing.T) {
		// Valid cases
		if err := ValidateMediaProxyConfig(model.MediaProxyConfig{Enabled: true, Template: ""}); err != nil {
			t.Errorf("empty template should be valid: %v", err)
		}
		if err := ValidateMediaProxyConfig(model.MediaProxyConfig{Enabled: true, Template: "https://wsrv.nl/?url={url}"}); err != nil {
			t.Errorf("https template with {url} should be valid: %v", err)
		}
		if err := ValidateMediaProxyConfig(model.MediaProxyConfig{Enabled: true, Template: "http://my-proxy.com/{hash}/{url}"}); err != nil {
			t.Errorf("http template with {url} should be valid: %v", err)
		}

		// Invalid cases
		if err := ValidateMediaProxyConfig(model.MediaProxyConfig{Enabled: true, Template: "https://wsrv.nl/?url=abc"}); err == nil {
			t.Errorf("template without {url} should fail")
		}
		if err := ValidateMediaProxyConfig(model.MediaProxyConfig{Enabled: true, Template: "ftp://wsrv.nl/{url}"}); err == nil {
			t.Errorf("ftp template should fail")
		}
		if err := ValidateMediaProxyConfig(model.MediaProxyConfig{Enabled: true, Template: "/relative/{url}"}); err == nil {
			t.Errorf("relative template should fail")
		}
	})

	t.Run("runtime site url exclusion without database io", func(t *testing.T) {
		UpdateMediaProxyRuntime(model.MediaProxyConfig{
			Enabled:  true,
			Template: "https://wsrv.nl/?url={url}",
		}, "https://mysite.example.com")

		// 本站图片不走反代
		localPic := "https://mysite.example.com/upload/pic/1.jpg"
		if got := WrapMediaURL(localPic); got != localPic {
			t.Errorf("expected local picture unproxied %q, got %q", localPic, got)
		}

		// 外站图片走反代
		remotePic := "https://ddmf.net/upload/pic/1.jpg"
		if got := WrapMediaURL(remotePic); !strings.HasPrefix(got, "https://wsrv.nl/?url=") {
			t.Errorf("expected remote picture proxied, got %q", got)
		}
	})
}
