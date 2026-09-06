package access

import (
	"context"
	"fmt"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestIsDigits(t *testing.T) {
	if isDigits("") {
		t.Errorf("empty string should not be digits")
	}
	if !isDigits("20260906") {
		t.Errorf("20260906 should be digits")
	}
	if isDigits("2026-09-06") {
		t.Errorf("2026-09-06 should not be pure digits")
	}
	if isDigits("abc") {
		t.Errorf("abc should not be digits")
	}
}

func TestIsAccessKeyOlderThan(t *testing.T) {
	lockKey := rollupLockKey()
	cutoff := "20260905"

	// Lock key must never be treated as older
	if isAccessKeyOlderThan(lockKey, cutoff) {
		t.Errorf("lock key should never be considered older")
	}

	// Meta keys without dates should not be considered older
	if isAccessKeyOlderThan(rolledDayKey(), cutoff) {
		t.Errorf("rolledDayKey should not be deleted on partial retention")
	}
	if isAccessKeyOlderThan(droppedKey(), cutoff) {
		t.Errorf("droppedKey should not be deleted on partial retention")
	}

	// Min keys
	minOld := config.AccessKeyPrefix + "min:202609011234"
	minNew := config.AccessKeyPrefix + "min:202609061234"
	if !isAccessKeyOlderThan(minOld, cutoff) {
		t.Errorf("minOld should be older than %s", cutoff)
	}
	if isAccessKeyOlderThan(minNew, cutoff) {
		t.Errorf("minNew should not be older than %s", cutoff)
	}

	// Day keys
	dayOld := uvKey("20260901")
	dayNew := uvKey("20260906")
	if !isAccessKeyOlderThan(dayOld, cutoff) {
		t.Errorf("dayOld should be older than %s", cutoff)
	}
	if isAccessKeyOlderThan(dayNew, cutoff) {
		t.Errorf("dayNew should not be older than %s", cutoff)
	}

	// App platform keys
	appOld := appPVKey("android", "20260901")
	appNew := appPVKey("android", "20260906")
	if !isAccessKeyOlderThan(appOld, cutoff) {
		t.Errorf("appOld should be older than %s", cutoff)
	}
	if isAccessKeyOlderThan(appNew, cutoff) {
		t.Errorf("appNew should not be older than %s", cutoff)
	}
}

func setupTestRedisAndDB(t *testing.T) (*miniredis.Miniredis, func()) {
	t.Helper()
	setupAccessDailyTestDB(t)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("run miniredis: %v", err)
	}
	prevRdb := db.Rdb
	prevCxt := db.Cxt
	db.Rdb = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Cxt = context.Background()

	cleanup := func() {
		if db.Rdb != nil {
			_ = db.Rdb.Close()
		}
		db.Rdb = prevRdb
		db.Cxt = prevCxt
		mr.Close()
	}
	return mr, cleanup
}

func TestGetAccessDataStats_Empty(t *testing.T) {
	_, cleanup := setupTestRedisAndDB(t)
	defer cleanup()

	stats := GetAccessDataStats()
	if stats.DailyStatsCount != 0 || stats.DailyTopCount != 0 || stats.RedisKeyCount != 0 {
		t.Errorf("expected empty stats, got %+v", stats)
	}
}

