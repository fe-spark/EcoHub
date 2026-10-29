package spider

import (
	"fmt"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/spider/progress"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPrepareBatchCollectStart_ReportsSkipReasons(t *testing.T) {
	prev := db.Mdb
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.CategoryMapping{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	rows := []model.FilmSource{
		{Id: "ready", Name: "可采", Uri: "http://example.invalid/ready", State: true},
		{Id: "off", Name: "停用", Uri: "http://example.invalid/off", State: false},
		{Id: "bare", Name: "无类", Uri: "http://example.invalid/bare", State: true},
		{Id: "both", Name: "都缺", Uri: "http://example.invalid/both", State: false},
		{Id: "busy", Name: "在采", Uri: "http://example.invalid/busy", State: true},
		{Id: "sync", Name: "拉类", Uri: "http://example.invalid/sync", State: true},
		{Id: "halt", Name: "收尾", Uri: "http://example.invalid/halt", State: true},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("seed sources: %v", err)
	}
	for _, id := range []string{"ready", "off", "busy", "sync", "halt"} {
		if err := gdb.Create(&model.CategoryMapping{SourceId: id, SourceTypeId: 1, CategoryId: 9}).Error; err != nil {
			t.Fatalf("seed mapping %s: %v", id, err)
		}
	}

	progress.MarkSourcesCollectStarting([]model.FilmSource{{Id: "busy", Name: "在采"}}, "q-test-busy")
	progress.MarkSourcesCollectStarting([]model.FilmSource{{Id: "halt", Name: "收尾"}}, "q-test-halt")
	progress.Update("halt", func(p *model.CollectProgress) { p.Status = progress.StatusStopping })
	progress.BroadcastCategoryStatus("sync", "syncing")
	t.Cleanup(func() {
		for _, id := range []string{"ready", "busy", "halt", "sync"} {
			releaseCollectSourceIDs(id)
			progress.Update(id, func(p *model.CollectProgress) { p.Status = progress.StatusStopped })
		}
		progress.BroadcastCategoryStatus("sync", "idle")
	})

	started, result := PrepareBatchCollectStart([]string{
		"ready", "off", "bare", "both", "busy", "sync", "halt", "missing", "ready", "  ",
	})
	if len(started) != 1 || started[0].Id != "ready" {
		t.Fatalf("started = %+v", started)
	}
	if !isOccupiedCollectSource("ready") {
		t.Fatal("ready source must be occupied")
	}
	if isOccupiedCollectSource("busy") || isOccupiedCollectSource("off") || isOccupiedCollectSource("bare") {
		t.Fatal("skipped sources must not be occupied")
	}

	got := map[string]string{}
	for _, item := range result.Skipped {
		got[item.Id] = item.Reason
	}
	want := map[string]string{
		"off":     "未启用",
		"bare":    "无分类",
		"both":    "未启用、无分类",
		"busy":    "采集中",
		"sync":    "分类采集中",
		"halt":    "停止中",
		"missing": "采集站不存在",
	}
	if len(got) != len(want) {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	for id, reason := range want {
		if got[id] != reason {
			t.Fatalf("skip %s = %q, want %q", id, got[id], reason)
		}
	}
}

func TestIsOccupiedCollectSource_SelfHealsStaleOccupy(t *testing.T) {
	const sourceID = "stale-occupy-test"
	collectingSourcesMu.Lock()
	collectingSources[sourceID] = 999
	collectingTimes[sourceID] = time.Now().Add(-35 * time.Second)
	collectingSourcesMu.Unlock()
	defer releaseCollectSourceIDs(sourceID)

	if isOccupiedCollectSource(sourceID) {
		t.Fatal("expected isOccupiedCollectSource to self-heal stale occupy and return false")
	}

	collectingSourcesMu.Lock()
	_, exists := collectingSources[sourceID]
	collectingSourcesMu.Unlock()
	if exists {
		t.Fatal("expected stale occupy to be deleted from collectingSources")
	}
}

func TestBatchCollect_ReleasesOccupyAfterFinished(t *testing.T) {
	prev := db.Mdb
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.CategoryMapping{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	src := model.FilmSource{Id: "test-finish-src", Name: "测试站", Uri: "http://example.invalid", State: true}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: src.Id, SourceTypeId: 1, CategoryId: 1}).Error; err != nil {
		t.Fatal(err)
	}
	defer releaseCollectSourceIDs(src.Id)

	claimed, res := PrepareBatchCollectStart([]string{src.Id})
	if len(claimed) != 1 {
		t.Fatalf("claimed = %+v, skipped = %+v", claimed, res.Skipped)
	}
	if !isOccupiedCollectSource(src.Id) {
		t.Fatal("source should be occupied after PrepareBatchCollectStart")
	}

	BatchCollectPrepared(model.CollectTriggerManual, 24, claimed)

	if isOccupiedCollectSource(src.Id) {
		t.Fatal("source should NOT be occupied after BatchCollectPrepared finishes")
	}
}
