package sourcecatalog

import (
	"context"
	"errors"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// ResolveCollectRecordLedger 按批次号解析采集记录主键与账本就绪状态。
func ResolveCollectRecordLedger(batchID string) (uint, bool, error) {
	return ResolveCollectRecordLedgerContext(context.Background(), batchID)
}

// ResolveCollectRecordLedgerContext 按批次号解析采集记录主键与账本就绪状态，支持上下文取消。
func ResolveCollectRecordLedgerContext(ctx context.Context, batchID string) (uint, bool, error) {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" || db.Mdb == nil {
		return 0, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var rec model.CollectRecord
	err := db.Mdb.WithContext(ctx).Select("id", "ledger").Where("batch_id = ?", batchID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return rec.ID, rec.Ledger, nil
}

// CollectRecordHasLedger 检查批次是否已写入 collect_record_films 账本。
func CollectRecordHasLedger(batchID string) (bool, error) {
	_, ready, err := ResolveCollectRecordLedger(batchID)
	return ready, err
}
