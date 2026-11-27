package ledger

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// removeRecordsAndFilms 在同一事务里删记录，并删掉这些记录对应的影片账本行（仅供删站收口事务复用）。
func removeRecordsAndFilms(tx *gorm.DB, rows []model.CollectRecord) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		if row.ID > 0 {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if err := tx.Where("id IN ?", ids).Delete(&model.CollectRecord{}).Error; err != nil {
		return err
	}
	return deleteCollectFilmsByRecordIDs(tx, ids)
}

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
		"ledger":          c.Ledger,
	}
	if c.SourceCount > 0 {
		updates["source_count"] = c.SourceCount
	}

	var rec model.CollectRecord
	if err := db.Mdb.Select("id", "status").Where("batch_id = ?", batchID).First(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("采集记录不存在: %s", batchID)
		}
		return err
	}
	if rec.Status != model.CollectRecordStatusRunning {
		return nil
	}
	return db.Mdb.Model(&model.CollectRecord{}).
		Where("batch_id = ? AND status = ?", batchID, model.CollectRecordStatusRunning).
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
	if batchID := strings.TrimSpace(q.BatchID); batchID != "" {
		tx = tx.Where("batch_id = ?", batchID)
	} else {
		if trig := strings.TrimSpace(q.Trigger); trig != "" {
			tx = tx.Where("`trigger` = ?", trig)
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
		if q.UpdatedOnly {
			tx = tx.Where("film_count > 0")
		}
		if q.FailedOnly {
			tx = tx.Where("(status IN ? OR failed_sources > 0)", []string{
				model.CollectRecordStatusFailed,
				model.CollectRecordStatusPartial,
				model.CollectRecordStatusInterrupted,
			})
		}
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

// DeleteCollectRecords 按 ID 列表批量删除已结束采集记录，账本行在事务外分批清理避免大事务锁表。
func DeleteCollectRecords(ids []uint) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	if len(ids) == 0 {
		return nil
	}
	session := db.Mdb.Session(&gorm.Session{SkipDefaultTransaction: true})
	var targetIDs []uint
	if err := session.Model(&model.CollectRecord{}).
		Where("id IN ? AND status != ?", ids, model.CollectRecordStatusRunning).
		Pluck("id", &targetIDs).Error; err != nil {
		return err
	}
	if len(targetIDs) == 0 {
		return nil
	}
	if err := session.Where("id IN ? AND status != ?", targetIDs, model.CollectRecordStatusRunning).
		Delete(&model.CollectRecord{}).Error; err != nil {
		return err
	}
	gone, err := recordIDsWithoutRecord(session, targetIDs)
	if err != nil {
		return err
	}
	return deleteCollectFilmsByRecordIDs(session, gone)
}

// ClearCollectRecords 清空已结束的采集记录，保留进行中批次及其账本；分批删除避免大事务超时。
func ClearCollectRecords() error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	session := db.Mdb.Session(&gorm.Session{SkipDefaultTransaction: true})
	recordChunk := collectCleanRecordChunk
	if recordChunk <= 0 {
		recordChunk = 200
	}
	var cursor uint
	for {
		var ids []uint
		if err := session.Model(&model.CollectRecord{}).
			Where("status != ? AND id > ?", model.CollectRecordStatusRunning, cursor).
			Order("id ASC").
			Limit(recordChunk).
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		cursor = ids[len(ids)-1]
		if err := session.Where("id IN ? AND status != ?", ids, model.CollectRecordStatusRunning).
			Delete(&model.CollectRecord{}).Error; err != nil {
			return err
		}
		gone, err := recordIDsWithoutRecord(session, ids)
		if err != nil {
			return err
		}
		if err := deleteCollectFilmsByRecordIDs(session, gone); err != nil {
			return err
		}
		if len(ids) < recordChunk {
			return nil
		}
	}
}

// MarkInterruptedCollectRecords 服务启动时兜底把残留 running 状态标记为 interrupted，并按已落库账本对账影片数。
func MarkInterruptedCollectRecords() error {
	if db.Mdb == nil {
		return nil
	}
	var rows []model.CollectRecord
	if err := db.Mdb.Where("status = ?", model.CollectRecordStatusRunning).Find(&rows).Error; err != nil {
		return err
	}
	now := time.Now()
	for _, rec := range rows {
		duration := now.Sub(rec.StartedAt).Milliseconds()
		if duration < 0 {
			duration = 0
		}
		films, bySource, err := FilmRowCounts(db.Mdb, rec.ID)
		if err != nil {
			return err
		}
		updates := map[string]any{
			"status":      model.CollectRecordStatusInterrupted,
			"finished_at": now,
			"duration_ms": duration,
			"error_msg":   "服务重启，任务异常中断",
			"film_count":  films,
			"ledger":      films > 0,
		}
		detail, err := applyStoredFilmTotals(rec.DetailJson, bySource)
		if err != nil {
			return err
		}
		if detail != "" {
			updates["detail_json"] = detail
		}
		if err := db.Mdb.Model(&model.CollectRecord{}).
			Where("id = ? AND status = ?", rec.ID, model.CollectRecordStatusRunning).
			Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}
