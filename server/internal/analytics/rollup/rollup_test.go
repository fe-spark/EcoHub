package rollup

import (
	"sync"
	"testing"
	"time"

	"server/internal/analytics/config"
	"server/internal/analytics/store"
	"server/internal/analytics/types"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDaysToRollAndSnapshotWithScopedActionsAndSources(t *testing.T) {
	loc := time.Local
	yesterday := time.Date(2026, 11, 27, 0, 0, 0, 0, loc)
	if got := DaysToRoll(yesterday, yesterday); len(got) != 0 {
		t.Fatalf("expected 0 days, got %v", got)
	}
	days := DaysToRoll(time.Date(2026, 11, 20, 0, 0, 0, 0, loc), yesterday)
	if len(days) != 7 {
		t.Fatalf("expected 7 days, got %d", len(days))
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.AccessDailyStats{}, &model.AccessDailyTop{}, &model.FilmIndex{}, &model.Category{}); err != nil {
		t.Fatal(err)
	}
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 202, FirstSourceId: "src1"},
		FilmIndexCategory: model.FilmIndexCategory{CName: "喜剧片"},
		FilmIndexContent:  model.FilmIndexContent{Name: "归档测试片"},
	})

	prevRdb, prevMdb := db.Rdb, db.Mdb
	db.Rdb, db.Mdb = rdb, gdb
	t.Cleanup(func() { db.Rdb, db.Mdb = prevRdb, prevMdb })

	store.WriteBatch([]*types.Event{
		{
			Ts:              yesterday.Add(10 * time.Hour),
			Method:          "PAGE",
			Path:            "/play",
			Page:            "/play",
			Action:          types.ActionPlay,
			ClientType:      "web",
			Resource:        "202",
			PlayMember:      "202",
			CollectSourceId: "src1",
			UVMember:        "u1",
		},
		{
			Ts:          yesterday.Add(11 * time.Hour),
			Method:      "PAGE",
			Path:        "SearchPage",
			Page:        "SearchPage",
			Action:      types.ActionSearch,
			ClientType:  "android",
			AppVersion:  "2.1.0",
			DeviceModel: "Pixel 9",
			Resource:    "科幻",
			UVMember:    "u2",
		},
	})

	stats, tops, has, err := SnapshotDayFromRedis(yesterday)
	if err != nil {
		t.Fatal(err)
	}
	if !has || stats.WebPV != 1 || stats.AppPV != 1 {
		t.Fatalf("unexpected snapshot stats: has=%v %+v", has, stats)
	}
	if err := PersistDaily(stats, tops); err != nil {
		t.Fatal(err)
	}
	row, ok, err := LoadDailyStats(yesterday.Format("2006-01-02"))
	if err != nil || !ok {
		t.Fatalf("load daily stats failed: ok=%v err=%v", ok, err)
	}
	webAct := store.UnmarshalIntMap(row.WebActionJSON)
	srcCalls := store.UnmarshalIntMap(row.SourceCallsJSON)
	if webAct[types.ActionPlay] != 1 || srcCalls["src1"] != 1 {
		t.Fatalf("expected persisted web action and source calls, got webAct=%v srcCalls=%v", webAct, srcCalls)
	}
}

func TestSchedulerStopWaitsAndClearDataDoesNotRace(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.AccessDailyStats{}, &model.AccessDailyTop{}); err != nil {
		t.Fatal(err)
	}

	prevRdb, prevMdb := db.Rdb, db.Mdb
	prevEnabled := config.IsEnabled()
	db.Rdb, db.Mdb = rdb, gdb
	t.Cleanup(func() {
		StopScheduler()
		config.SetEnabled(prevEnabled)
		db.Rdb, db.Mdb = prevRdb, prevMdb
	})

	yesterday := time.Now().In(time.Local).AddDate(0, 0, -1)
	store.WriteBatch([]*types.Event{
		{
			Ts:         yesterday,
			Method:     "PAGE",
			Path:       "/",
			Page:       "/",
			Action:     types.ActionBrowse,
			ClientType: "web",
			UVMember:   "u1",
		},
	})

	config.SetEnabled(true)
	StartScheduler()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		RunDailyRollup()
	}()
	go func() {
		defer wg.Done()
		config.SetEnabled(false)
		StopScheduler()
		if _, err := store.ClearData(0, Mutex(), nil); err != nil {
			t.Errorf("ClearData failed: %v", err)
		}
	}()
	wg.Wait()

	// ClearData 完成后，再次触发 RunDailyRollup 因 disabled 且在锁内二次校验，不得写回任何残留行
	RunDailyRollup()
	var count int64
	if err := gdb.Model(&model.AccessDailyStats{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 stats rows after disabled reset, got %d", count)
	}
}
