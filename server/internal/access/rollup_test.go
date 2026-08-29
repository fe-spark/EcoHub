package access

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDaysToRoll(t *testing.T) {
	loc := time.Local
	cutoff := time.Date(2026, 8, 16, 0, 0, 0, 0, loc)
	yesterday := time.Date(2026, 8, 28, 0, 0, 0, 0, loc)

	none := daysToRoll(yesterday, yesterday, cutoff)
	if len(none) != 0 {
		t.Fatalf("already rolled through yesterday: %v", none)
	}

	fromZero := daysToRoll(cutoff.AddDate(0, 0, -1), yesterday, cutoff)
	if len(fromZero) != 13 {
		t.Fatalf("want 13 closed days in 14-day window, got %d", len(fromZero))
	}
	if !fromZero[0].Equal(cutoff) || !fromZero[len(fromZero)-1].Equal(yesterday) {
		t.Fatalf("range %v .. %v", fromZero[0], fromZero[len(fromZero)-1])
	}

	stale := daysToRoll(time.Date(2026, 1, 1, 0, 0, 0, 0, loc), yesterday, cutoff)
	if !stale[0].Equal(cutoff) {
		t.Fatalf("stale watermark should clamp to cutoff, got %v", stale[0])
	}
}

func setupAccessDailyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.AccessDailyStats{}, &model.AccessDailyTop{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
	return gdb
}

func TestParseRolledDay(t *testing.T) {
	cutoff := time.Date(2026, 8, 16, 0, 0, 0, 0, time.Local)
	missing, err := parseRolledDay("", redis.Nil, cutoff)
	if err != nil || !missing.Equal(cutoff.AddDate(0, 0, -1)) {
		t.Fatalf("missing watermark: %v %v", missing, err)
	}
	if _, err := parseRolledDay("", errors.New("timeout"), cutoff); err == nil {
		t.Fatal("redis errors must abort rollup, not skip watermark")
	}
	got, err := parseRolledDay("2026-08-20", nil, cutoff)
	if err != nil || got.Format("2006-01-02") != "2026-08-20" {
		t.Fatalf("parsed watermark: %v %v", got, err)
	}
	bad, err := parseRolledDay("not-a-day", nil, cutoff)
	if err != nil || !bad.Equal(cutoff.AddDate(0, 0, -1)) {
		t.Fatalf("corrupt watermark: %v %v", bad, err)
	}
}

func TestPersistDailyUpsertAndPrune(t *testing.T) {
	setupAccessDailyTestDB(t)
	stats := model.AccessDailyStats{
		Day:        "2026-08-20",
		PV:         10,
		UV:         3,
		ClientJSON: `{"web":10}`,
		ActionJSON: `{"browse":10}`,
		HistJSON:   `{}`,
		RolledAt:   time.Now(),
	}
	tops := []model.AccessDailyTop{
		{Day: "2026-08-20", Kind: "search", Rank: 1, ItemKey: "流浪地球", Count: 4},
	}
	if err := persistDaily(stats, tops); err != nil {
		t.Fatalf("persist: %v", err)
	}
	stats.PV = 22
	if err := persistDaily(stats, tops); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	row, ok := loadDailyStats("2026-08-20")
	if !ok || row.PV != 22 {
		t.Fatalf("upsert pv want 22 got ok=%v pv=%d", ok, row.PV)
	}
	items := loadDailyTops("2026-08-20", "search", 10)
	if len(items) != 1 || items[0].Key != "流浪地球" {
		t.Fatalf("tops: %+v", items)
	}

	old := model.AccessDailyStats{Day: "2026-07-01", PV: 1, ClientJSON: "{}", ActionJSON: "{}", HistJSON: "{}", RolledAt: time.Now()}
	if err := persistDaily(old, nil); err != nil {
		t.Fatalf("old: %v", err)
	}
	cutoff := time.Date(2026, 8, 16, 0, 0, 0, 0, time.Local)
	if err := pruneDaily(cutoff); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, ok := loadDailyStats("2026-07-01"); ok {
		t.Fatal("pruned day still present")
	}
	if _, ok := loadDailyStats("2026-08-20"); !ok {
		t.Fatal("in-window day should remain")
	}
}

func TestQueryOverviewReadsDailyForPastDay(t *testing.T) {
	setupAccessDailyTestDB(t)
	yesterday := startOfLocalDay(time.Now().In(time.Local)).AddDate(0, 0, -1)
	day := yesterday.Format("2006-01-02")
	if err := persistDaily(model.AccessDailyStats{
		Day:        day,
		PV:         88,
		UV:         9,
		Err4:       1,
		ProvidePV:  3,
		ClientJSON: `{"web":88}`,
		ActionJSON: `{"play":8}`,
		HistJSON:   `{"b50":8}`,
		RolledAt:   time.Now(),
	}, nil); err != nil {
		t.Fatalf("persist: %v", err)
	}
	ov, err := QueryOverview(day)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if ov.PV != 88 || ov.UV != 9 || ov.Provide.PV != 3 {
		t.Fatalf("overview %+v", ov)
	}
	if ov.Client["web"] != 88 || ov.Action["play"] != 8 {
		t.Fatalf("maps %+v %+v", ov.Client, ov.Action)
	}
	if len(ov.Series) != 0 {
		t.Fatalf("legacy daily row without series JSON should not invent points, got %d", len(ov.Series))
	}
}

func TestQueryOverviewReadsPersistedSeries(t *testing.T) {
	setupAccessDailyTestDB(t)
	yesterday := startOfLocalDay(time.Now().In(time.Local)).AddDate(0, 0, -1)
	day := yesterday.Format("2006-01-02")
	if err := persistDaily(model.AccessDailyStats{
		Day:        day,
		PV:         12,
		ClientJSON: `{"web":12}`,
		ActionJSON: `{"browse":12}`,
		HistJSON:   `{}`,
		SeriesJSON: `[{"t":"2026-08-29T00:00:00+08:00","pv":4,"providePv":1},{"t":"2026-08-29T00:15:00+08:00","pv":8}]`,
		RolledAt:   time.Now(),
	}, nil); err != nil {
		t.Fatalf("persist: %v", err)
	}
	ov, err := QueryOverview(day)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if ov.PV != 12 || len(ov.Series) != 2 || ov.Series[0].PV != 4 || ov.Series[1].PV != 8 {
		t.Fatalf("persisted series %+v", ov)
	}
}

func TestQueryPastDayWithoutRedisReturnsEmpty(t *testing.T) {
	setupAccessDailyTestDB(t)
	prevRdb := db.Rdb
	db.Rdb = nil
	t.Cleanup(func() { db.Rdb = prevRdb })

	yesterday := startOfLocalDay(time.Now().In(time.Local)).AddDate(0, 0, -1)
	day := yesterday.Format("2006-01-02")
	ov, err := QueryOverview(day)
	if err != nil {
		t.Fatalf("historical overview must not require redis: %v", err)
	}
	if ov.Day != day || ov.PV != 0 {
		t.Fatalf("empty overview: %+v", ov)
	}

	if err := persistDaily(model.AccessDailyStats{
		Day: day, PV: 1, ClientJSON: "{}", ActionJSON: "{}", HistJSON: "{}", RolledAt: time.Now(),
	}, nil); err != nil {
		t.Fatalf("persist: %v", err)
	}
	items, err := QueryTops(day, "search", 10)
	if err != nil {
		t.Fatalf("historical tops must use daily row even if empty: %v", err)
	}
	if items == nil {
		t.Fatal("tops should be empty slice, not nil error")
	}
	if len(items) != 0 {
		t.Fatalf("want empty search tops, got %+v", items)
	}
}
