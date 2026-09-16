package repository

import (
	"errors"
	"log"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func pendingFailureScope(tx *gorm.DB, fl model.FailureRecord) *gorm.DB {
	return tx.Where("origin_id = ? AND page_number = ? AND hour = ? AND status = ?",
		fl.OriginId, fl.PageNumber, fl.Hour,
		model.FailureRecordStatusPending,
	)
}

func findPendingFailure(tx *gorm.DB, fl model.FailureRecord) (*model.FailureRecord, error) {
	var current model.FailureRecord
	err := pendingFailureScope(tx, fl).First(&current).Error
	if err != nil {
		return nil, err
	}
	return &current, nil
}

// SaveFailureRecord 添加采集失效记录
func SaveFailureRecord(fl model.FailureRecord) error {
	if fl.Status <= 0 {
		fl.Status = model.FailureRecordStatusPending
	}
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		current, err := findPendingFailure(tx, fl)
		if err == nil {
			updates := map[string]any{
				"origin_name": fl.OriginName,
				"uri":         fl.Uri,
				"cause":       fl.Cause,
			}
			if err = tx.Model(&model.FailureRecord{}).Where("id = ?", current.ID).Updates(updates).Error; err != nil {
				log.Println("Update failure record failed:", err)
				return err
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Println("Query failure record failed:", err)
			return err
		}

		if err = tx.Create(&fl).Error; err != nil {
			log.Println("Add failure record failed:", err)
			return err
		}
		return nil
	})
	if err != nil {
		log.Println("Save failure record affairs failed:", err)
	}
	return err
}

// FailureRecordList 获取所有的采集失效记录
func FailureRecordList(vo model.RecordRequestVo) []model.FailureRecord {
	qw := db.Mdb.Model(&model.FailureRecord{})
	if vo.OriginId != "" {
		qw = qw.Where("origin_id = ?", vo.OriginId)
	}
	if !vo.BeginTime.IsZero() && !vo.EndTime.IsZero() {
		qw = qw.Where("created_at BETWEEN ? AND ? ", vo.BeginTime, vo.EndTime)
	}
	if vo.Status >= 0 {
		qw = qw.Where("status = ?", vo.Status)
	}

	dto.GetPage(qw, vo.Paging)
	var list []model.FailureRecord
	if err := qw.Limit(vo.Paging.PageSize).Offset((vo.Paging.Current - 1) * vo.Paging.PageSize).Order("created_at DESC, id DESC").Find(&list).Error; err != nil {
		log.Println(err)
		return nil
	}
	return list
}

// FindRecordById 获取 id 对应的失效记录
func FindRecordById(id uint) *model.FailureRecord {
	var fr model.FailureRecord
	if err := db.Mdb.First(&fr, id).Error; err != nil {
		return nil
	}
	return &fr
}

// PendingRecord 查询所有待处理的记录信息
func PendingRecord() []model.FailureRecord {
	var list []model.FailureRecord
	if err := db.Mdb.
		Where("status = ?", model.FailureRecordStatusPending).
		Order("created_at ASC, id ASC").
		Find(&list).Error; err != nil {
		log.Println("Query pending failure records failed:", err)
		return nil
	}
	return list
}

// UpdateFailureRecordStatus 修改失败记录的重试结果状态。
func UpdateFailureRecordStatus(fr *model.FailureRecord, status int) {
	if fr == nil || fr.ID == 0 {
		return
	}
	db.Mdb.Model(&model.FailureRecord{}).Where("id = ?", fr.ID).Update("status", status)
}

// MarkFailureRecordRetryFailed 更新当前失败记录的失败原因，并用数据库当前重试次数判断是否最终失败。
func MarkFailureRecordRetryFailed(fr *model.FailureRecord, cause string, maxRetryCount int) (bool, int, error) {
	if fr == nil || fr.ID == 0 {
		return false, 0, errors.New("failure record not found")
	}
	if maxRetryCount <= 0 {
		maxRetryCount = model.MaxFailureRetryCount
	}
	updates := map[string]any{
		"cause":       cause,
		"retry_count": gorm.Expr("CASE WHEN retry_count + 1 >= ? THEN ? ELSE retry_count + 1 END", maxRetryCount, maxRetryCount),
		"status":      gorm.Expr("CASE WHEN retry_count + 1 >= ? THEN ? ELSE ? END", maxRetryCount, model.FailureRecordStatusFailed, model.FailureRecordStatusPending),
	}
	if err := db.Mdb.Model(&model.FailureRecord{}).Where("id = ?", fr.ID).Updates(updates).Error; err != nil {
		return false, 0, err
	}
	var current model.FailureRecord
	if err := db.Mdb.Select("status", "retry_count").First(&current, fr.ID).Error; err != nil {
		return false, 0, err
	}
	return current.Status == model.FailureRecordStatusFailed, current.RetryCount, nil
}

// UpdateFailureRecordStatusByID 按 ID 修改失败记录的重试结果状态。
func UpdateFailureRecordStatusByID(id uint, status int) error {
	fr := FindRecordById(id)
	if fr == nil {
		return errors.New("failure record not found")
	}
	return db.Mdb.Model(&model.FailureRecord{}).Where("id = ?", fr.ID).Update("status", status).Error
}

// DeleteFailureRecord 按记录 ID 删除单个失败记录。
func DeleteFailureRecord(fr *model.FailureRecord) {
	if fr == nil || fr.ID == 0 {
		return
	}
	if err := db.Mdb.Delete(&model.FailureRecord{}, fr.ID).Error; err != nil {
		log.Printf("[Spider] 删除重试成功记录失败 id=%d: %v\n", fr.ID, err)
	}
}

// DeleteRetriedRecords 删除已有重试结果的记录信息
func DeleteRetriedRecords() {
	if err := db.Mdb.Where("status IN ?", []int{model.FailureRecordStatusSuccess, model.FailureRecordStatusFailed}).Delete(&model.FailureRecord{}).Error; err != nil {
		log.Println("Delete failure record failed:", err)
	}
}

// DeleteFailureRecordsByOriginIdTx 按源站 ID 物理清除对应的采集失败记录
func DeleteFailureRecordsByOriginIdTx(tx *gorm.DB, originId string) error {
	if tx == nil || originId == "" {
		return nil
	}
	return tx.Where("origin_id = ?", originId).Delete(&model.FailureRecord{}).Error
}

// NormalizeFailureRecordsRetryCount 纠正历史数据中状态与重试次数不一致的记录
func NormalizeFailureRecordsRetryCount() {
	if db.Mdb == nil {
		return
	}
	_ = db.Mdb.Model(&model.FailureRecord{}).
		Where("retry_count >= ?", model.MaxFailureRetryCount).
		Updates(map[string]any{
			"retry_count": model.MaxFailureRetryCount,
			"status":      model.FailureRecordStatusFailed,
		}).Error

	_ = db.Mdb.Model(&model.FailureRecord{}).
		Where("retry_count < ? AND status = ?", model.MaxFailureRetryCount, model.FailureRecordStatusFailed).
		Updates(map[string]any{
			"status": model.FailureRecordStatusPending,
		}).Error
}

// TruncateRecordTable 截断 record table
func TruncateRecordTable() {
	err := support.TruncateTable(db.Mdb, model.TableFailureRecord)
	if err != nil {
		log.Println("TRUNCATE TABLE Error: ", err)
	}
}
