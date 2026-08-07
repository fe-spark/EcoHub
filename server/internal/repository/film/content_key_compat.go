package film

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// ErrLegacyContentKeySchema 迁移后仍残留 name_* 时的兜底错误。
var ErrLegacyContentKeySchema = errors.New("旧库存未能完全迁移，请重置站点数据或查看服务日志")

const (
	// ContentKeyMigrationNoticeKey 后台公告：已自动迁移，建议全量采集。
	ContentKeyMigrationNoticeKey = config.RedisKeyPrefix + ":Notice:ContentKeyMigrated"
	// ContentKeyMigrationFailedKey 后台公告：启动/写库迁移失败。
	ContentKeyMigrationFailedKey = config.RedisKeyPrefix + ":Notice:ContentKeyMigrateFailed"
	// contentKeyMigrationNoticeTTL 迁移成功提示保留时长（避免永久黄条）。
	contentKeyMigrationNoticeTTL = 14 * 24 * time.Hour
	// contentKeyMigrateBatch 单次加载待迁移行上限（防超大库一次占内存）。
	contentKeyMigrateBatch = 2000
)

var contentKeySchemaCache struct {
	mu     sync.Mutex
	known  bool
	legacy bool
}

// migrateContentKeyMu 串行化迁移：启动、后台入口、写库可能并发调用，
// 避免多个事务同时改写同一批 content_key 造成锁竞争。
var migrateContentKeyMu sync.Mutex

// InvalidateContentKeySchemaCache 清库/迁移后调用。
func InvalidateContentKeySchemaCache() {
	contentKeySchemaCache.mu.Lock()
	defer contentKeySchemaCache.mu.Unlock()
	contentKeySchemaCache.known = false
	contentKeySchemaCache.legacy = false
}

func setContentKeySchemaCache(legacy bool) {
	contentKeySchemaCache.mu.Lock()
	defer contentKeySchemaCache.mu.Unlock()
	contentKeySchemaCache.known = true
	contentKeySchemaCache.legacy = legacy
}

