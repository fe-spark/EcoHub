package migration

import (
	"server/internal/model"

	"gorm.io/gorm"
)

// migrateSchemaCleanup 执行破坏性表结构清理与重建（不迁移旧数据）：
// 1. 按聚簇主键 (record_id, source_id, mid) 清空重建 collect_record_films，并关闭存量记录的 ledger 标记；
// 2. 删除 home_recommend_group 废弃的 builtin / builtin_key 列。
func migrateSchemaCleanup(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	if err := gdb.Migrator().DropTable(&model.CollectRecordFilmItem{}); err != nil {
		return err
	}
	if err := gdb.AutoMigrate(&model.CollectRecordFilmItem{}); err != nil {
		return err
	}
	if gdb.Migrator().HasTable(&model.CollectRecord{}) {
		if err := gdb.Model(&model.CollectRecord{}).
			Where("ledger = ?", true).
			Update("ledger", false).Error; err != nil {
			return err
		}
	}
	if gdb.Migrator().HasTable(&model.HomeRecommendGroup{}) {
		// 从 v3.1.7 升级时，删除 builtin 列前先清理废弃的旧内置分组行，避免升级后残留空壳分组导致首页空白。
		if gdb.Migrator().HasColumn(&model.HomeRecommendGroup{}, "builtin") {
			var builtinIDs []string
			if err := gdb.Table(model.TableHomeRecommendGroup).Where("builtin = ?", true).Pluck("id", &builtinIDs).Error; err != nil {
				return err
			}
			if len(builtinIDs) > 0 {
				if gdb.Migrator().HasTable(&model.HomeRecommendItem{}) {
					if err := gdb.Table(model.TableHomeRecommendItem).Where("group_id IN ?", builtinIDs).Delete(&model.HomeRecommendItem{}).Error; err != nil {
						return err
					}
				}
				if err := gdb.Table(model.TableHomeRecommendGroup).Where("id IN ?", builtinIDs).Delete(&model.HomeRecommendGroup{}).Error; err != nil {
					return err
				}
			}
		}
		for _, col := range []string{"builtin", "builtin_key"} {
			if gdb.Migrator().HasColumn(&model.HomeRecommendGroup{}, col) {
				if err := gdb.Exec("ALTER TABLE " + model.TableHomeRecommendGroup + " DROP COLUMN " + col).Error; err != nil {
					return err
				}
			}
		}
	}
	return nil
}
