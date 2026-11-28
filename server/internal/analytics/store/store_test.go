package store

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

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

func TestAdmitBoundedHashFieldAndCompactTopIntMap(t *testing.T) {
	ResetBoundedHashGateForTest()
	key := "test:app:models:20261128"
	big := map[string]int64{}
	for i := 0; i < MaxBoundedHashFields+80; i++ {
		name := fmt.Sprintf("Device-Model-LongName-%04d", i)
		got := AdmitBoundedHashField(key, name)
		if i < MaxBoundedHashFields && got != name {
			t.Fatalf("within limit want %q, got %q", name, got)
		}
		if i >= MaxBoundedHashFields && got != "other" {
			t.Fatalf("overflow want other, got %q", got)
		}
		big[name] = int64(i + 1)
	}
	compacted := CompactTopIntMap(big, 80, 32768)
	raw, err := json.Marshal(compacted)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 32768 || compacted["other"] <= 0 {
		t.Fatalf("unexpected compact result len=%d other=%d", len(raw), compacted["other"])
	}
}

func TestWriteBatch_BucketSlotsAndClearData(t *testing.T) {
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
	db.Rdb, db.Mdb = rdb, gdb
	t.Cleanup(func() { db.Rdb, db.Mdb = prevRdb, prevMdb })

	now := time.Now().In(time.Local)
	dayKey := now.Format("20060102")
	WriteBatch([]*types.Event{
		{
			Ts:         now,
			Method:     "PAGE",
			Path:       "/play",
			Page:       "/play",
			Action:     types.ActionPlay,
			ClientType: "web",
			UAFamily:   "chrome",
			OS:         "macOS",
			Resource:   "101",
			PlayMember: "101",
			UVMember:   "u1",
		},
		{
			Ts:         now,
			Method:     "GET",
			Path:       "/api/provide/vod",
			Action:     types.ActionPlay,
			Status:     200,
			LatencyMs:  40,
			ClientType: "tvbox",
			Resource:   "101",
			PlayMember: "101",
			UVMember:   "u2",
		},
	})

	rawBucket, err := rdb.HGetAll(db.Cxt, BucketKey(dayKey)).Result()
	if err != nil {
		t.Fatal(err)
	}
	series, totalPV := FoldBucketMap(now, now, rawBucket)
	if len(series) == 0 || totalPV != 2 {
		t.Fatalf("expected totalPV=2 in bucket hash, got len=%d totalPV=%d", len(series), totalPV)
	}

	stats, err := GetDataStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.RedisKeyCount == 0 {
		t.Fatal("expected non-zero redis keys before cleanup")
	}

	res, err := ClearData(0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedRedisKeys == 0 {
		t.Fatal("expected deleted redis keys > 0")
	}
}
