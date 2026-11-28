package enrich

import (
	"fmt"
	"testing"

	"server/internal/analytics/types"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestResolveClassifyMemberMemoryOnly_ZeroSQLWhenDBClosed(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	ResetCachesForTest()
	taxonomy.SetCategoryTreeForTest(map[int64]int64{10: 0}, map[int64]string{10: "科幻片"})

	if got := ResolveClassifyMemberMemoryOnly(&types.Event{Action: types.ActionClassify, Resource: "科幻片"}); got != "10" {
		t.Fatalf("expected 10, got %q", got)
	}
	if got := ResolveClassifyMemberMemoryOnly(&types.Event{Action: types.ActionClassify, Resource: "不存在分类"}); got != "不存在分类" {
		t.Fatalf("expected raw fallback, got %q", got)
	}
}

func TestTakePlayTopsAndEnrichLogEvents(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}, &model.Category{}); err != nil {
		t.Fatal(err)
	}
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101, FirstSourceId: "s1"},
		FilmIndexCategory: model.FilmIndexCategory{CName: "动作片"},
		FilmIndexContent:  model.FilmIndexContent{Name: "测试影片", Picture: "https://img.example.com/101.jpg", Year: 2025},
	})
	gdb.Create(&model.Category{Id: 5, Pid: 0, Name: "动作片", Show: true, Sort: 1})

	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() {
		db.Mdb = prev
		ResetCachesForTest()
	})
	ResetCachesForTest()

	tops, err := TakePlayTops([]types.TopItem{
		{Key: "bad-key", Count: 9},
		{Key: "101", Count: 5},
		{Key: "9999", Count: 3},
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tops) != 1 || tops[0].Title != "测试影片" || tops[0].Poster != "https://img.example.com/101.jpg" {
		t.Fatalf("unexpected play tops: %+v", tops)
	}

	logs, err := EnrichLogEvents([]types.Event{
		{Action: types.ActionPlay, Resource: "101"},
		{Action: types.ActionClassify, Resource: "5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if logs[0].ResourceTitle != "测试影片" || logs[1].ResourceCat != "动作片" {
		t.Fatalf("unexpected enriched logs: %+v", logs)
	}
}

func TestSanitizePosterURL(t *testing.T) {
	if got := SanitizePosterURL("javascript:alert(1)"); got != "" {
		t.Fatalf("expected empty for javascript:, got %q", got)
	}
	if got := SanitizePosterURL("https://cdn.example.com/a.jpg"); got != "https://cdn.example.com/a.jpg" {
		t.Fatalf("expected https url kept, got %q", got)
	}
}

func TestResolveFilmMetas_BatchesLargeIDList(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}); err != nil {
		t.Fatal(err)
	}
	const total = 1200
	batch := make([]model.FilmIndex, 0, total)
	ids := make([]int64, 0, total)
	for i := 1; i <= total; i++ {
		id := int64(i)
		ids = append(ids, id)
		batch = append(batch, model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: id, FirstSourceId: "s1"},
			FilmIndexContent:  model.FilmIndexContent{Name: fmt.Sprintf("影片-%d", i)},
		})
	}
	if err := gdb.CreateInBatches(&batch, 200).Error; err != nil {
		t.Fatal(err)
	}

	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() {
		db.Mdb = prev
		ResetCachesForTest()
	})
	ResetCachesForTest()

	metas, err := ResolveFilmMetas(ids)
	if err != nil {
		t.Fatalf("ResolveFilmMetas large batch failed: %v", err)
	}
	if len(metas) != total {
		t.Fatalf("expected %d metas, got %d", total, len(metas))
	}
}