func TestGetAccessDataStats_Populated(t *testing.T) {
	_, cleanup := setupTestRedisAndDB(t)
	defer cleanup()

	// Insert into MySQL
	day1 := "2026-09-01"
	day2 := "2026-09-02"
	_ = db.Mdb.Create(&model.AccessDailyStats{
		Day:      day1,
		PV:       100,
		UV:       50,
		RolledAt: time.Now(),
	})
	_ = db.Mdb.Create(&model.AccessDailyStats{
		Day:      day2,
		PV:       200,
		UV:       80,
		RolledAt: time.Now(),
	})
	_ = db.Mdb.Create(&model.AccessDailyTop{
		Day:     day1,
		Kind:    "play",
		Rank:    1,
		ItemKey: "123",
		Count:   10,
	})

	// Add Redis keys
	_ = db.Rdb.Set(db.Cxt, uvKey("20260901"), "val", 0)
	_ = db.Rdb.Set(db.Cxt, uvKey("20260902"), "val", 0)
	// Add lock key - should NOT be counted in RedisKeyCount
	_ = db.Rdb.Set(db.Cxt, rollupLockKey(), "token", 0)

	stats := GetAccessDataStats()
	if stats.DailyStatsCount != 2 {
		t.Errorf("want DailyStatsCount=2, got %d", stats.DailyStatsCount)
	}
	if stats.DailyTopCount != 1 {
		t.Errorf("want DailyTopCount=1, got %d", stats.DailyTopCount)
	}
	if stats.RedisKeyCount != 2 {
		t.Errorf("want RedisKeyCount=2 (excluding lock key), got %d", stats.RedisKeyCount)
	}
	if stats.EarliestDay != day1 || stats.LatestDay != day2 {
		t.Errorf("want %s~%s, got %s~%s", day1, day2, stats.EarliestDay, stats.LatestDay)
	}
	if stats.TotalPV != 300 || stats.TotalUV != 130 {
		t.Errorf("want PV=300 UV=130, got PV=%d UV=%d", stats.TotalPV, stats.TotalUV)
	}
}

func TestGetAccessDataStats_TopOnlyFallback(t *testing.T) {
	_, cleanup := setupTestRedisAndDB(t)
	defer cleanup()

	_ = db.Mdb.Create(&model.AccessDailyTop{
		Day:     "2026-09-03",
		Kind:    "play",
		Rank:    1,
		ItemKey: "123",
		Count:   10,
	})
	_ = db.Mdb.Create(&model.AccessDailyTop{
		Day:     "2026-09-05",
		Kind:    "play",
		Rank:    2,
		ItemKey: "456",
		Count:   5,
	})

	stats := GetAccessDataStats()
	if stats.DailyStatsCount != 0 {
		t.Errorf("want DailyStatsCount=0, got %d", stats.DailyStatsCount)
	}
	if stats.DailyTopCount != 2 {
		t.Errorf("want DailyTopCount=2, got %d", stats.DailyTopCount)
	}
	if stats.EarliestDay != "2026-09-03" || stats.LatestDay != "2026-09-05" {
		t.Errorf("want 2026-09-03~2026-09-05, got %s~%s", stats.EarliestDay, stats.LatestDay)
	}
}

func TestClearAccessData_All(t *testing.T) {
	_, cleanup := setupTestRedisAndDB(t)
	defer cleanup()

	// Populate DB
	_ = db.Mdb.Create(&model.AccessDailyStats{Day: "2026-09-01", PV: 10, RolledAt: time.Now()})
	_ = db.Mdb.Create(&model.AccessDailyTop{Day: "2026-09-01", Kind: "play", Rank: 1, ItemKey: "1"})

	_ = db.Rdb.Set(db.Cxt, uvKey("20260901"), "1", 0)
	_ = db.Rdb.Set(db.Cxt, dayAggKey("20260901"), "1", 0)
	_ = db.Rdb.Set(db.Cxt, rolledDayKey(), "2026-09-01", 0)

	res, err := ClearAccessData(0)
	if err != nil {
		t.Fatalf("ClearAccessData error: %v", err)
	}
	if res.DeletedDailyStats != 1 {
		t.Errorf("expected 1 deleted daily stats, got %d", res.DeletedDailyStats)
	}
	if res.DeletedDailyTop != 1 {
		t.Errorf("expected 1 deleted daily top, got %d", res.DeletedDailyTop)
	}
	if res.DeletedRedisKeys != 3 {
		t.Errorf("expected 3 deleted redis keys, got %d", res.DeletedRedisKeys)
	}

	assertRollupLockReleased(t)

	// Verify DB is empty
	stats := GetAccessDataStats()
	if stats.DailyStatsCount != 0 || stats.DailyTopCount != 0 || stats.RedisKeyCount != 0 {
		t.Errorf("expected empty stats after clear all, got %+v", stats)
	}

	hasData, total := HasPersistedData()
	if hasData || total != 0 {
		t.Errorf("expected HasPersistedData false and 0, got %v and %d", hasData, total)
	}
}

