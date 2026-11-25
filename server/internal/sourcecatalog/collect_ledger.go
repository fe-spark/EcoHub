package sourcecatalog

import (
	"errors"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// CollectRecordHasLedger 检查批次是否已写入 collect_record_films 账本。
func CollectRecordHasLedger(batchID string) (bool, error) {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" || db.Mdb == nil {
		return false, nil
	}
	var rec model.CollectRecord
	err := db.Mdb.Select("ledger").Where("batch_id = ?", batchID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if rec.Ledger {
		return true, nil
	}
	return MarkCollectRecordLedgerIfPresent(batchID)
}

// MarkCollectRecordLedgerIfPresent 终态已记下部数，但清单标记没写上。表里已经有编号就补上。
func MarkCollectRecordLedgerIfPresent(batchID string) (bool, error) {
	if db.Mdb == nil {
		return false, nil
	}
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return false, nil
	}
	var n int64
	if err := db.Mdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", batchID).Limit(1).Count(&n).Error; err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if err := db.Mdb.Model(&model.CollectRecord{}).Where("batch_id = ? AND ledger = ?", batchID, false).Update("ledger", true).Error; err != nil {
		return false, err
	}
	return true, nil
}
