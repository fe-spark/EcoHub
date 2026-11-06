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

// removeRecordsAndFilms 在同一事务里删记录，并只删这些批次里已经不存在记录的影片行。
func removeRecordsAndFilms(tx *gorm.DB, rows []model.CollectRecord) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(rows))
	batchIDs := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		batchID := strings.TrimSpace(row.BatchID)
		if batchID == "" {
			continue
		}
		if _, ok := seen[batchID]; ok {
			continue
		}
		seen[batchID] = struct{}{}
		batchIDs = append(batchIDs, batchID)
	}
	if err := tx.Where("id IN ?", ids).Delete(&model.CollectRecord{}).Error; err != nil {
		return err
	}
	if len(batchIDs) == 0 {
		return nil
	}
	alive := tx.Model(&model.CollectRecord{}).Where("batch_id IN ? AND batch_id != ''", batchIDs).Select("batch_id")
	return tx.Where("batch_id IN ? AND batch_id NOT IN (?)", batchIDs, alive).
		Delete(&model.CollectRecordFilmItem{}).Error
}

// FilmNamesByIDs 按全局影片编号读取片名。已删除的影片仍返回原名，供采集记录展示。
func FilmNamesByIDs(ids []int64) (map[int64]string, error) {
	out := make(map[int64]string)
	if len(ids) == 0 {
		return out, nil
	}
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	var rows []struct {
		Mid  int64
		Name string
	}
	if err := db.Mdb.Unscoped().Model(&model.FilmIndex{}).
		Select("mid", "name").
		Where("mid IN ?", ids).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if row.Mid > 0 && name != "" {
			out[row.Mid] = name
		}
	}
	return out, nil
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
	}
	if strings.TrimSpace(c.FilmIDs) != "" {
		updates["film_ids"] = strings.TrimSpace(c.FilmIDs)
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
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		var rows []model.CollectRecord
		if err := tx.Select("id", "batch_id").
			Where("id IN ? AND status != ?", ids, model.CollectRecordStatusRunning).
			Find(&rows).Error; err != nil {
			return err
		}
		return removeRecordsAndFilms(tx, rows)
	})
}

// ClearCollectRecords 清空已结束的采集记录，保留进行中的批次和它的影片清单。
func ClearCollectRecords() error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		var rows []model.CollectRecord
		if err := tx.Select("id", "batch_id").
			Where("status != ?", model.CollectRecordStatusRunning).
			Find(&rows).Error; err != nil {
			return err
		}
		return removeRecordsAndFilms(tx, rows)
	})
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
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		var rows []model.CollectRecord
		if err := tx.Select("id", "batch_id").
			Where("started_at < ?", threshold).
			Find(&rows).Error; err != nil {
			return err
		}
		if err := removeRecordsAndFilms(tx, rows); err != nil {
			return err
		}
		// 只清没有记录的旧影片行。仍在库里的批次，包括进行中的，清单保留。
		alive := tx.Model(&model.CollectRecord{}).Where("batch_id != ''").Select("batch_id")
		return tx.Where("created_at < ? AND batch_id NOT IN (?)", threshold, alive).
			Delete(&model.CollectRecordFilmItem{}).Error
	})
}

// UpdateRunningCollectRecord 刷新仍在运行的批次页数、影片数和明细。不改终态。
func UpdateRunningCollectRecord(batchID string, c model.CollectRecordCompletion) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return errors.New("批次号不能为空")
	}
	updates := map[string]any{
		"success_sources": c.SuccessSources,
		"failed_sources":  c.FailedSources,
		"film_count":      c.FilmCount,
		"page_total":      c.PageTotal,
		"page_success":    c.PageSuccess,
		"page_failed":     c.PageFailed,
		"detail_json":     c.DetailJson,
	}
	if c.Hour != 0 {
		updates["hour"] = c.Hour
	}
	return db.Mdb.Model(&model.CollectRecord{}).
		Where("batch_id = ? AND status = ?", batchID, model.CollectRecordStatusRunning).
		Updates(updates).Error
}

// MarkInterruptedCollectRecords 服务启动时兜底把残留 running 状态标记为 interrupted
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
		films, bySource, err := filmRowCounts(db.Mdb, rec.BatchID)
		if err != nil {
			return err
		}
		updates := map[string]any{
			"status":      model.CollectRecordStatusInterrupted,
			"finished_at": now,
			"duration_ms": duration,
			"error_msg":   "服务重启，任务异常中断",
			"film_count":  films,
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
