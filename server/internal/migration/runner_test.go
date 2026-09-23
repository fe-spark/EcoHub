package migration

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRunAutoMigrations(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test sqlite: %v", err)
	}

	origMdb := db.Mdb
	db.Mdb = testDB
	defer func() {
		db.Mdb = origMdb
	}()

	// 1. 初始化模型
	if err := testDB.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// 2. 首次执行迁移
	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("RunAutoMigrations first run failed: %v", err)
	}

	// 3. 验证 schema_migrations 记录数
	var count int64
	if err := testDB.Model(&model.SchemaMigration{}).Count(&count).Error; err != nil {
		t.Fatalf("Count schema_migrations failed: %v", err)
	}
	if count != int64(len(migrations)) {
		t.Fatalf("expected %d migrations recorded, got %d", len(migrations), count)
	}

	// 4. 二次执行迁移（测试幂等性与跳过逻辑）
	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("RunAutoMigrations second run failed: %v", err)
	}

	// 再次验证记录数没有增加
	var countAfter int64
	if err := testDB.Model(&model.SchemaMigration{}).Count(&countAfter).Error; err != nil {
		t.Fatalf("Count schema_migrations failed: %v", err)
	}
	if countAfter != count {
		t.Fatalf("expected count to remain %d, got %d", count, countAfter)
	}
}

func TestDropSnapDeletedAtIndex_AlwaysDroppedAfterAutoMigrate(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test sqlite: %v", err)
	}

	origMdb := db.Mdb
	db.Mdb = testDB
	defer func() {
		db.Mdb = origMdb
	}()

	if err := testDB.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}
	if testDB.Migrator().HasIndex(&model.FilmListSnapshot{}, "idx_film_list_snapshot_deleted_at") {
		t.Fatal("AutoMigrate must not create idx_film_list_snapshot_deleted_at")
	}
	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("RunAutoMigrations failed: %v", err)
	}

	if err := testDB.Exec("CREATE INDEX idx_film_list_snapshot_deleted_at ON film_list_snapshot(deleted_at)").Error; err != nil {
		t.Fatalf("create leftover index: %v", err)
	}
	if !testDB.Migrator().HasIndex(&model.FilmListSnapshot{}, "idx_film_list_snapshot_deleted_at") {
		t.Fatal("expected leftover deleted_at index")
	}

	if err := testDB.AutoMigrate(&model.FilmListSnapshot{}); err != nil {
		t.Fatalf("second AutoMigrate failed: %v", err)
	}
	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("RunAutoMigrations second run failed: %v", err)
	}
	if testDB.Migrator().HasIndex(&model.FilmListSnapshot{}, "idx_film_list_snapshot_deleted_at") {
		t.Fatal("leftover deleted_at index should be dropped on every RunAutoMigrations")
	}
}

func TestMigrateAddFilmSourceCreatedAtColumn_BackfillExisting(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test sqlite: %v", err)
	}

	// 模拟升级前已创建表，且插入没有 created_at 时间的历史采集站数据
	if err := testDB.AutoMigrate(&model.FilmSource{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	if err := testDB.Exec("INSERT INTO film_sources (id, name, uri, grade, state) VALUES ('s1', 'Master', 'https://m.com', 1, 1), ('s2', 'Slave1', 'https://s1.com', 2, 1), ('s3', 'Slave2', 'https://s2.com', 2, 1)").Error; err != nil {
		t.Fatalf("insert existing sources failed: %v", err)
	}

	// 执行回填迁移
	if err := migrateAddFilmSourceCreatedAtColumn(testDB); err != nil {
		t.Fatalf("migrateAddFilmSourceCreatedAtColumn failed: %v", err)
	}

	var list []model.FilmSource
	if err := testDB.Order("grade ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		t.Fatalf("query list failed: %v", err)
	}

	if len(list) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(list))
	}

	for i, s := range list {
		if s.CreatedAt.IsZero() {
			t.Fatalf("expected source %s CreatedAt to be non-zero, got zero", s.Id)
		}
		if i > 0 && !s.CreatedAt.After(list[i-1].CreatedAt) {
			t.Fatalf("expected source %d CreatedAt (%v) to be strictly after previous source (%v)", i, s.CreatedAt, list[i-1].CreatedAt)
		}
	}
}

