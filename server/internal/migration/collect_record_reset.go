package migration

import (
	"server/internal/model"

	"gorm.io/gorm"
)

// migrateCollectRecordReset 采集记录不再保留旧行。两张表按当前模型重建。
func migrateCollectRecordReset(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	if err := gdb.Migrator().DropTable(&model.CollectRecordFilmItem{}, &model.CollectRecord{}); err != nil {
		return err
	}
	return gdb.AutoMigrate(&model.CollectRecord{}, &model.CollectRecordFilmItem{})
}
