package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// CreateCollectRecord 创建采集运行记录（初始状态一般为 running）
func CreateCollectRecord(rec *model.CollectRecord) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	if rec == nil || strings.TrimSpace(rec.BatchID) == "" {
		return errors.New("采集批次信息异常")
	}
	if rec.StartedAt.IsZero() {
		rec.StartedAt = time.Now()
	}
	return db.Mdb.Create(rec).Error
}

// CompleteCollectRecord 更新采集批次终态与结算指标
func CompleteCollectRecord(batchID string, c model.CollectRecordCompletion) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return errors.New("批次号不能为空")
	}

	finishedAt := c.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}

	updates := map[string]any{
		"status":          c.Status,
		"hour":            c.Hour,
		"success_sources": c.SuccessSources,
		"failed_sources":  c.FailedSources,
		"finished_at":     finishedAt,
		"duration_ms":     c.DurationMs,
		"film_count":      c.FilmCount,
		"page_total":      c.PageTotal,
		"page_success":    c.PageSuccess,
		"page_failed":     c.PageFailed,
		"error_msg":       c.ErrorMsg,
		"detail_json":     c.DetailJson,
	}

	var n int64
	if err := db.Mdb.Model(&model.CollectRecord{}).Where("batch_id = ?", batchID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("采集记录不存在: %s", batchID)
	}
	return db.Mdb.Model(&model.CollectRecord{}).
		Where("batch_id = ?", batchID).
		Updates(updates).Error
}

// GetCollectRecordByBatchID 通过批次号获取单个采集记录
func GetCollectRecordByBatchID(batchID string) (*model.CollectRecord, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return nil, errors.New("批次号不能为空")
	}
	var rec model.CollectRecord
	if err := db.Mdb.Where("batch_id = ?", batchID).First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetCollectRecordList 分页筛选采集运行记录
func GetCollectRecordList(q model.CollectRecordQuery) ([]model.CollectRecord, int64, error) {
	if db.Mdb == nil {
		return nil, 0, errors.New("数据库未连接")
	}

	tx := db.Mdb.Model(&model.CollectRecord{})
	if trig := strings.TrimSpace(q.Trigger); trig != "" {
		tx = tx.Where("trigger = ?", trig)
	}
	if st := strings.TrimSpace(q.Status); st != "" {
		tx = tx.Where("status = ?", st)
	}
	if !q.BeginTime.IsZero() {
		tx = tx.Where("started_at >= ?", q.BeginTime)
	}
	if !q.EndTime.IsZero() {
		tx = tx.Where("started_at <= ?", q.EndTime)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 10
	} else if pageSize > 100 {
		pageSize = 100
	}

	var list []model.CollectRecord
	err := tx.Order("started_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// DeleteCollectRecords 按 ID 列表批量删除采集记录
func DeleteCollectRecords(ids []uint) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	if len(ids) == 0 {
		return nil
	}
	return db.Mdb.Where("id IN ?", ids).Delete(&model.CollectRecord{}).Error
}

// ClearCollectRecords 清空全部采集记录
func ClearCollectRecords() error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.CollectRecord{}).Error
}

// CleanOldCollectRecords 清理超过指定天数的历史采集记录（防数据库膨胀）
func CleanOldCollectRecords(days int) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	if days <= 0 {
		days = 30
	}
	threshold := time.Now().AddDate(0, 0, -days)
	return db.Mdb.Where("started_at < ?", threshold).Delete(&model.CollectRecord{}).Error
}

// MarkInterruptedCollectRecords 服务启动时兜底把残留 running 状态标记为 interrupted
func MarkInterruptedCollectRecords() error {
	if db.Mdb == nil {
		return nil
	}
	now := time.Now()
	return db.Mdb.Model(&model.CollectRecord{}).
		Where("status = ?", model.CollectRecordStatusRunning).
		Updates(map[string]any{
			"status":      model.CollectRecordStatusInterrupted,
			"finished_at": now,
			"error_msg":   "服务重启，任务异常中断",
		}).Error
}
