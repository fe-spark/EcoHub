package notify

import (
	"fmt"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDBForChangeBatch(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	dsn := fmt.Sprintf("file:notify_batch_%d?mode=memory&cache=shared", time.Now().UnixNano())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}

	type filmIndexRow struct {
		Mid         int64 `gorm:"column:mid;primaryKey"`
		Pid         int64 `gorm:"column:pid"`
		UpdateStamp int64 `gorm:"column:update_stamp"`
	}

	if err := gdb.Table(model.TableFilmIndex).AutoMigrate(&filmIndexRow{}); err != nil {
		t.Fatalf("migrate film_index failed: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Category{}); err != nil {
		t.Fatalf("migrate film_category failed: %v", err)
	}

	oldDB := db.Mdb
	db.Mdb = gdb

	cleanup := func() {
		db.Mdb = oldDB
		sqlDB, err := gdb.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
	return gdb, cleanup
}

func TestBuildCategoryPlanForMids_OrderConsistency(t *testing.T) {
	gdb, cleanup := setupTestDBForChangeBatch(t)
	defer cleanup()

	// 模拟写入数据：
	// mid=10, stamp=100, pid=1
	// mid=20, stamp=300, pid=1 (最新, 与 mid=40 同 stamp)
	// mid=30, stamp=200, pid=2
	// mid=40, stamp=300, pid=2 (最新, mid 更大应排最前)
	// mid=50, stamp=50,  pid=0 (其他分类，最早)
	rows := []map[string]interface{}{
		{"mid": int64(10), "pid": int64(1), "update_stamp": int64(100)},
		{"mid": int64(20), "pid": int64(1), "update_stamp": int64(300)},
		{"mid": int64(30), "pid": int64(2), "update_stamp": int64(200)},
		{"mid": int64(40), "pid": int64(2), "update_stamp": int64(300)},
		{"mid": int64(50), "pid": int64(0), "update_stamp": int64(50)},
	}
	for _, r := range rows {
		if err := gdb.Table(model.TableFilmIndex).Create(r).Error; err != nil {
			t.Fatalf("insert film_index failed: %v", err)
		}
	}

	// 传入由小到大的 items (之前 MySQL 写入防死锁升序排列后的顺序)
	items := []ChangeMidItem{
		{Mid: 10, SourceName: "源A"},
		{Mid: 20, SourceName: "源A"},
		{Mid: 30, SourceName: "源B"},
		{Mid: 40, SourceName: "源B"},
		{Mid: 50, SourceName: "源C"},
	}

	cats, catMids, err := BuildCategoryPlanForMids(items)
	if err != nil {
		t.Fatalf("BuildCategoryPlanForMids failed: %v", err)
	}

	// 预期排序为 update_stamp DESC, mid DESC:
	// 1. stamp=300, mid=40
	// 2. stamp=300, mid=20
	// 3. stamp=200, mid=30
	// 4. stamp=100, mid=10
	// 5. stamp=50,  mid=50
	expectedMids := []int64{40, 20, 30, 10, 50}
	if len(items) != len(expectedMids) {
		t.Fatalf("want len %d, got %d", len(expectedMids), len(items))
	}
	for i, want := range expectedMids {
		if items[i].Mid != want {
			t.Errorf("items[%d] want mid %d, got %d", i, want, items[i].Mid)
		}
	}

	// 与每日更新接口查询结果对比：验证两者的排序完全一致
	dailyMids, totalMids, err := ListDailyUpdateMids(DailyUpdateListQuery{
		From:     time.Unix(0, 0),
		To:       time.Now().Add(24 * time.Hour),
		Pid:      DailyPidAll,
		Current:  1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListDailyUpdateMids failed: %v", err)
	}
	if totalMids != 5 || len(dailyMids) != 5 {
		t.Fatalf("dailyMids want 5 got %d (total %d)", len(dailyMids), totalMids)
	}
	for i, dMid := range dailyMids {
		if items[i].Mid != dMid {
			t.Errorf("mismatch at index %d: notify mid=%d, daily update mid=%d", i, items[i].Mid, dMid)
		}
	}

	// 验证会话分页第一页读取与最新优先对齐
	sess := FilmBatchSession{
		BatchID:  "test_batch_order",
		PageSize: 2,
		Total:    len(items),
		AllItems: items,
		Cats:     cats,
		CatMids:  catMids,
	}
	chunk, total, start, end, page := batchPageChunk(sess, -1, 1)
	if total != 5 || page != 1 || start != 0 || end != 2 {
		t.Fatalf("unexpected chunk meta: total=%d page=%d start=%d end=%d", total, page, start, end)
	}
	if len(chunk) != 2 || chunk[0].Mid != 40 || chunk[1].Mid != 20 {
		t.Fatalf("first page must show newest films [40, 20], got %+v", chunk)
	}
}

func TestBuildCategoryPlanForMids_NilDBFallback(t *testing.T) {
	oldDB := db.Mdb
	db.Mdb = nil
	defer func() { db.Mdb = oldDB }()

	items := []ChangeMidItem{
		{Mid: 1},
		{Mid: 5},
		{Mid: 2},
		{Mid: 8},
	}
	_, _, err := BuildCategoryPlanForMids(items)
	if err != nil {
		t.Fatalf("nil db should not error: %v", err)
	}
	expectedMids := []int64{8, 5, 2, 1}
	for i, want := range expectedMids {
		if items[i].Mid != want {
			t.Errorf("fallback items[%d] want %d, got %d", i, want, items[i].Mid)
		}
	}
}
