package sourcecatalog

import (
	"context"
	"errors"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// ResolveCollectRecordID 按批次号解析采集记录主键。
func ResolveCollectRecordID(batchID string) (uint, error) {
	return ResolveCollectRecordIDContext(context.Background(), batchID)
}

// ResolveCollectRecordIDContext 按批次号解析采集记录主键，支持上下文取消。
func ResolveCollectRecordIDContext(ctx context.Context, batchID string) (uint, error) {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" || db.Mdb == nil {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var rec model.CollectRecord
	err := db.Mdb.WithContext(ctx).Select("id").Where("batch_id = ?", batchID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return rec.ID, nil
}
