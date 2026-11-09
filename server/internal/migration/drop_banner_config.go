package migration

import (
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"

	"gorm.io/gorm"
)

// legacyBannerConfigTable 旧版一键排片配置表。排片配置改为每次排片时在弹窗里确认，不再保存。
const legacyBannerConfigTable = "banner_config"

// legacyBannerConfigCacheKey 旧版排片配置的 Redis 缓存键。
const legacyBannerConfigCacheKey = config.RedisKeyPrefix + ":Config:BannerConfig"

// migrateDropBannerConfig 删除旧版排片配置表和缓存。表里的配置不迁移，删除后无法恢复。
func migrateDropBannerConfig(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	if gdb.Migrator().HasTable(legacyBannerConfigTable) {
		if err := gdb.Migrator().DropTable(legacyBannerConfigTable); err != nil {
			return err
		}
	}
	if db.Rdb != nil {
		if err := db.Rdb.Del(db.Cxt, legacyBannerConfigCacheKey).Err(); err != nil {
			// 缓存删不掉不影响启动，旧键已经没有代码读取。
			syslog.Errorf("[Migration] 删除旧排片配置缓存失败: %v", err)
		}
	}
	return nil
}
