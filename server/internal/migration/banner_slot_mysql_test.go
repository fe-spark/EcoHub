package migration

import (
	"os"
	"testing"
	"time"

	"server/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// openTestMySQL 设置 ECOHUB_TEST_MYSQL_DSN 时连真实 MySQL（会重建用到的表，多个包一起跑时加 -p 1），否则跳过。
func openTestMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("ECOHUB_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("ECOHUB_TEST_MYSQL_DSN 未设置，跳过 MySQL 测试")
	}
	gdb, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	return gdb
}

// MySQL 上 DROP TABLE 会隐式提交。模拟上次回填已写入、旧表未删、迁移未记完成，重跑应成功。
func TestMigrateBannerSlot_MySQLRerun(t *testing.T) {
	gdb := openTestMySQL(t)
	_ = gdb.Migrator().DropTable(&model.BannerSlot{}, &model.FilmIndex{}, &model.FilmSourcePlaylist{}, &model.FilmSource{}, legacyBannerTable)
	if err := gdb.AutoMigrate(&model.FilmIndex{}, &model.FilmSourcePlaylist{}, &model.FilmSource{}, &legacyBannerRow{}); err != nil {
		t.Fatal(err)
	}
	gdb.Create(&model.FilmSource{Id: "s1", Uri: "http://s1.test", State: true, Sort: 1, CreatedAt: time.Now()})
	gdb.Create(&model.FilmIndex{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "s1"}})
	gdb.Create(&model.FilmIndex{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "s1"}})
	gdb.Create(&[]legacyBannerRow{{Id: "a", Mid: 1, Sort: 1}, {Id: "b", Mid: 2, Sort: 2}})

	if err := migrateBannerSlot(gdb); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// 还原成“回填已提交、旧表还在”的状态。
	if err := gdb.AutoMigrate(&legacyBannerRow{}); err != nil {
		t.Fatal(err)
	}
	gdb.Create(&[]legacyBannerRow{{Id: "a", Mid: 1, Sort: 1}, {Id: "b", Mid: 2, Sort: 2}})
	var before int64
	gdb.Model(&model.BannerSlot{}).Count(&before)
	if before != 2 {
		t.Fatalf("setup: want 2 slots, got %d", before)
	}
	if err := migrateBannerSlot(gdb); err != nil {
		t.Fatalf("rerun should succeed on MySQL: %v", err)
	}
	var after int64
	gdb.Model(&model.BannerSlot{}).Count(&after)
	if after != 2 || gdb.Migrator().HasTable(legacyBannerTable) {
		t.Fatalf("rerun result: slots=%d legacy=%v", after, gdb.Migrator().HasTable(legacyBannerTable))
	}
}