func TestClearAccessData_Retention(t *testing.T) {
	_, cleanup := setupTestRedisAndDB(t)
	defer cleanup()

	now := time.Now().In(time.Local)
	oldDate := now.AddDate(0, 0, -20)
	recentDate := now.AddDate(0, 0, -2)

	oldDayStr := oldDate.Format("2006-01-02")
	recentDayStr := recentDate.Format("2006-01-02")
	oldDayKey := oldDate.Format("20060102")
	recentDayKey := recentDate.Format("20060102")

	// DB
	_ = db.Mdb.Create(&model.AccessDailyStats{Day: oldDayStr, PV: 10, RolledAt: time.Now()})
	_ = db.Mdb.Create(&model.AccessDailyStats{Day: recentDayStr, PV: 20, RolledAt: time.Now()})
	_ = db.Mdb.Create(&model.AccessDailyTop{Day: oldDayStr, Kind: "play", Rank: 1, ItemKey: "old"})
	_ = db.Mdb.Create(&model.AccessDailyTop{Day: recentDayStr, Kind: "play", Rank: 1, ItemKey: "recent"})

	// Redis
	_ = db.Rdb.Set(db.Cxt, uvKey(oldDayKey), "old", 0)
	_ = db.Rdb.Set(db.Cxt, uvKey(recentDayKey), "recent", 0)

	// Retain 7 days -> old data (20 days ago) should be deleted, recent (2 days ago) kept
	res, err := ClearAccessData(7)
	if err != nil {
		t.Fatalf("ClearAccessData(7) error: %v", err)
	}
	if res.DeletedDailyStats != 1 {
		t.Errorf("expected 1 deleted daily stats, got %d", res.DeletedDailyStats)
	}
	if res.DeletedDailyTop != 1 {
		t.Errorf("expected 1 deleted daily top, got %d", res.DeletedDailyTop)
	}
	if res.DeletedRedisKeys != 1 {
		t.Errorf("expected 1 deleted redis key, got %d", res.DeletedRedisKeys)
	}

	// Verify recent day remains
	var remainingStats model.AccessDailyStats
	if err := db.Mdb.Where("day = ?", recentDayStr).First(&remainingStats).Error; err != nil {
		t.Errorf("recent stats should remain: %v", err)
	}
	var remainingTop model.AccessDailyTop
	if err := db.Mdb.Where("day = ?", recentDayStr).First(&remainingTop).Error; err != nil {
		t.Errorf("recent top should remain: %v", err)
	}
	if _, err := db.Rdb.Get(db.Cxt, uvKey(recentDayKey)).Result(); err != nil {
		t.Errorf("recent redis key should remain: %v", err)
	}

	// Verify old day is gone
	if err := db.Mdb.Where("day = ?", oldDayStr).First(&model.AccessDailyStats{}).Error; err == nil {
		t.Errorf("old stats should have been deleted")
	}
	if _, err := db.Rdb.Get(db.Cxt, uvKey(oldDayKey)).Result(); err == nil {
		t.Errorf("old redis key should have been deleted")
	}
	assertRollupLockReleased(t)
}

func TestClearAccessData_NegativeRetention(t *testing.T) {
	_, err := ClearAccessData(-1)
	if err == nil {
		t.Fatalf("expected error on negative retention days, got nil")
	}
}

