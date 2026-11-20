package repository

import (
	"testing"
	"time"

	"server/internal/model"

	"gorm.io/gorm"
)

func TestAssertSourceDataCleanedTx_CatchesRemainingData(t *testing.T) {
	gdb := setupTestDB(t)
	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.MovieSourceMapping{},
		&model.CollectRecordFilmItem{},
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
	err := gdb.Transaction(func(tx *gorm.DB) error {
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

	// 5. 残留 CollectRecordFilmItem 清单时阻断
	item := &model.CollectRecordFilmItem{
		BatchID:   "batch-1",
		SourceID:  srcID,
		Mid:       904,
		CreatedAt: time.Now(),
	}
	if err := gdb.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		return AssertSourceDataCleanedTx(tx, srcID)
	})
	if err == nil || !contains(err.Error(), "采集记录清单未清理完毕") {
		t.Fatalf("expected collect record film item guard error, got %v", err)
	}
	gdb.Delete(item)

	// 6. 清理完毕后通过
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		return AssertSourceDataCleanedTx(tx, srcID)
	}); err != nil {
		t.Fatalf("expected clean data to pass guard, got %v", err)
	}
}

func TestDelCollectResource_PreservesSourceWhenRemainingDataDetected(t *testing.T) {
	gdb := setupTestDB(t)
	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.MovieSourceMapping{},
		&model.CollectRecordFilmItem{},
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
		item := &model.CollectRecordFilmItem{
			BatchID:   "batch-leak",
			SourceID:  src.Id,
			Mid:       888,
			CreatedAt: time.Now(),
		}
		if err := tx.Create(item).Error; err != nil {
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

// 验证历史软删除的影片档案（deleted_at IS NOT NULL）也能被分批物理清理干净
func TestDelCollectResource_CleansSoftDeletedFilmsPhysically(t *testing.T) {
	gdb := setupTestDB(t)
	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatal(err)
	}

	src := model.FilmSource{
		Id:    "src_soft_del_test",
		Name:  "SoftDelTest",
		Uri:   "http://soft.test",
		State: true,
	}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}

	film := &model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:           9999,
			FirstSourceId: src.Id,
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "已软删除影片",
		},
	}
	if err := gdb.Create(film).Error; err != nil {
		t.Fatal(err)
	}
	// 模拟被 GORM 软删除
	if err := gdb.Delete(film).Error; err != nil {
		t.Fatal(err)
	}

	// 确认此时数据库存在 soft deleted 记录
	var softCount int64
	gdb.Unscoped().Model(&model.FilmIndex{}).Where("mid = ?", 9999).Count(&softCount)
	if softCount != 1 {
		t.Fatalf("expected 1 soft deleted film, got %d", softCount)
	}

	// 执行删站
	if _, err := DelCollectResource(src.Id); err != nil {
		t.Fatalf("DelCollectResource failed: %v", err)
	}

	// 断言物理记录已被彻底清除
	gdb.Unscoped().Model(&model.FilmIndex{}).Where("mid = ?", 9999).Count(&softCount)
	if softCount != 0 {
		t.Fatalf("soft deleted film must be physically deleted, remaining=%d", softCount)
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
