package service

import (
	"strings"
	"testing"
)

func TestProvideService_GetTVBoxConfig(t *testing.T) {
	res, err := ProvideSvc.GetTVBoxConfig("http://127.0.0.1:8080", "")
	if err != nil {
		t.Fatalf("GetTVBoxConfig 失败: %v", err)
	}

	sites, ok := res["sites"].([]map[string]any)
	if !ok || len(sites) == 0 {
		t.Fatalf("sites 应为非空切片，实际: %+v", res["sites"])
	}

	first := sites[0]
	if first["key"] != "EcoHub" {
		t.Errorf("首个站点 key 应为 EcoHub，实际: %v", first["key"])
	}
	if api, _ := first["api"].(string); !strings.Contains(api, "/api/provide/vod") {
		t.Errorf("api 应包含 /api/provide/vod，实际: %v", api)
	}
}

func TestProvideService_GetTVBoxConfig_WithKey(t *testing.T) {
	res, err := ProvideSvc.GetTVBoxConfig("https://movie.example.com/", "mysecretkey")
	if err != nil {
		t.Fatalf("GetTVBoxConfig 失败: %v", err)
	}

	sites, ok := res["sites"].([]map[string]any)
	if !ok || len(sites) == 0 {
		t.Fatalf("sites 应为非空切片，实际: %+v", res["sites"])
	}

	first := sites[0]
	api, _ := first["api"].(string)
	if !strings.Contains(api, "key=mysecretkey") {
		t.Errorf("api 应携带 key 参数，实际: %v", api)
	}
}

func TestProvideService_GetAppSourceConfig(t *testing.T) {
	res, err := ProvideSvc.GetAppSourceConfig("https://movie.example.com/")
	if err != nil {
		t.Fatalf("GetAppSourceConfig 失败: %v", err)
	}

	if code, _ := res["code"].(int); code != 1 {
		t.Errorf("code 应为 1，实际: %v", code)
	}

	data, ok := res["data"].(map[string]any)
	if !ok {
		t.Fatalf("data 应为 map，实际: %+v", res["data"])
	}

	if status, _ := data["status"].(string); status != "online" {
		t.Errorf("status 应为 online，实际: %v", status)
	}
	if siteURL, _ := data["site_url"].(string); siteURL != "https://movie.example.com" {
		t.Errorf("site_url 应规范去除尾部斜杠，实际: %v", siteURL)
	}
}
