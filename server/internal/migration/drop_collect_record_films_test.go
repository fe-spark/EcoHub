package migration

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"server/internal/model"

	"github.com/glebarez/sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrateDropCollectRecordFilms(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}, &model.CollectRecord{}, &legacyCollectRecordFilmItem{}); err != nil {
		t.Fatal(err)
	}
	// 模拟存量表尚无 last_record_id 且仅有旧 3 列索引或需在线升级的场景。
	if err := gdb.Exec(`DROP INDEX IF EXISTS idx_film_index_source_record_mid`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`ALTER TABLE film_index DROP COLUMN last_record_id`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`ALTER TABLE collect_records ADD COLUMN ledger INTEGER NOT NULL DEFAULT 1`).Error; err != nil {
		t.Fatal(err)
	}

	prevChunk := backfillTargetMidsChunkSize
	backfillTargetMidsChunkSize = 2
	t.Cleanup(func() { backfillTargetMidsChunkSize = prevChunk })

	var recIDs []uint
	for i := 0; i < 3; i++ {
		rec := &model.CollectRecord{
			BatchID:   fmt.Sprintf("single-legacy-%d", i+1),
			Trigger:   model.CollectTriggerSingleUpdate,
			Tag:       model.CollectTagSingleUpdate,
			Status:    model.CollectRecordStatusSuccess,
			StartedAt: time.Now(),
		}
		if err := gdb.Create(rec).Error; err != nil {
			t.Fatal(err)
		}
		recIDs = append(recIDs, rec.ID)
		if err := gdb.Create(&legacyCollectRecordFilmItem{RecordID: rec.ID, SourceID: "s1", Mid: int64(501 + i)}).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := migrateDropCollectRecordFilms(gdb); err != nil {
		t.Fatalf("migrateDropCollectRecordFilms failed: %v", err)
	}
	if err := migrateDropCollectRecordFilms(gdb); err != nil {
		t.Fatalf("second migrateDropCollectRecordFilms failed: %v", err)
	}

	if gdb.Migrator().HasTable("collect_record_films") {
		t.Fatal("collect_record_films table should be dropped")
	}
	if gdb.Migrator().HasColumn(&model.CollectRecord{}, "ledger") {
		t.Fatal("collect_records.ledger column should be dropped")
	}
	if !gdb.Migrator().HasColumn(&model.FilmIndex{}, "last_record_id") {
		t.Fatal("film_index.last_record_id column should exist")
	}
	cols, exists, err := currentIndexColumns(gdb, true, model.TableFilmIndex, notifyFilmIndexSpec.name)
	if err != nil || !exists || !slices.Equal(cols, notifyFilmIndexSpec.cols) {
		t.Fatalf("expected index %s cols=%v, got exists=%v cols=%v err=%v", notifyFilmIndexSpec.name, notifyFilmIndexSpec.cols, exists, cols, err)
	}
	for i, id := range recIDs {
		var updated model.CollectRecord
		if err := gdb.Where("id = ?", id).First(&updated).Error; err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("[%d]", 501+i)
		if updated.TargetMids != want {
			t.Fatalf("record %d expected target_mids=%s, got %q", id, want, updated.TargetMids)
		}
	}
}

func TestMigrateDropCollectRecordFilms_MySQLRerun(t *testing.T) {
	gdb := openTestMySQL(t)
	_ = gdb.Migrator().DropTable(&model.FilmIndex{}, &model.CollectRecord{}, &legacyCollectRecordFilmItem{})
	if err := gdb.AutoMigrate(&model.FilmIndex{}, &model.CollectRecord{}, &legacyCollectRecordFilmItem{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`ALTER TABLE film_index DROP INDEX idx_film_index_source_record_mid`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`ALTER TABLE film_index DROP COLUMN last_record_id`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`ALTER TABLE collect_records ADD COLUMN ledger TINYINT(1) NOT NULL DEFAULT 1`).Error; err != nil {
		t.Fatal(err)
	}

	rec := &model.CollectRecord{
		BatchID:   "mysql-single-1",
		Trigger:   model.CollectTriggerSingleUpdate,
		Tag:       model.CollectTagSingleUpdate,
		Status:    model.CollectRecordStatusSuccess,
		StartedAt: time.Now(),
	}
	if err := gdb.Create(rec).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&legacyCollectRecordFilmItem{RecordID: rec.ID, SourceID: "s1", Mid: 777}).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateDropCollectRecordFilms(gdb); err != nil {
		t.Fatalf("first mysql migrate failed: %v", err)
	}
	if err := migrateDropCollectRecordFilms(gdb); err != nil {
		t.Fatalf("second mysql migrate failed: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}, &model.CollectRecord{}); err != nil {
		t.Fatalf("post-migration AutoMigrate failed: %v", err)
	}

	if gdb.Migrator().HasTable("collect_record_films") {
		t.Fatal("collect_record_films table should be dropped")
	}
	if gdb.Migrator().HasColumn(&model.CollectRecord{}, "ledger") {
		t.Fatal("collect_records.ledger column should be dropped")
	}
	if !gdb.Migrator().HasColumn(&model.FilmIndex{}, "last_record_id") {
		t.Fatal("film_index.last_record_id column should exist")
	}
	cols, exists, err := currentIndexColumns(gdb, false, model.TableFilmIndex, notifyFilmIndexSpec.name)
	if err != nil || !exists || !slices.Equal(cols, notifyFilmIndexSpec.cols) {
		t.Fatalf("expected index %s cols=%v, got exists=%v cols=%v err=%v", notifyFilmIndexSpec.name, notifyFilmIndexSpec.cols, exists, cols, err)
	}
	var updated model.CollectRecord
	if err := gdb.Where("id = ?", rec.ID).First(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if updated.TargetMids != "[777]" {
		t.Fatalf("expected target_mids=[777], got %q", updated.TargetMids)
	}
}

func TestIsMySQLInstantUnsupported(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"1845", &mysqldriver.MySQLError{Number: 1845, Message: "ALGORITHM=INSTANT is not supported"}, true},
		{"1846", &mysqldriver.MySQLError{Number: 1846, Message: "ALGORITHM=INSTANT is not supported. Reason: MAX_ROW_VERSION"}, true},
		{"1064", &mysqldriver.MySQLError{Number: 1064, Message: "You have an error in your SQL syntax"}, true},
		{"other_mysql", &mysqldriver.MySQLError{Number: 1205, Message: "Lock wait timeout exceeded"}, false},
		{"wrapped_text", errors.New("ALGORITHM=INSTANT is not supported for this operation"), true},
	}
	for _, tc := range cases {
		if got := isMySQLInstantUnsupported(tc.err); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
