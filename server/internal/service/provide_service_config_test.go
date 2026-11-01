package service

import (
	"strings"
	"testing"

	"server/internal/model"
)

func TestProvideService_GetTVBoxConfig_PrimarySites(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	if err := gdb.Create(&[]model.FilmSource{
		{Id: "src_b", Name: "乙站", Uri: "https://b.example/api", State: true, Sort: 2},
		{Id: "src_off", Name: "停用站", Uri: "https://off.example/api", State: false, Sort: 1},
		{Id: "src_a", Name: "甲站", Uri: "https://a.example/api", State: true, Sort: 0},
	}).Error; err != nil {
		t.Fatalf("sources: %v", err)
	}

	res, err := ProvideSvc.GetTVBoxConfig("http://127.0.0.1:8080", "")
	if err != nil {
		t.Fatalf("GetTVBoxConfig 失败: %v", err)
	}
	sites, ok := res["sites"].([]map[string]any)
	if !ok || len(sites) != 2 {
		t.Fatalf("应只有两个启用站，实际: %+v", res["sites"])
	}
	if sites[0]["key"] != "source_src_a" || sites[0]["name"] != "📡 甲站" {
		t.Fatalf("首位应为主站，实际: %+v", sites[0])
	}
	api, _ := sites[0]["api"].(string)
	if !strings.Contains(api, "/api/provide/vod") || !strings.Contains(api, "source=src_a") {
		t.Fatalf("主站 api = %v", api)
	}
	if sites[1]["key"] != "source_src_b" {
		t.Fatalf("第二站 = %+v", sites[1])
	}
	for _, site := range sites {
		name, _ := site["name"].(string)
		if strings.Contains(name, "全量") {
			t.Fatalf("不应再有全量站点: %s", name)
		}
	}

	withKey, err := ProvideSvc.GetTVBoxConfig("https://movie.example.com/", "mysecretkey")
	if err != nil {
		t.Fatalf("GetTVBoxConfig 失败: %v", err)
	}
	keyed, _ := withKey["sites"].([]map[string]any)
	keyedAPI, _ := keyed[0]["api"].(string)
	if !strings.Contains(keyedAPI, "key=mysecretkey") || !strings.Contains(keyedAPI, "source=src_a") {
		t.Fatalf("api 应携带 key 和主站 source，实际: %v", keyedAPI)
	}
}

func TestProvideService_GetAppSourceConfig(t *testing.T) {
	setupFilmServiceTestDB(t)
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