func TestClearAccessData_LargeKeySetBatching(t *testing.T) {
	_, cleanup := setupTestRedisAndDB(t)
	defer cleanup()

	// Populate > 1000 Redis keys to thoroughly test 500-key batching
	totalKeys := 1200
	pipe := db.Rdb.Pipeline()
	for i := 0; i < totalKeys; i++ {
		k := fmt.Sprintf("%smin:20260901%04d", config.AccessKeyPrefix, i)
		pipe.Set(db.Cxt, k, "1", 0)
	}
	_, err := pipe.Exec(db.Cxt)
	if err != nil {
		t.Fatalf("populate redis keys: %v", err)
	}

	stats := GetAccessDataStats()
	if stats.RedisKeyCount != int64(totalKeys) {
		t.Fatalf("want RedisKeyCount=%d, got %d", totalKeys, stats.RedisKeyCount)
	}

	res, err := ClearAccessData(0)
	if err != nil {
		t.Fatalf("ClearAccessData(0) failed: %v", err)
	}
	if res.DeletedRedisKeys != int64(totalKeys) {
		t.Fatalf("want DeletedRedisKeys=%d, got %d", totalKeys, res.DeletedRedisKeys)
	}

	assertRollupLockReleased(t)

	statsAfter := GetAccessDataStats()
	if statsAfter.RedisKeyCount != 0 {
		t.Fatalf("expected 0 redis keys after clear, got %d", statsAfter.RedisKeyCount)
	}
}

func TestClearAccessData_ClusterLockHeld(t *testing.T) {
	_, cleanup := setupTestRedisAndDB(t)
	defer cleanup()

	_ = db.Mdb.Create(&model.AccessDailyStats{Day: "2026-09-01", PV: 10, RolledAt: time.Now()})
	_ = db.Rdb.Set(db.Cxt, uvKey("20260901"), "1", 0)
	_ = db.Rdb.Set(db.Cxt, rollupLockKey(), "other-node", 0)

	_, err := ClearAccessData(0)
	if err == nil {
		t.Fatal("expected error when cluster rollup lock is held")
	}

	var remaining model.AccessDailyStats
	if err := db.Mdb.Where("day = ?", "2026-09-01").First(&remaining).Error; err != nil {
		t.Fatalf("mysql row should remain when lock is held: %v", err)
	}
	if _, err := db.Rdb.Get(db.Cxt, uvKey("20260901")).Result(); err != nil {
		t.Fatalf("redis key should remain when lock is held: %v", err)
	}
	if val, err := db.Rdb.Get(db.Cxt, rollupLockKey()).Result(); err != nil || val != "other-node" {
		t.Fatalf("foreign lock token must remain, val=%s err=%v", val, err)
	}
}

func TestClearAccessData_RedisUnavailable(t *testing.T) {
	mr, cleanup := setupTestRedisAndDB(t)
	defer cleanup()

	_ = db.Mdb.Create(&model.AccessDailyStats{Day: "2026-09-01", PV: 10, RolledAt: time.Now()})
	mr.Close()

	_, err := ClearAccessData(0)
	if err == nil {
		t.Fatal("expected error when redis is unavailable")
	}

	var remaining model.AccessDailyStats
	if err := db.Mdb.Where("day = ?", "2026-09-01").First(&remaining).Error; err != nil {
		t.Fatalf("mysql row should remain when redis lock cannot be acquired: %v", err)
	}
}

func TestDeleteMatchingAccessRedisKeys_ScanError(t *testing.T) {
	mr, cleanup := setupTestRedisAndDB(t)
	defer cleanup()

	_ = db.Rdb.Set(db.Cxt, uvKey("20260901"), "1", 0)
	mr.SetError("scan-fail")

	deleted, err := deleteMatchingAccessRedisKeys(0, "", rollupLockKey())
	if err == nil {
		t.Fatal("expected scan error")
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted keys on scan error, got %d", deleted)
	}
}

func assertRollupLockReleased(t *testing.T) {
	t.Helper()
	_, err := db.Rdb.Get(db.Cxt, rollupLockKey()).Result()
	if err != redis.Nil {
		t.Fatalf("rollup lock should be released after cleanup, err=%v", err)
	}
}