// HasLegacyContentKeyInventory 是否仍存在 name_* 且 mid>0（仅统计未软删行）。
func HasLegacyContentKeyInventory(gdb *gorm.DB) (bool, error) {
	if gdb == nil {
		gdb = db.Mdb
	}
	if gdb == nil {
		return false, fmt.Errorf("database not ready")
	}
	var n int64
	err := gdb.Model(&model.FilmIndex{}).
		Where("content_key LIKE ? AND mid > ?", contentKeyNamePrefix+"%", 0).
		Limit(1).
		Count(&n).Error
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// HasContentKeyMigrationNotice 是否展示「已迁移、建议全量采集」公告。
func HasContentKeyMigrationNotice() bool {
	return redisKeyExists(ContentKeyMigrationNoticeKey)
}

// HasContentKeyMigrationFailed 是否展示迁移失败公告。
func HasContentKeyMigrationFailed() bool {
	return redisKeyExists(ContentKeyMigrationFailedKey)
}

func redisKeyExists(key string) bool {
	if db.Rdb == nil {
		return false
	}
	n, err := db.Rdb.Exists(db.Cxt, key).Result()
	return err == nil && n > 0
}

// ClearContentKeyMigrationNotice 重置站点 / 主站全量成功后清除「已迁移」公告。
func ClearContentKeyMigrationNotice() {
	redisDel(ContentKeyMigrationNoticeKey)
}

// ClearContentKeyMigrationFailed 迁移成功或清库后清除失败标记。
func ClearContentKeyMigrationFailed() {
	redisDel(ContentKeyMigrationFailedKey)
}

func redisDel(key string) {
	if db.Rdb == nil {
		return
	}
	_ = db.Rdb.Del(db.Cxt, key).Err()
}

func markContentKeyMigrationNotice() {
	if db.Rdb == nil {
		return
	}
	_ = db.Rdb.Set(db.Cxt, ContentKeyMigrationNoticeKey, time.Now().UTC().Format(time.RFC3339), contentKeyMigrationNoticeTTL).Err()
}

func markContentKeyMigrationFailed(reason string) {
	if db.Rdb == nil {
		return
	}
	msg := strings.TrimSpace(reason)
	if msg == "" {
		msg = "unknown"
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	_ = db.Rdb.Set(db.Cxt, ContentKeyMigrationFailedKey, msg, contentKeyMigrationNoticeTTL).Err()
}

// MigrateLegacyContentKeys 将 name_* + mid>0 改写为 vod_{mid}（幂等，可移植 GORM 实现）。
// 1) 释放软删行占用的 vod_{mid} 唯一键；2) 迁移活跃 name_*；3) 同步快照 content_key。
// 返回成功改写的 film_index 活跃行数。
func MigrateLegacyContentKeys(gdb *gorm.DB) (int64, error) {
	// 全局串行：并发调用方（启动/后台/写库）排队执行，后到者幂等快速返回。
	migrateContentKeyMu.Lock()
	defer migrateContentKeyMu.Unlock()

	if gdb == nil {
		gdb = db.Mdb
	}
	if gdb == nil {
		return 0, fmt.Errorf("database not ready")
	}

	legacy, err := HasLegacyContentKeyInventory(gdb)
	if err != nil {
		return 0, err
	}
	if !legacy {
		InvalidateContentKeySchemaCache()
		ClearContentKeyMigrationFailed()
		setContentKeySchemaCache(false)
		return 0, nil
	}

	var indexUpdated int64
	log.Printf("[ContentKey] migrate start: name_* rows pending, running %s batch path", gdb.Dialector.Name())
	err = gdb.Transaction(func(tx *gorm.DB) error {
		n, err := migrateLegacyFilmIndexesTx(tx)
		if err != nil {
			return err
		}
		indexUpdated = n
		if err := migrateSnapshotContentKeysTx(tx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		markContentKeyMigrationFailed(err.Error())
		InvalidateContentKeySchemaCache()
		return 0, err
	}

	InvalidateContentKeySchemaCache()
	still, stillErr := HasLegacyContentKeyInventory(gdb)
	if stillErr != nil {
		markContentKeyMigrationFailed(stillErr.Error())
		return indexUpdated, stillErr
	}
	setContentKeySchemaCache(still)

	if indexUpdated > 0 {
		markContentKeyMigrationNotice()
		ClearContentKeyMigrationFailed()
		log.Printf("[ContentKey] migrated film_index rows=%d name_* -> vod_{mid}", indexUpdated)
	}
	if still {
		log.Printf("[ContentKey] residual name_* rows remain after migrate (target key occupied)")
		if indexUpdated == 0 {
			markContentKeyMigrationFailed("residual name_* after migrate")
		}
	} else {
		ClearContentKeyMigrationFailed()
	}
	return indexUpdated, nil
}

// migrateLegacyFilmIndexesTx 按方言分发：
//   - MySQL 生产路径：批量 SQL（软删释放 + 分块 UPDATE），百万级库存秒级完成；
//   - 其它（SQLite 单测）：逐行兼容实现。
func migrateLegacyFilmIndexesTx(tx *gorm.DB) (int64, error) {
	if tx.Dialector.Name() == "mysql" {
		return migrateLegacyFilmIndexesMysqlTx(tx)
	}
	return migrateLegacyFilmIndexesGenericTx(tx)
}

// migrateLegacyFilmIndexesMysqlTx MySQL 批量迁移，避免百万行逐行 SQL。
// 1) 软删行占用目标 vod_{mid} → 改 del_{id} 释放唯一键；
// 2) 按 id 分块 UPDATE name_* → vod_{mid}（目标被任意行占用则跳过）。
func migrateLegacyFilmIndexesMysqlTx(tx *gorm.DB) (int64, error) {
	now := time.Now().Format("2006-01-02 15:04:05")

	// 软删占用目标 key：改名为 del_{id}（self-join，一条 SQL）。
	if err := tx.Exec(`UPDATE film_index AS t
INNER JOIN film_index AS live
  ON live.content_key LIKE ?
 AND live.mid > 0
 AND live.deleted_at IS NULL
 AND t.content_key = CONCAT(?, live.mid)
 AND t.id <> live.id
SET t.content_key = CONCAT('del_', t.id), t.updated_at = ?
WHERE t.deleted_at IS NOT NULL`,
		contentKeyNamePrefix+"%", contentKeyVodPrefix, now).Error; err != nil {
		return 0, err
	}

	// 分块迁移：避免单条 UPDATE 锁全部行/巨型 undo。
	var maxID uint
	if err := tx.Model(&model.FilmIndex{}).Select("IFNULL(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		return 0, err
	}
	var updated int64
	const blockStep = 100000
	for lo := uint(0); lo < maxID; lo += blockStep {
		hi := lo + blockStep
		res := tx.Exec(`UPDATE film_index AS fi
SET fi.content_key = CONCAT(?, fi.mid), fi.updated_at = ?
WHERE fi.content_key LIKE ?
  AND fi.mid > 0
  AND fi.deleted_at IS NULL
  AND fi.id > ? AND fi.id <= ?
  AND NOT EXISTS (
    SELECT 1 FROM film_index AS o
    WHERE o.content_key = CONCAT(?, fi.mid) AND o.id <> fi.id
  )`,
			contentKeyVodPrefix, now, contentKeyNamePrefix+"%", lo, hi, contentKeyVodPrefix)
		if res.Error != nil {
			return updated, res.Error
		}
		if res.RowsAffected > 0 {
			updated += res.RowsAffected
			log.Printf("[ContentKey] migrate progress id<=%d rows=%d total=%d", hi, res.RowsAffected, updated)
		}
	}
	return updated, nil
}

// migrateLegacyFilmIndexesGenericTx 逐行迁移（SQLite 单测路径）。
func migrateLegacyFilmIndexesGenericTx(tx *gorm.DB) (int64, error) {
	var updated int64
	// 目标 key 被活跃行占用而无法迁移的行：后续批次不再重复扫描。
	var blockedIDs []uint
	for {
		q := tx.Model(&model.FilmIndex{}).
			Where("content_key LIKE ? AND mid > ?", contentKeyNamePrefix+"%", 0)
		if len(blockedIDs) > 0 {
			q = q.Where("id NOT IN ?", blockedIDs)
		}
		var lives []model.FilmIndex
		if err := q.Order("id ASC").
			Limit(contentKeyMigrateBatch).
			Find(&lives).Error; err != nil {
			return updated, err
		}
		if len(lives) == 0 {
			return updated, nil
		}

		batchMoved := 0
		for i := range lives {
			live := &lives[i]
			newKey := fmt.Sprintf("%s%d", contentKeyVodPrefix, live.Mid)

			// 软删占用目标 key：改名为 del_{id}，释放唯一索引（含软删行）。
			var tombs []model.FilmIndex
			if err := tx.Unscoped().Model(&model.FilmIndex{}).
				Where("content_key = ? AND id <> ? AND deleted_at IS NOT NULL", newKey, live.ID).
				Find(&tombs).Error; err != nil {
				return updated, err
			}
			for _, tomb := range tombs {
				if err := tx.Unscoped().Model(&model.FilmIndex{}).
					Where("id = ?", tomb.ID).
					Update("content_key", fmt.Sprintf("del_%d", tomb.ID)).Error; err != nil {
					return updated, err
				}
			}

			// 仍有任意行（含活跃）占用目标 key → 跳过本行并记录，避免反复扫描
			var occupy int64
			if err := tx.Unscoped().Model(&model.FilmIndex{}).
				Where("content_key = ? AND id <> ?", newKey, live.ID).
				Limit(1).
				Count(&occupy).Error; err != nil {
				return updated, err
			}
			if occupy > 0 {
				blockedIDs = append(blockedIDs, live.ID)
				continue
			}

			if err := tx.Model(&model.FilmIndex{}).
				Where("id = ? AND content_key LIKE ?", live.ID, contentKeyNamePrefix+"%").
				Update("content_key", newKey).Error; err != nil {
				return updated, err
			}
			updated++
			batchMoved++
		}

		// 本批无人可迁（全是目标 key 被活跃行占用）→ 结束，避免死循环
		if batchMoved == 0 {
			return updated, nil
		}
	}
}

func migrateSnapshotContentKeysTx(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&model.FilmListSnapshot{}) {
		return nil
	}
	// MySQL：批量更新（快照 content_key 非唯一约束）。
	if tx.Dialector.Name() == "mysql" {
		return tx.Exec(`UPDATE film_list_snapshot
SET content_key = CONCAT(?, mid), updated_at = ?
WHERE content_key LIKE ? AND mid > 0`,
			contentKeyVodPrefix, time.Now().Format("2006-01-02 15:04:05"), contentKeyNamePrefix+"%").Error
	}
	// SQLite 单测路径：逐行。
	var snaps []model.FilmListSnapshot
	if err := tx.Model(&model.FilmListSnapshot{}).
		Where("content_key LIKE ? AND mid > ?", contentKeyNamePrefix+"%", 0).
		Find(&snaps).Error; err != nil {
		return err
	}
	for i := range snaps {
		s := &snaps[i]
		newKey := fmt.Sprintf("%s%d", contentKeyVodPrefix, s.Mid)
		if err := tx.Model(&model.FilmListSnapshot{}).
			Where("id = ?", s.ID).
			Update("content_key", newKey).Error; err != nil {
			return err
		}
	}
	return nil
}

// EnsureContentKeySchemaReady 写库前：尽量迁移；仍有 name_* 则拒绝写入。
func EnsureContentKeySchemaReady(gdb *gorm.DB) error {
	contentKeySchemaCache.mu.Lock()
	if contentKeySchemaCache.known && contentKeySchemaCache.legacy {
		contentKeySchemaCache.mu.Unlock()
		return ErrLegacyContentKeySchema
	}
	if contentKeySchemaCache.known && !contentKeySchemaCache.legacy {
		contentKeySchemaCache.mu.Unlock()
		legacy, err := HasLegacyContentKeyInventory(gdb)
		if err != nil {
			return fmt.Errorf("库存检查失败: %w", err)
		}
		if !legacy {
			return nil
		}
		InvalidateContentKeySchemaCache()
	} else {
		contentKeySchemaCache.mu.Unlock()
	}

	if _, err := MigrateLegacyContentKeys(gdb); err != nil {
		return fmt.Errorf("库存迁移失败: %w", err)
	}

	legacy, err := HasLegacyContentKeyInventory(gdb)
	if err != nil {
		return fmt.Errorf("库存检查失败: %w", err)
	}
	setContentKeySchemaCache(legacy)
	if legacy {
		return ErrLegacyContentKeySchema
	}
	return nil
}

// AdminContentKeyNotice 供后台 ManageIndex 使用。
type AdminContentKeyNotice struct {
	Level      string
	Code       string
	Message    string
	ActionPath string
	ActionText string
}

// ListAdminContentKeyNotices 进入后台时的公告列表（先尝试迁移再展示）。
func ListAdminContentKeyNotices() []AdminContentKeyNotice {
	out := make([]AdminContentKeyNotice, 0, 2)

	if _, err := MigrateLegacyContentKeys(nil); err != nil {
		log.Printf("[ContentKey] admin migrate: %v", err)
		out = append(out, AdminContentKeyNotice{
			Level:      "error",
			Code:       "content_key_migrate_failed",
			Message:    "旧库存自动迁移失败，请查看服务日志或重置站点数据",
			ActionPath: "/manage/system/website",
			ActionText: "去重置",
		})
		return out
	}

	if HasContentKeyMigrationFailed() {
		out = append(out, AdminContentKeyNotice{
			Level:      "error",
			Code:       "content_key_migrate_failed",
			Message:    "旧库存自动迁移失败，请查看服务日志或重置站点数据",
			ActionPath: "/manage/system/website",
			ActionText: "去重置",
		})
	}

	if legacy, err := HasLegacyContentKeyInventory(nil); err == nil && legacy {
		out = append(out, AdminContentKeyNotice{
			Level:      "error",
			Code:       "legacy_content_key",
			Message:    "部分旧库存未能自动迁移，请重置站点数据或查看日志",
			ActionPath: "/manage/system/website",
			ActionText: "去重置",
		})
		return out
	}

	if HasContentKeyMigrationNotice() {
		out = append(out, AdminContentKeyNotice{
			Level:      "warning",
			Code:       "content_key_migrated",
			Message:    "主站身份键已自动迁移，建议主站全量采集以补齐历史误合并影片",
			ActionPath: "/manage/collect",
			ActionText: "去采集",
		})
	}
	return out
}
