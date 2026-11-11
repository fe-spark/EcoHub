package model

import (
	"encoding/json"
	"testing"
)

// 旧配置（数据库、缓存、备份文件）没有 danmaku 字段：弹幕模块为关闭，其他模块原样保留。
func TestProxyModuleScopeUnmarshal_LegacyWithoutDanmakuStaysOff(t *testing.T) {
	var cfg ProxyConfig
	raw := `{"enabled":true,"proxyUrl":"http://127.0.0.1:7890","modules":{"spider":false,"tmdb":true,"notify":false,"upgrade":true}}`
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	want := ProxyModuleScope{Spider: false, TMDB: true, Notify: false, Upgrade: true, Danmaku: false}
	if cfg.Modules != want {
		t.Fatalf("modules = %+v, want %+v", cfg.Modules, want)
	}
}

func TestProxyModuleScopeJSON_DanmakuRoundTrip(t *testing.T) {
	for _, v := range []bool{true, false} {
		b, err := json.Marshal(ProxyModuleScope{Danmaku: v})
		if err != nil {
			t.Fatal(err)
		}
		var probe map[string]bool
		if err := json.Unmarshal(b, &probe); err != nil {
			t.Fatal(err)
		}
		if got, ok := probe["danmaku"]; !ok || got != v {
			t.Fatalf("marshal = %s", b)
		}
		var back ProxyModuleScope
		if err := json.Unmarshal(b, &back); err != nil || back.Danmaku != v {
			t.Fatalf("round trip %v -> %+v (%v)", v, back, err)
		}
	}
}
