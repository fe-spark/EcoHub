package access

import (
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("setup test db error: %v", err)
	}
	if err := gdb.AutoMigrate(&model.ApiAccessLog{}); err != nil {
		t.Fatalf("migrate test db error: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	resetTodayStatsCache()
	tableMu.Lock()
	tableMigrated = false
	tableMu.Unlock()
	t.Cleanup(func() {
		db.Mdb = prev
		resetTodayStatsCache()
	})
	return gdb
}

func TestApiLogger_PruneExpiredApiLogs(t *testing.T) {
	gdb := setupTestDB(t)

	// 插入 1 条 10 天前数据，1 条 1 天前数据
	oldLog := model.ApiAccessLog{
		CreatedAt:  time.Now().AddDate(0, 0, -10),
		Method:     "GET",
		Path:       "/api/old",
		Status:     200,
		DurationMs: 15,
	}
	recentLog := model.ApiAccessLog{
		CreatedAt:  time.Now().AddDate(0, 0, -1),
		Method:     "GET",
		Path:       "/api/recent",
		Status:     200,
		DurationMs: 25,
	}
	if err := gdb.Create(&oldLog).Error; err != nil {
		t.Fatalf("create old log: %v", err)
	}
	if err := gdb.Create(&recentLog).Error; err != nil {
		t.Fatalf("create recent log: %v", err)
	}

	deleted, err := PruneExpiredApiLogs(7)
	if err != nil {
		t.Fatalf("PruneExpiredApiLogs failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted log, got %d", deleted)
	}

	var remaining []model.ApiAccessLog
	gdb.Find(&remaining)
	if len(remaining) != 1 || remaining[0].Path != "/api/recent" {
		t.Errorf("expected only recent log remaining, got %v", remaining)
	}
}

func TestApiLogger_QueryApiAccessLogs(t *testing.T) {
	gdb := setupTestDB(t)

	now := time.Now()
	logs := []model.ApiAccessLog{
		{CreatedAt: now, Method: "GET", Path: "/api/film/detail", Status: 200, DurationMs: 50, IP: "127.0.0.1"},
		{CreatedAt: now, Method: "POST", Path: "/api/user/login", Status: 401, DurationMs: 120, IP: "192.168.1.1"},
		{CreatedAt: now, Method: "GET", Path: "/api/film/slow", Status: 500, DurationMs: 600, IP: "10.0.0.1"},
	}
	for i := range logs {
		_ = gdb.Create(&logs[i])
	}

	// 1. 全部查询
	res, err := QueryApiAccessLogs(ApiLogQueryParams{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("QueryApiAccessLogs failed: %v", err)
	}
	if res.Total != 3 {
		t.Errorf("expected 3 items, got %d", res.Total)
	}
	if res.ErrorToday != 2 { // 401 and 500
		t.Errorf("expected 2 errors, got %d", res.ErrorToday)
	}
	if res.SlowToday != 1 { // 600ms
		t.Errorf("expected 1 slow, got %d", res.SlowToday)
	}

	// 2. 仅查慢接口
	slowRes, err := QueryApiAccessLogs(ApiLogQueryParams{Duration: "slow"})
	if err != nil {
		t.Fatalf("Query slow failed: %v", err)
	}
	if slowRes.Total != 1 || slowRes.List[0].Path != "/api/film/slow" {
		t.Errorf("expected 1 slow query result, got %v", slowRes.List)
	}

	// 3. 关键字搜索 IP
	ipRes, err := QueryApiAccessLogs(ApiLogQueryParams{Q: "192.168"})
	if err != nil {
		t.Fatalf("Query ip failed: %v", err)
	}
	if ipRes.Total != 1 || ipRes.List[0].Path != "/api/user/login" {
		t.Errorf("expected 1 ip search result, got %v", ipRes.List)
	}

	didLog := model.ApiAccessLog{
		CreatedAt:  now,
		Method:     "GET",
		Path:       "/api/film/play",
		Status:     200,
		DurationMs: 40,
		IP:         "10.0.0.8",
		DeviceId:   "and_abc123",
	}
	_ = gdb.Create(&didLog)
	didRes, err := QueryApiAccessLogs(ApiLogQueryParams{Q: "and_abc123"})
	if err != nil {
		t.Fatalf("Query device id failed: %v", err)
	}
	if didRes.Total != 1 || didRes.List[0].DeviceId != "and_abc123" {
		t.Errorf("expected 1 device id search result, got %+v", didRes.List)
	}

	// 4. 非法日期参数保护，优雅回退默认近 3 天
	invalidDayRes, err := QueryApiAccessLogs(ApiLogQueryParams{Day: "invalid-date"})
	if err != nil {
		t.Fatalf("Query with invalid day should not fail: %v", err)
	}
	if invalidDayRes.Total != 4 {
		t.Errorf("expected fallback 4 items, got %d", invalidDayRes.Total)
	}

	// 5. LIKE 通配符匹配转义测试（防止 % 或 _ 作为通配符扫全量）
	specialLog := model.ApiAccessLog{
		CreatedAt:  now,
		Method:     "GET",
		Path:       "/api/film_special%item",
		Status:     200,
		DurationMs: 10,
		IP:         "10.0.0.2",
	}
	_ = gdb.Create(&specialLog)
	qRes, err := QueryApiAccessLogs(ApiLogQueryParams{Q: "film_special%"})
	if err != nil {
		t.Fatalf("Query with special like characters failed: %v", err)
	}
	if qRes.Total != 1 || qRes.List[0].Path != "/api/film_special%item" {
		t.Errorf("expected 1 exact matched item for film_special%%, got %d", qRes.Total)
	}
}

func TestApiLogger_WorkerDrain(t *testing.T) {
	gdb := setupTestDB(t)

	// 测试压入队列后正常被消费落库
	testLog := &model.ApiAccessLog{
		CreatedAt:  time.Now(),
		Method:     "POST",
		Path:       "/api/worker/drain",
		Status:     200,
		DurationMs: 30,
		IP:         "127.0.0.1",
	}
	EnqueueApiAccessLog(testLog)

	// 等待定时器超时刷盘或者调用一次 flush
	time.Sleep(ApiLogFlushInterval + 200*time.Millisecond)

	var count int64
	gdb.Model(&model.ApiAccessLog{}).Where("path = ?", "/api/worker/drain").Count(&count)
	if count < 1 {
		t.Errorf("expected log to be flushed to DB, got %d", count)
	}
}
