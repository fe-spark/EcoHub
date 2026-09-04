package access

import (
	"fmt"
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

	// 6. 显式大跨度时间范围必须收口到 3 天，阻断跨全表深度扫描
	oldLog5d := model.ApiAccessLog{
		CreatedAt:  now.AddDate(0, 0, -5),
		Method:     "GET",
		Path:       "/api/old/5d",
		Status:     200,
		DurationMs: 10,
		IP:         "10.0.0.9",
	}
	_ = gdb.Create(&oldLog5d)
	wideRes, err := QueryApiAccessLogs(ApiLogQueryParams{
		Page:      1,
		PageSize:  10,
		StartTime: now.AddDate(0, 0, -10).Format("2006-01-02 15:04:05"),
		EndTime:   now.Add(time.Minute).Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		t.Fatalf("Query with wide range failed: %v", err)
	}
	if wideRes.Total != 5 { // 近 3 天内的 5 行，5 天前那行必须被窗口排除
		t.Errorf("expected wide-range window clamped to 3 days (5 items), got %d", wideRes.Total)
	}

	// 7. 关键字搜索窗口额外收口到 24 小时
	oldLog2d := model.ApiAccessLog{
		CreatedAt:  now.AddDate(0, 0, -2),
		Method:     "GET",
		Path:       "/api/keyword_old",
		Status:     200,
		DurationMs: 10,
		IP:         "10.9.9.9",
	}
	_ = gdb.Create(&oldLog2d)
	qDayRes, err := QueryApiAccessLogs(ApiLogQueryParams{Q: "keyword_old"})
	if err != nil {
		t.Fatalf("Query keyword with old day failed: %v", err)
	}
	if qDayRes.Total != 0 {
		t.Errorf("expected keyword search clamped to 24h (0 items), got %d", qDayRes.Total)
	}

	// 8. 深分页超过物理上限返回空页，不执行全量排序扫描
	deepRes, err := QueryApiAccessLogs(ApiLogQueryParams{Page: 999999, PageSize: 20})
	if err != nil {
		t.Fatalf("Query deep page failed: %v", err)
	}
	if len(deepRes.List) != 0 {
		t.Errorf("expected deep page beyond cap to return empty list, got %d", len(deepRes.List))
	}
}

func TestApiLogFlushBlocked(t *testing.T) {
	now := time.Now()
	if apiLogFlushBlocked(time.Time{}, now) {
		t.Fatal("zero failUntil must not block")
	}
	if !apiLogFlushBlocked(now.Add(2*time.Second), now) {
		t.Fatal("future failUntil must block hot-path flush")
	}
	if apiLogFlushBlocked(now.Add(-time.Millisecond), now) {
		t.Fatal("expired failUntil must allow flush")
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

func TestCapApiLogBatch(t *testing.T) {
	// 未超上限的批次保持不变
	small := make([]*model.ApiAccessLog, 100)
	for i := range small {
		small[i] = &model.ApiAccessLog{}
	}
	if got := capApiLogBatch(small); len(got) != 100 {
		t.Fatalf("small batch must stay unchanged, got %d", len(got))
	}

	// 超过上限时丢弃最旧，保留最新 ApiLogQueueCapacity 条
	big := make([]*model.ApiAccessLog, ApiLogQueueCapacity+100)
	for i := range big {
		big[i] = &model.ApiAccessLog{Path: fmt.Sprintf("/api/%d", i)}
	}
	got := capApiLogBatch(big)
	if len(got) != ApiLogQueueCapacity {
		t.Fatalf("oversize batch must be capped to %d, got %d", ApiLogQueueCapacity, len(got))
	}
	if got[0].Path != "/api/100" {
		t.Fatalf("expected oldest entries dropped, first got %s", got[0].Path)
	}
	if got[len(got)-1].Path != fmt.Sprintf("/api/%d", ApiLogQueueCapacity+99) {
		t.Fatalf("expected newest kept, last got %s", got[len(got)-1].Path)
	}
}

func TestApiLogger_PruneConcurrentSafety(t *testing.T) {
	setupTestDB(t)

	// 手动获取锁，模拟正在执行的长任务
	pruneMu.Lock()
	defer pruneMu.Unlock()

	// 并发触发修剪应当非阻塞返回 (0, nil)，不发生死锁或争用
	deleted, err := PruneExpiredApiLogs(7)
	if err != nil {
		t.Fatalf("concurrent prune should not error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted when locked, got %d", deleted)
	}
}

func TestApiLogger_QueryApiAccessLogs_Ordering(t *testing.T) {
	gdb := setupTestDB(t)

	t1 := time.Now().Add(-10 * time.Minute)
	t2 := time.Now().Add(-5 * time.Minute)
	t3 := time.Now().Add(-1 * time.Minute)

	logs := []model.ApiAccessLog{
		{CreatedAt: t1, Method: "GET", Path: "/api/t1", Status: 200, DurationMs: 10},
		{CreatedAt: t2, Method: "GET", Path: "/api/t2", Status: 200, DurationMs: 10},
		{CreatedAt: t3, Method: "GET", Path: "/api/t3", Status: 200, DurationMs: 10},
	}
	for i := range logs {
		_ = gdb.Create(&logs[i])
	}

	res, err := QueryApiAccessLogs(ApiLogQueryParams{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("QueryApiAccessLogs failed: %v", err)
	}
	if len(res.List) != 3 {
		t.Fatalf("expected 3 items, got %d", len(res.List))
	}
	// 期望 created_at DESC 降序排列：t3, t2, t1
	if res.List[0].Path != "/api/t3" || res.List[1].Path != "/api/t2" || res.List[2].Path != "/api/t1" {
		t.Fatalf("expected created_at DESC ordering [t3, t2, t1], got [%s, %s, %s]",
			res.List[0].Path, res.List[1].Path, res.List[2].Path)
	}
}
