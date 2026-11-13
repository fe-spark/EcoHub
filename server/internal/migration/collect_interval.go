package migration

import (
	"fmt"

	"server/internal/model"

	"gorm.io/gorm"
)

// migrateCollectInterval 增加抓页间隔，默认 200 毫秒。
// 仍为 0 的旧站改成 200。已经填过的非 0 间隔不动。之后再保存 0 仍表示不等待。
func migrateCollectInterval(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}); err != nil {
		return err
	}
	if err := setCollectIntervalDefault(gdb); err != nil {
		return err
	}
	return gdb.Model(&model.FilmSource{}).
		Where("collect_interval_ms IS NULL OR collect_interval_ms = ?", 0).
		Update("collect_interval_ms", model.CollectIntervalMsDefault).Error
}

func setCollectIntervalDefault(gdb *gorm.DB) error {
	if gdb.Dialector.Name() == "mysql" {
		stmt := fmt.Sprintf(
			"ALTER TABLE %s ALTER COLUMN collect_interval_ms SET DEFAULT %d",
			model.TableFilmSource,
			model.CollectIntervalMsDefault,
		)
		return gdb.Exec(stmt).Error
	}
	return gdb.Migrator().AlterColumn(&model.FilmSource{}, "CollectIntervalMs")
}
