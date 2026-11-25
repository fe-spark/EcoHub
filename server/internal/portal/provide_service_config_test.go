package portal

import (
	"server/internal/repobase"
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
	if !ok || len(sites) != 1 {
		t.Fatalf("应只有主站，实际: %+v", res["sites"])
	}
	if sites[0]["key"] != "source_src_a" || sites[0]["name"] != "EcoHub" {
		t.Fatalf("未配置站名时应为 EcoHub，实际: %+v", sites[0])
	}
	if err := repobase.SaveSiteBasic(model.BasicConfig{SiteName: "ECOHUB私人影库"}); err != nil {
		t.Fatal(err)
	}
	named, err := ProvideSvc.GetTVBoxConfig("http://127.0.0.1:8080", "")
	if err != nil {
		t.Fatalf("GetTVBoxConfig 失败: %v", err)
	}
	namedSites, _ := named["sites"].([]map[string]any)
	if len(namedSites) != 1 || namedSites[0]["name"] != "ECOHUB私人影库" || namedSites[0]["key"] != "source_src_a" {
		t.Fatalf("应使用网站名称，实际: %+v", namedSites)
	}
	api, _ := sites[0]["api"].(string)
	if !strings.Contains(api, "/api/provide/vod") || strings.Contains(api, "source=") || strings.Contains(api, "b.example") {
		t.Fatalf("主站 api = %v", api)
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
	if !strings.Contains(keyedAPI, "key=mysecretkey") || strings.Contains(keyedAPI, "source=") {
		t.Fatalf("api 应只携带 key，实际: %v", keyedAPI)
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

func TestProvideService_GetVodList_EmptyWhenNoPrimarySource(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatal(err)
	}
	gdb.Create(&model.FilmSource{Id: "src_off", Name: "停用站", Uri: "https://off.example/api", State: false, Sort: 0})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 701, FirstSourceId: "src_off"},
		FilmIndexContent:  model.FilmIndexContent{Name: "停用站泄漏影片"},
	})
	_ = repobase.RefreshCollectSourceCatalog()

	cur, pages, total, list, err := ProvideSvc.GetVodList(0, 0, 1, "停用", 0, "", "", "", "", "", 20)
	if err != nil {
		t.Fatalf("GetVodList failed: %v", err)
	}
	if cur != 1 || pages != 1 || total != 0 || len(list) != 0 {
		t.Fatalf("expected empty list when no primary source, got cur=%d pages=%d total=%d list=%+v", cur, pages, total, list)
	}
}
