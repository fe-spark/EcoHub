package ledger

import (
	"errors"
	"fmt"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// 清理参数（包级变量，便于测试覆盖分批删除）。
var (
	collectCleanRecordChunk      = 200
	collectCleanBeforeDeleteHook func(ids []uint)
)

// CleanOldCollectRecords 按留存天数分批清理过期已结束采集记录（进行中批次一律保留）。
func CleanOldCollectRecords(days int) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	if days <= 0 {
		days = 30
	}
	recordThreshold := time.Now().AddDate(0, 0, -days)
	session := db.Mdb.Session(&gorm.Session{SkipDefaultTransaction: true})
	recordChunk := collectCleanRecordChunk
	if recordChunk <= 0 {
		recordChunk = 200
	}

	var cursor uint
	for {
		var ids []uint
		if err := session.Model(&model.CollectRecord{}).
			Where("started_at < ? AND status != ? AND id > ?", recordThreshold, model.CollectRecordStatusRunning, cursor).
			Order("id ASC").
			Limit(recordChunk).
			Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("查询过期采集记录失败: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		cursor = ids[len(ids)-1]
		if collectCleanBeforeDeleteHook != nil {
			collectCleanBeforeDeleteHook(ids)
		}
		if err := session.Where("id IN ? AND status != ?", ids, model.CollectRecordStatusRunning).
			Delete(&model.CollectRecord{}).Error; err != nil {
			return fmt.Errorf("删除过期采集记录失败: %w", err)
		}
		if len(ids) < recordChunk {
			return nil
		}
	}
}
