package support

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"
)

func setupPrimaryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.PrimaryState{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
	return gdb
}

// 存储为空时按排序首位懒初始化并持久化；之后排序变化不再改变主站。
func TestCurrentPrimarySource_LazyPickPersistsAndWinsOverSort(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "a", Name: "A", Uri: "http://a", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "b", Name: "B", Uri: "http://b", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}

	if p := CurrentPrimarySource(); p == nil || p.Id != "b" {
		t.Fatalf("expected lazy-picked b, got %+v", p)
	}
	var rec model.PrimaryState
	if err := gdb.Where("id = ?", 1).First(&rec).Error; err != nil {
		t.Fatalf("primary state should be persisted: %v", err)
	}
	if rec.PrimarySourceId != "b" {
		t.Fatalf("expected persisted b, got %q", rec.PrimarySourceId)
	}

	// 排序把 a 顶到首位，主站不变（S-05 语义）
	if err := gdb.Model(&model.FilmSource{}).Where("id = ?", "a").Update("sort", -1).Error; err != nil {
		t.Fatal(err)
	}
	if p := CurrentPrimarySource(); p == nil || p.Id != "b" {
		t.Fatalf("stored primary must win over sort order, got %+v", p)
	}
	if id := CurrentPrimaryID(); id != "b" {
		t.Fatalf("expected id b, got %q", id)
	}
}

// SetPrimarySourceID 显式写入后，主站以存储为准。
func TestSetPrimarySourceID_StoredWins(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "a", Name: "A", Uri: "http://a", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "b", Name: "B", Uri: "http://b", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := SetPrimarySourceID("b"); err != nil {
		t.Fatal(err)
	}
	if p := CurrentPrimarySource(); p == nil || p.Id != "b" {
		t.Fatalf("expected stored b, got %+v", p)
	}
	var srcB, srcA model.FilmSource
	if err := gdb.Where("id = ?", "b").First(&srcB).Error; err != nil {
		t.Fatal(err)
	}
	if srcB.Sort != 0 {
		t.Fatalf("expected b moved to sort 0, got %d", srcB.Sort)
	}
	if err := gdb.Where("id = ?", "a").First(&srcA).Error; err != nil {
		t.Fatal(err)
	}
	if srcA.Sort != 1 {
		t.Fatalf("expected a shifted to sort 1, got %d", srcA.Sort)
	}
}

// D6：存储指向停用站属异常，返回 nil，不自动换站。
func TestCurrentPrimarySource_StoredDisabledReturnsNil(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "a", Name: "A", Uri: "http://a", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "b", Name: "B", Uri: "http://b", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := SetPrimarySourceID("a"); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&model.FilmSource{}).Where("id = ?", "a").Update("state", false).Error; err != nil {
		t.Fatal(err)
	}
	if p := CurrentPrimarySource(); p != nil {
		t.Fatalf("expected nil for disabled stored primary, got %+v", p)
	}
	if id := CurrentPrimaryID(); id != "" {
		t.Fatalf("expected empty id for disabled stored primary, got %q", id)
	}
}

// D6：存储指向删除中的站同样不自动换站。
func TestCurrentPrimarySource_StoredDeletingReturnsNil(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "a", Name: "A", Uri: "http://a", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := SetPrimarySourceID("a"); err != nil {
		t.Fatal(err)
	}
	RegisterSourceDeletingChecker(func(id string) bool { return id == "a" })
	t.Cleanup(func() { RegisterSourceDeletingChecker(nil) })
	if p := CurrentPrimarySource(); p != nil {
		t.Fatalf("expected nil for deleting stored primary, got %+v", p)
	}
}

// 存储指向不存在的站：告警并返回 nil，等待人工切换。
func TestCurrentPrimarySource_StoredMissingReturnsNil(t *testing.T) {
	setupPrimaryTestDB(t)
	if err := SetPrimarySourceID("ghost"); err != nil {
		t.Fatal(err)
	}
	if p := CurrentPrimarySource(); p != nil {
		t.Fatalf("expected nil for missing stored primary, got %+v", p)
	}
}

// 无任何启用站时为空主站（D5）。
func TestCurrentPrimarySource_NoEnabledReturnsNil(t *testing.T) {
	gdb := setupPrimaryTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "a", Name: "A", Uri: "http://a", State: false, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if p := CurrentPrimarySource(); p != nil {
		t.Fatalf("expected nil when no enabled sources, got %+v", p)
	}
	if err := gdb.Model(&model.FilmSource{}).Where("id = ?", "a").Update("state", true).Error; err != nil {
		t.Fatal(err)
	}
	// 主站为空时，第一个被启用的站自动成为主站（D5）
	if p := CurrentPrimarySource(); p == nil || p.Id != "a" {
		t.Fatalf("expected a after enabling, got %+v", p)
	}
}
