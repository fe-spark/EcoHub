package film

import (
	"errors"
	"fmt"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// ErrLegacyContentKeySchema 旧版 name_* 库存，需先重置再采集。
var ErrLegacyContentKeySchema = errors.New("检测到旧版库存，请先「重置站点数据」再全量采集")

var contentKeySchemaCache struct {
	mu     sync.Mutex
	known  bool
	legacy bool
}

// InvalidateContentKeySchemaCache 在清库/重置后调用，避免沿用升级前的判定结果。
func InvalidateContentKeySchemaCache() {
	contentKeySchemaCache.mu.Lock()
	defer contentKeySchemaCache.mu.Unlock()
	contentKeySchemaCache.known = false
	contentKeySchemaCache.legacy = false
}

// HasLegacyContentKeyInventory 是否存在旧版主站身份键：
// content_key 为 name_* 且 mid>0。v1.1.5 起有 id 的主站行应使用 vod_*。
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

// EnsureContentKeySchemaReady 主站写入/主站采集前调用。
// 存在旧版 name_* 库存时返回 ErrLegacyContentKeySchema，避免 mid 唯一键冲突。
func EnsureContentKeySchemaReady(gdb *gorm.DB) error {
	contentKeySchemaCache.mu.Lock()
	if contentKeySchemaCache.known {
		legacy := contentKeySchemaCache.legacy
		contentKeySchemaCache.mu.Unlock()
		if legacy {
			return ErrLegacyContentKeySchema
		}
		return nil
	}
	contentKeySchemaCache.mu.Unlock()

	legacy, err := HasLegacyContentKeyInventory(gdb)
	if err != nil {
		return fmt.Errorf("库存检查失败: %w", err)
	}

	contentKeySchemaCache.mu.Lock()
	contentKeySchemaCache.known = true
	contentKeySchemaCache.legacy = legacy
	contentKeySchemaCache.mu.Unlock()

	if legacy {
		return ErrLegacyContentKeySchema
	}
	return nil
}

