package mediaproxy

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
)

func TestMediaProxyRuntime_ErrorDoesNotPoisonCache(t *testing.T) {
	prevDB := db.Mdb
	prevRdb := db.Rdb
	db.Mdb = nil
	db.Rdb = nil
	InvalidateMediaProxyMemoryCache()
	t.Cleanup(func() {
		db.Mdb = prevDB
		db.Rdb = prevRdb
		InvalidateMediaProxyMemoryCache()
	})

	// 数据库不可用时返回兜底默认配置，但不允许写入 cachedRuntime
	rt := getActiveRuntime()
	if rt.config != model.DefaultMediaProxyConfig() {
		t.Fatalf("expected fallback default config, got %+v", rt.config)
	}
	if cachedRuntime.Load() != nil {
		t.Fatal("expected failed GetSiteBasic not to poison cachedRuntime")
	}
}
