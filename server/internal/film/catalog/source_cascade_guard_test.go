package catalog

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm/logger"
	"server/internal/infra/db"
	"server/internal/testutil"
	"testing"

	"server/internal/model"

	"gorm.io/gorm"
)

func TestAssertSourceDataCleanedTx_CatchesRemainingData(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	origMdb := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = origMdb })
	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.MovieSourceMapping{},
	); err != nil {
		t.Fatal(err)
	}

	srcID := "src_guard_test"

	// 1. 无任何数据时断言通过
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		return AssertSourceDataCleanedTx(tx, srcID)
	}); err != nil {
		t.Fatalf("expected nil error on empty data, got %v", err)
	}

	// 2. 残留 FilmIndex 归属档案时阻断
	film := &model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 901, FirstSourceId: srcID},
		FilmIndexContent:  model.FilmIndexContent{Name: "测试影片"},
	}
	if err := gdb.Create(film).Error; err != nil {
		t.Fatal(err)
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		return AssertSourceDataCleanedTx(tx, srcID)
	})
	if err == nil || !contains(err.Error(), "影片档案未清理完毕") {
		t.Fatalf("expected film_index guard error, got %v", err)
	}
	gdb.Unscoped().Delete(film)

	// 3. 残留 FilmSourcePlaylist 播放列表时阻断
	playlist := &model.FilmSourcePlaylist{
		Mid:      902,
		SourceId: srcID,
		LineKind: "play",
		Content:  "[]",
	}
	if err := gdb.Create(playlist).Error; err != nil {
		t.Fatal(err)
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		return AssertSourceDataCleanedTx(tx, srcID)
	})
	if err == nil || !contains(err.Error(), "播放列表未清理完毕") {
		t.Fatalf("expected playlist guard error, got %v", err)
	}
	gdb.Delete(playlist)

	// 4. 残留 MovieSourceMapping 映射时阻断
	mapping := &model.MovieSourceMapping{
		SourceId:  srcID,
		SourceMid: 12345,
		GlobalMid: 903,
	}
	if err := gdb.Create(mapping).Error; err != nil {
		t.Fatal(err)
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		return AssertSourceDataCleanedTx(tx, srcID)
	})
	if err == nil || !contains(err.Error(), "影片映射未清理完毕") {
		t.Fatalf("expected mapping guard error, got %v", err)
	}
	gdb.Delete(mapping)

	// 5. 清理完毕后通过
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		return AssertSourceDataCleanedTx(tx, srcID)
	}); err != nil {
		t.Fatalf("expected clean data to pass guard, got %v", err)
	}
}

func TestDelCollectResource_PreservesSourceWhenRemainingDataDetected(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	origMdb := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = origMdb })
	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.MovieSourceMapping{},
		&model.CollectSourceStats{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
	); err != nil {
		t.Fatal(err)
	}

	src := model.FilmSource{
		Id:   "src_preserve_test",
		Name: "不能消失的站",
		Uri:  "http://preserve.example.com",
	}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}

	// 模拟在非 first_source_id 存在其他归属的映射数据或者有并发插入
	// 无论发生什么，只要数据库存在未清理的数据，DelCollectResource 的最终事务断言必须保证站点行不被删除
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		// 故意插入一条关联数据测试门禁
		m := &model.MovieSourceMapping{
			SourceId:  src.Id,
			SourceMid: 888,
			GlobalMid: 888,
		}
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		if err := AssertSourceDataCleanedTx(tx, src.Id); err != nil {
			return err
		}
		return tx.Where("id = ?", src.Id).Delete(&model.FilmSource{}).Error
	}); err == nil {
		t.Fatal("expected transaction rollback due to remaining data")
	}

	// 验证站点仍然完好无损地保存在数据库中
	var count int64
	gdb.Model(&model.FilmSource{}).Where("id = ?", src.Id).Count(&count)
	if count != 1 {
		t.Fatalf("film source must NOT be deleted when remaining data exists, count=%d", count)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) > 0 && len(s) > 0 && (s[:len(substr)] == substr || containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
