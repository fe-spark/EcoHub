package film

import (
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupResetStatsDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.Category{},
		&model.CategoryMapping{},
		&model.CollectRecord{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	db.Rdb = nil
	return gdb
}

func TestGetLibraryInventory_TotalsAcrossSources(t *testing.T) {
	gdb := setupResetStatsDB(t)

	gdb.Create(&model.FilmSource{Id: "src_a", Name: "首选站A", Uri: "http://a.test", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "备用站B", Uri: "http://b.test", State: true, Sort: 1})

	for _, row := range []model.FilmIndex{
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src_a"}, FilmIndexContent: model.FilmIndexContent{Name: "共享片"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src_b"}, FilmIndexContent: model.FilmIndexContent{Name: "共享片"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "src_a"}, FilmIndexContent: model.FilmIndexContent{Name: "独播片"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 4, FirstSourceId: "src_a"}, FilmIndexContent: model.FilmIndexContent{Name: "单线路"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 6, FirstSourceId: "src_a"}, FilmIndexContent: model.FilmIndexContent{Name: "共享片"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 5, FirstSourceId: "src_a"}, FilmIndexContent: model.FilmIndexContent{Name: "已删片"}},
	} {
		gdb.Create(&row)
	}
	gdb.Delete(&model.FilmIndex{}, "mid = ?", int64(5))

	play := func(mid int64, sourceID string, group int) {
		gdb.Create(&model.FilmSourcePlaylist{Mid: mid, SourceId: sourceID, LineKind: "play", GroupIndex: group, Content: "[]"})
	}
	play(1, "src_a", 0)
	play(1, "src_a", 1)
	gdb.Create(&model.FilmSourcePlaylist{Mid: 1, SourceId: "src_a", LineKind: "download", GroupIndex: 0, Content: "[]"})
	play(2, "src_b", 0)
	play(3, "src_a", 0)
	play(3, "src_b", 0)
	play(6, "src_a", 0)
	play(5, "src_a", 0)
	play(99, "src_a", 0)

	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", StableKey: "movie", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 2, Pid: 0, Name: "隐藏", StableKey: "hidden", Show: false, Sort: 2})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 2, CategoryId: 2})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 9, CategoryId: 999})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 1, CategoryId: 1})

	now := time.Now()
	gdb.Create(&model.CollectRecord{
		BatchID:    "yesterday",
		Trigger:    model.CollectTriggerCron,
		Status:     model.CollectRecordStatusPartial,
		StartedAt:  now.AddDate(0, 0, -2),
		PageFailed: 100,
		DetailJson: `[{"sourceId":"src_a","status":"partial","failedCnt":100,"failedPages":[1]}]`,
	})
	gdb.Create(&model.CollectRecord{
		BatchID:    "today",
		Trigger:    model.CollectTriggerCron,
		Status:     model.CollectRecordStatusPartial,
		StartedAt:  now,
		PageFailed: 6,
		DetailJson: `[{"sourceId":"src_a","status":"partial","failedCnt":4,"failedPages":[11,12,13,14]},` +
			`{"sourceId":"src_b","status":"partial","failedCnt":2}]`,
	})

	filmsnapshot.ClearActiveFilmReadModel()
	if err := repository.RefreshFilmCounts(); err != nil {
		t.Fatal(err)
	}
	stats, err := GetLibraryInventory()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Films != 5 || stats.Categories != 3 || stats.Failures != 6 {
		t.Fatalf("library = %+v", stats)
	}

	sources, err := GetSourceInventories()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %+v", sources)
	}
	primary := sources[0]
	if primary.Id != "src_a" || !primary.IsPrimary || !primary.Enabled || primary.Films != 4 || primary.Categories != 2 || primary.Failures != 4 {
		t.Fatalf("primary source = %+v", primary)
	}
	other := sources[1]
	if other.Id != "src_b" || other.IsPrimary || other.Films != 1 || other.Categories != 1 || other.Failures != 2 {
		t.Fatalf("other source = %+v", other)
	}
}

func TestGetLibraryInventory_ReflectsFilmCountAfterPrimaryCacheWarmed(t *testing.T) {
	gdb := setupResetStatsDB(t)

	gdb.Create(&model.FilmSource{Id: "src_a", Name: "首选站A", Uri: "http://a.test", State: true, Sort: 0, FilmCount: 0})
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "备用站B", Uri: "http://b.test", State: true, Sort: 1, FilmCount: 500})
	// 先预热主站内存缓存，模拟采集前已读取过主站
	if primary := repository.CurrentPrimarySource(); primary == nil || primary.FilmCount != 0 {
		t.Fatalf("expected warmed primary with FilmCount=0, got %+v", primary)
	}

	// 模拟采集入库后仅更新数据库中的 film_count
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		return repository.AddFilmCountTx(tx, "src_a", 1135)
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := GetLibraryInventory()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Films != 1635 {
		t.Fatalf("expected library films 1635 after count update, got %d", stats.Films)
	}
}
