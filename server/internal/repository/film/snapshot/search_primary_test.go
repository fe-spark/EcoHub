package snapshot

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRetainIndexedDropsIgnoresAbsentMIDs(t *testing.T) {
	prev := db.Mdb
	db.Mdb = nil
	t.Cleanup(func() { db.Mdb = prev })
	idx := sealSearchMetaIndex("v", []FilmSearchMeta{{Mid: 1}})
	got := retainIndexedDrops(idx, map[int64]struct{}{1: {}, 9: {}})
	if len(got) != 1 {
		t.Fatalf("drops = %v", got)
	}
	if _, ok := got[1]; !ok {
		t.Fatal("indexed mid must stay")
	}
}

func TestPrimarySearchMIDs_QueryError(t *testing.T) {
	prev := db.Mdb
	t.Cleanup(func() { db.Mdb = prev })
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{
		Id: "uku", Name: "主站", Uri: "https://example.com/uku", State: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	db.Mdb = gdb

	got, err := PrimarySearchMIDs([]int64{1, 2})
	if err == nil {
		t.Fatal("missing film_index should fail the ownership query")
	}
	if got != nil {
		t.Fatalf("error result = %v", got)
	}
}

func TestPrimarySearchMIDs_DropsOtherSource(t *testing.T) {
	prev := db.Mdb
	t.Cleanup(func() { db.Mdb = prev })
	gdb := setupSearchSourceTestDB(t)
	if err := gdb.Create(&[]model.FilmIndex{
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "uku"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "hd"}},
	}).Error; err != nil {
		t.Fatal(err)
	}

	got, err := PrimarySearchMIDs([]int64{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("kept = %v", got)
	}
}
