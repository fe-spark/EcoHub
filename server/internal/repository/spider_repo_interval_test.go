package repository

import (
	"encoding/json"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAddCollectSourceKeepsExplicitZeroInterval(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	if err := AddCollectSource(model.FilmSource{Id: "zero", Name: "零", Uri: "http://zero", CollectIntervalMs: 0}); err != nil {
		t.Fatal(err)
	}
	if err := AddCollectSource(model.FilmSource{Id: "paced", Name: "间隔", Uri: "http://paced", CollectIntervalMs: model.CollectIntervalMsDefault}); err != nil {
		t.Fatal(err)
	}

	var zero, paced model.FilmSource
	if err := gdb.First(&zero, "id = ?", "zero").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.First(&paced, "id = ?", "paced").Error; err != nil {
		t.Fatal(err)
	}
	if zero.CollectIntervalMs != 0 {
		t.Fatalf("explicit zero stored as %d", zero.CollectIntervalMs)
	}
	if paced.CollectIntervalMs != model.CollectIntervalMsDefault {
		t.Fatalf("default interval stored as %d", paced.CollectIntervalMs)
	}
}

func TestJSONMissingIntervalUsesDefault(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	var missing model.FilmSource
	if err := json.Unmarshal([]byte(`{"id":"oldbk","name":"旧","uri":"http://oldbk"}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.CollectIntervalMs != model.CollectIntervalMsDefault {
		t.Fatalf("missing interval = %d", missing.CollectIntervalMs)
	}
	var explicit model.FilmSource
	if err := json.Unmarshal([]byte(`{"id":"explicit0","name":"零","uri":"http://explicit0","collectIntervalMs":0}`), &explicit); err != nil {
		t.Fatal(err)
	}
	if explicit.CollectIntervalMs != 0 {
		t.Fatalf("explicit zero = %d", explicit.CollectIntervalMs)
	}
	var backup model.ConfigBackup
	if err := json.Unmarshal([]byte(`{"filmSources":[{"id":"frombak","name":"备","uri":"http://frombak"}]}`), &backup); err != nil {
		t.Fatal(err)
	}
	if len(backup.FilmSources) != 1 || backup.FilmSources[0].CollectIntervalMs != model.CollectIntervalMsDefault {
		t.Fatalf("backup interval = %+v", backup.FilmSources)
	}
	if err := AddCollectSource(missing); err != nil {
		t.Fatal(err)
	}
	if err := AddCollectSource(explicit); err != nil {
		t.Fatal(err)
	}
	if err := AddCollectSource(model.FilmSource{Id: "keep80", Name: "留", Uri: "http://keep80", CollectIntervalMs: 80}); err != nil {
		t.Fatal(err)
	}

	var edited model.FilmSource
	if err := json.Unmarshal([]byte(`{"id":"keep80","name":"留","uri":"http://keep80"}`), &edited); err != nil {
		t.Fatal(err)
	}
	if err := UpdateCollectSource(edited); err != nil {
		t.Fatal(err)
	}

	var old, zero, kept model.FilmSource
	if err := gdb.First(&old, "id = ?", "oldbk").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.First(&zero, "id = ?", "explicit0").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.First(&kept, "id = ?", "keep80").Error; err != nil {
		t.Fatal(err)
	}
	if old.CollectIntervalMs != model.CollectIntervalMsDefault || zero.CollectIntervalMs != 0 || kept.CollectIntervalMs != model.CollectIntervalMsDefault {
		t.Fatalf("stored old=%d zero=%d kept=%d", old.CollectIntervalMs, zero.CollectIntervalMs, kept.CollectIntervalMs)
	}
}
