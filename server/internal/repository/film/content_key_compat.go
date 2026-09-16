package film

import (
	"server/internal/config"
	"server/internal/infra/db"
)

// ContentKey 兼容策略（自 v1.1.5-beta.5）：
//
//  不在启动时 bulk 改写旧库 name_* 行。
//  读路径按 mid / 分类等字段，不依赖 content_key 前缀。
//  写路径按 mid 冲突更新（filmIndexMidUpsert）；重采时即使业务无变更也会懒升 content_key→vod_*。
//  未再采集的旧行保持 name_*，展示与播放不受影响。
//  历史误合并（多 vod 共用同一 name_* 行）需再次采到对应 vod 后才会拆开。

// 旧版 bulk 迁移曾写入的 Redis 公告 key（仅清理，不再读写业务逻辑）。
const (
	contentKeyMigrationNoticeKey = config.RedisKeyPrefix + ":Notice:ContentKeyMigrated"
	contentKeyMigrationFailedKey = config.RedisKeyPrefix + ":Notice:ContentKeyMigrateFailed"
)

// ClearLegacyContentKeyNotices 清库或升级后清理遗留迁移公告 key。
func ClearLegacyContentKeyNotices() {
	if db.Rdb == nil {
		return
	}
	_ = db.Rdb.Del(db.Cxt, contentKeyMigrationNoticeKey, contentKeyMigrationFailedKey).Err()
}
