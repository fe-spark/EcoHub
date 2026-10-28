package repository

import (
	"errors"
	"log"
	"time"

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
func FailureRecordList(vo model.RecordRequestVo) ([]model.FailureRecord, error) {
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

	if err := dto.GetPage(qw, vo.Paging); err != nil {
		return nil, err
	}
	if vo.Paging.PageCount == 0 {
		return []model.FailureRecord{}, nil
	}
	if vo.Paging.Current > vo.Paging.PageCount {
		vo.Paging.Current = vo.Paging.PageCount
	}
	list := make([]model.FailureRecord, 0)
	if err := qw.Limit(vo.Paging.PageSize).Offset((vo.Paging.Current - 1) * vo.Paging.PageSize).Order("created_at DESC, id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
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
func PendingRecord() ([]model.FailureRecord, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	list := make([]model.FailureRecord, 0)
	if err := db.Mdb.
		Where("status = ?", model.FailureRecordStatusPending).
		Order("created_at ASC, id ASC").
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// MarkFailureRecordRetryFailed 用更新前的重试次数决定是否最终失败。
// 次数和状态必须一起按旧值计算，不能在同一条 UPDATE 里先写次数再读次数。
func MarkFailureRecordRetryFailed(fr *model.FailureRecord, cause string, maxRetryCount int) (bool, int, error) {
	if fr == nil || fr.ID == 0 {
		return false, 0, errors.New("failure record not found")
	}
	if maxRetryCount <= 0 {
		maxRetryCount = model.MaxFailureRetryCount
	}
	var final bool
	var count int
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		var current model.FailureRecord
		if err := tx.Select("id", "retry_count").First(&current, fr.ID).Error; err != nil {
			return err
		}
		count = current.RetryCount + 1
		final = count >= maxRetryCount
		if count > maxRetryCount {
			count = maxRetryCount
		}
		status := model.FailureRecordStatusPending
		if final {
			status = model.FailureRecordStatusFailed
		}
		return tx.Model(&model.FailureRecord{}).Where("id = ?", fr.ID).Updates(map[string]any{
			"cause":       cause,
			"retry_count": count,
			"status":      status,
		}).Error
	})
	if err != nil {
		return false, 0, err
	}
	return final, count, nil
}

// MarkFailureRecordRetrySucceeded 重试成功后把记录留在列表里，只改成成功状态。
func MarkFailureRecordRetrySucceeded(fr *model.FailureRecord) error {
	if fr == nil || fr.ID == 0 {
		return errors.New("failure record not found")
	}
	err := db.Mdb.Model(&model.FailureRecord{}).Where("id = ?", fr.ID).Updates(map[string]any{
		"status": model.FailureRecordStatusSuccess,
		"cause":  "",
	}).Error
	if err != nil {
		return err
	}
	fr.Status = model.FailureRecordStatusSuccess
	fr.Cause = ""
	return nil
}

// UpdateFailureRecordStatusByID 按 ID 修改失败记录的重试结果状态。
func UpdateFailureRecordStatusByID(id uint, status int) error {
	fr := FindRecordById(id)
	if fr == nil {
		return errors.New("failure record not found")
	}
	return db.Mdb.Model(&model.FailureRecord{}).Where("id = ?", fr.ID).Update("status", status).Error
}

// DeleteRetriedRecords 删除已有重试结果的记录信息
func DeleteRetriedRecords() error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return db.Mdb.Where("status IN ?", []int{model.FailureRecordStatusSuccess, model.FailureRecordStatusFailed}).Delete(&model.FailureRecord{}).Error
}

// DeleteFailureRecordsByOriginIdTx 按源站 ID 物理清除对应的采集失败记录
func DeleteFailureRecordsByOriginIdTx(tx *gorm.DB, originId string) error {
	if tx == nil || originId == "" {
		return nil
	}
	return tx.Where("origin_id = ?", originId).Delete(&model.FailureRecord{}).Error
}

// TruncateRecordTable 截断 record table
func TruncateRecordTable() error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return support.TruncateTable(db.Mdb, model.TableFailureRecord)
}

// PruneExpiredFailureRecords 物理修剪超过保留期的终态失败记录（status 为 success 或 failed 且创建超过 retention）
func PruneExpiredFailureRecords(retention time.Duration) int64 {
	if db.Mdb == nil {
		return 0
	}
	cutoff := time.Now().Add(-retention)
	res := db.Mdb.Where("status IN ? AND created_at < ?",
		[]int{model.FailureRecordStatusSuccess, model.FailureRecordStatusFailed}, cutoff).
		Delete(&model.FailureRecord{})
	if res.Error != nil {
		log.Printf("[FailureRecord] 修剪历史失败流水失败: %v", res.Error)
		return 0
	}
	return res.RowsAffected
}
