package repository

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// --------- Crontab Tasks -----------

// SaveFilmTask 保存影视采集任务信息
func SaveFilmTask(t model.FilmCollectTask) error {
	rec := model.CrontabRecord{
		TaskId:    t.Id,
		Time:      t.Time,
		Spec:      t.Spec,
		TaskModel: t.Model,
		State:     t.State,
		Remark:    t.Remark,
	}

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "task_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"time", "spec", "task_model", "state", "remark", "updated_at"}),
		}).Create(&rec).Error; err != nil {
			return err
		}

		// 更新关联站点
		if err := tx.Where("task_id = ?", t.Id).Delete(&model.CronSourceRel{}).Error; err != nil {
			return err
		}
		if len(t.Ids) > 0 {
			rels := make([]model.CronSourceRel, 0, len(t.Ids))
			for _, sid := range t.Ids {
				rels = append(rels, model.CronSourceRel{TaskId: t.Id, SourceId: sid})
			}
			if err := tx.Create(&rels).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		log.Println("SaveFilmTask Error:", err)
	}
	return err
}

// GetAllFilmTask 获取所有的任务信息
func GetAllFilmTask() []model.FilmCollectTask {
	var records []model.CrontabRecord
	if err := db.Mdb.Find(&records).Error; err != nil {
		log.Println("GetAllFilmTask Error:", err)
		return nil
	}

	var tl []model.FilmCollectTask
	for _, r := range records {
		var ids []string
		db.Mdb.Model(&model.CronSourceRel{}).Where("task_id = ?", r.TaskId).Pluck("source_id", &ids)
		tl = append(tl, model.FilmCollectTask{
			Id:     r.TaskId,
			Ids:    ids,
			Time:   r.Time,
			Spec:   r.Spec,
			Model:  r.TaskModel,
			State:  r.State,
			Remark: r.Remark,
		})
	}
	return tl
}

// GetFilmTaskById 通过 Id 获取当前任务信息
func GetFilmTaskById(id string) (model.FilmCollectTask, error) {
	var r model.CrontabRecord
	if err := db.Mdb.Where("task_id = ?", id).First(&r).Error; err != nil {
		return model.FilmCollectTask{}, errors.New(" The task does not exist ")
	}

	var ids []string
	db.Mdb.Model(&model.CronSourceRel{}).Where("task_id = ?", r.TaskId).Pluck("source_id", &ids)

	return model.FilmCollectTask{
		Id:     r.TaskId,
		Ids:    ids,
		Time:   r.Time,
		Spec:   r.Spec,
		Model:  r.TaskModel,
		State:  r.State,
		Remark: r.Remark,
	}, nil
}

// UpdateFilmTask 更新定时任务信息 (直接覆盖 Id 对应的定时任务信息)
func UpdateFilmTask(t model.FilmCollectTask) error {
	return SaveFilmTask(t)
}

// DelFilmTask 通过 Id 删除对应的定时任务信息
func DelFilmTask(id string) {
	_ = db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", id).Delete(&model.CrontabRecord{}).Error; err != nil {
			return err
		}
		return tx.Where("task_id = ?", id).Delete(&model.CronSourceRel{}).Error
	})
}

func ResetFilmTasks(tasks []model.FilmCollectTask) error {
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.CronSourceRel{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.CrontabRecord{}).Error; err != nil {
			return err
		}
		for _, task := range tasks {
			rec := model.CrontabRecord{
				TaskId:    task.Id,
				Time:      task.Time,
				Spec:      task.Spec,
				TaskModel: task.Model,
				State:     task.State,
				Remark:    task.Remark,
			}
			if err := tx.Create(&rec).Error; err != nil {
				return err
			}
			if len(task.Ids) == 0 {
				continue
			}
			rels := make([]model.CronSourceRel, 0, len(task.Ids))
			for _, sid := range task.Ids {
				rels = append(rels, model.CronSourceRel{TaskId: task.Id, SourceId: sid})
			}
			if err := tx.Create(&rels).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ExistTask 是否存在定时任务相关信息
func ExistTask() bool {
	var count int64
	db.Mdb.Model(&model.CrontabRecord{}).Count(&count)
	return count > 0
}

// --------- Collect Source -----------

// GetCollectSourceList 获取采集站 API 列表
func GetCollectSourceList() []model.FilmSource {
	var list []model.FilmSource
	if err := db.Mdb.Order("grade ASC").Find(&list).Error; err != nil {
		log.Println("GetCollectSourceList Error:", err)
		return nil
	}
	return list
}

// GetCollectSourceListByGrade 返回指定类型的采集 Api 信息 Master | Slave
func GetCollectSourceListByGrade(grade model.SourceGrade) []model.FilmSource {
	var list []model.FilmSource
	if err := db.Mdb.Where("grade = ?", grade).Find(&list).Error; err != nil {
		log.Println("GetCollectSourceListByGrade Error:", err)
		return nil
	}
	return list
}

// GetEnabledCollectSourceList 获取已启用采集站列表。
func GetEnabledCollectSourceList() []model.FilmSource {
	var list []model.FilmSource
	if err := db.Mdb.Where("state = ?", true).Order("grade ASC").Find(&list).Error; err != nil {
		log.Println("GetEnabledCollectSourceList Error:", err)
		return nil
	}
	return list
}

func GetCollectSourceStats(sourceIDs []string) map[string]*time.Time {
	result := make(map[string]*time.Time, len(sourceIDs))
	if len(sourceIDs) == 0 {
		return result
	}
	var rows []model.CollectSourceStats
	if err := db.Mdb.Where("source_id IN ?", sourceIDs).Find(&rows).Error; err != nil {
		log.Println("GetCollectSourceStats Error:", err)
		return result
	}
	for _, row := range rows {
		if row.LastCollectTime == nil || row.LastCollectTime.IsZero() {
			continue
		}
		value := *row.LastCollectTime
		result[row.SourceId] = &value
	}
	return result
}

func TouchCollectSourceStatsTx(tx *gorm.DB, sourceID string, at time.Time) error {
	if sourceID == "" {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	now := time.Now()
	stat := model.CollectSourceStats{SourceId: sourceID, LastCollectTime: &at}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "source_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"last_collect_time": at,
			"updated_at":        now,
			"deleted_at":        nil,
		}),
	}).Create(&stat).Error
}

func DeleteCollectSourceStatsTx(tx *gorm.DB, sourceIDs ...string) error {
	ids := make([]string, 0, len(sourceIDs))
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			continue
		}
		if _, ok := seen[sourceID]; ok {
			continue
		}
		seen[sourceID] = struct{}{}
		ids = append(ids, sourceID)
	}
	if len(ids) == 0 {
		return nil
	}
	return tx.Where("source_id IN ?", ids).Unscoped().Delete(&model.CollectSourceStats{}).Error
}

func ClearCollectSourceStatsTx(tx *gorm.DB) error {
	return tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.CollectSourceStats{}).Error
}

// FindCollectSourceById 通过 Id 标识获取对应的资源站信息
func FindCollectSourceById(id string) *model.FilmSource {
	var fs model.FilmSource
	if err := db.Mdb.Where("id = ?", id).First(&fs).Error; err != nil {
		return nil
	}
	return &fs
}

// DelCollectResource 通过 Id 删除对应的采集站点信息及其附属采集数据
func DelCollectResource(id string) error {
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		// 1. 删除关联的定时任务关系
		if err := tx.Where("source_id = ?", id).Delete(&model.CronSourceRel{}).Error; err != nil {
			return err
		}
		// 2. 删除附属站播放列表
		if err := tx.Where("source_id = ?", id).Delete(&model.MoviePlaylist{}).Error; err != nil {
			return err
		}
		// 3. 删除采集失败记录
		if err := tx.Where("origin_id = ?", id).Delete(&model.FailureRecord{}).Error; err != nil {
			return err
		}
		// 4. 删除采集站本身
		return tx.Where("id = ?", id).Delete(&model.FilmSource{}).Error
	})
}

// AddCollectSource 添加采集站信息
func AddCollectSource(s model.FilmSource) error {
	return AddCollectSourceTx(db.Mdb, s)
}

func AddCollectSourceTx(tx *gorm.DB, s model.FilmSource) error {
	var count int64
	if err := tx.Model(&model.FilmSource{}).Where("uri = ?", s.Uri).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("当前采集站点信息已存在，请勿重复添加")
	}
	// 基于 URI 生成稳定的哈希 ID，确保服务重启后采集源顺序一致且支持主从切换
	if s.Id == "" {
		s.Id = utils.GenerateHashKey(s.Uri)
	}
	normalizeCollectSourceDefaults(&s)
	return tx.Create(&s).Error
}

// BatchAddCollectSource 批量添加采集站信息
func BatchAddCollectSource(list []model.FilmSource) error {
	// 为没有 ID 的采集源生成稳定的哈希 ID
	for i := range list {
		if list[i].Id == "" {
			list[i].Id = utils.GenerateHashKey(list[i].Uri)
		}
		normalizeCollectSourceDefaults(&list[i])
	}
	return db.Mdb.Create(list).Error
}

// UpdateCollectSource 更新采集站信息
func UpdateCollectSource(s model.FilmSource) error {
	return UpdateCollectSourceTx(db.Mdb, s)
}

func UpdateCollectSourceTx(tx *gorm.DB, s model.FilmSource) error {
	var count int64
	if err := tx.Model(&model.FilmSource{}).Where("id != ? AND uri = ?", s.Id, s.Uri).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("当前采集站链接已存在其他站点中，请勿重复添加")
	}
	normalizeCollectSourceDefaults(&s)
	return tx.Save(&s).Error
}

// DemoteExistingMaster 将现有的主站降级为附属站，确保全局仅一个主站
func DemoteExistingMaster() error {
	return DemoteExistingMasterTx(db.Mdb)
}

func DemoteExistingMasterTx(tx *gorm.DB) error {
	return tx.Model(&model.FilmSource{}).
		Where("grade = ?", model.MasterCollect).
		Update("grade", model.SlaveCollect).Error
}

// ClearAllCollectSource 删除所有采集站信息
func ClearAllCollectSource() {
	if err := db.Mdb.Exec(fmt.Sprintf("TRUNCATE table %s", model.TableFilmSource)).Error; err != nil {
		log.Println("TRUNCATE table film_sources Error:", err)
	}
}

func ResetCollectSources(list []model.FilmSource) error {
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.FilmSource{}).Error; err != nil {
			return err
		}
		if err := ClearCollectSourceStatsTx(tx); err != nil {
			return err
		}
		for i := range list {
			if list[i].Id == "" {
				list[i].Id = utils.GenerateHashKey(list[i].Uri)
			}
			normalizeCollectSourceDefaults(&list[i])
		}
		if len(list) == 0 {
			return nil
		}
		return tx.Create(&list).Error
	})
}

func normalizeCollectSourceDefaults(source *model.FilmSource) {
	if source.Interval <= 0 {
		source.Interval = config.DefaultSpiderInterval
	}
}

// ExistCollectSourceList 查询是否已经存在站点 list 相关数据
func ExistCollectSourceList() bool {
	var count int64
	db.Mdb.Model(&model.FilmSource{}).Count(&count)
	return count > 0
}

// --------- Failure Record -----------

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
	if fl.RetryCount <= 0 {
		fl.RetryCount = 1
	}
	// 数据量不多但存在并发问题，开启事务
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		current, err := findPendingFailure(tx, fl)
		if err == nil {
			if err = tx.Model(&model.FailureRecord{}).Where("id = ?", current.ID).Updates(map[string]any{
				"origin_name": fl.OriginName,
				"uri":         fl.Uri,
				"cause":       fl.Cause,
				"retry_count": gorm.Expr("retry_count + ?", 1),
			}).Error; err != nil {
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
	// 如果事务提交失败，则输出相应信息，(存一份数据到 Redis??)
	if err != nil {
		log.Println("Save failure record affairs failed:", err)
	}
	return err
}

// FailureRecordList 获取所有的采集失效记录
func FailureRecordList(vo model.RecordRequestVo) []model.FailureRecord {
	// 通过 RecordRequestVo，生成查询条件
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

	// 获取分页数据
	dto.GetPage(qw, vo.Paging)
	// 获取分页查询的数据
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
	// 通过 ID 查询对应的数据
	if err := db.Mdb.First(&fr, id).Error; err != nil {
		return nil
	}
	return &fr
}

// PendingRecord 查询所有待处理的记录信息
func PendingRecord() []model.FailureRecord {
	var list []model.FailureRecord
	if err := db.Mdb.
		Where("(hour > 4320 OR hour < 0) AND status = ?", model.FailureRecordStatusPending).
		Order("created_at ASC, id ASC").
		Find(&list).Error; err != nil {
		log.Println("Query pending failure records failed:", err)
		return nil
	}

	var fr model.FailureRecord
	if err := db.Mdb.
		Where("hour > 0 AND hour < 4320 AND status = ?", model.FailureRecordStatusPending).
		Order("hour DESC, created_at ASC, id ASC").
		First(&fr).Error; err == nil {
		list = append(list, fr)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Println("Query incremental failure record failed:", err)
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
		return false, 0, errors.New("max retry count must be positive")
	}
	updates := map[string]any{
		"cause":       cause,
		"retry_count": gorm.Expr("retry_count + ?", 1),
		"status":      gorm.Expr("CASE WHEN status = ? OR retry_count >= ? THEN ? ELSE status END", model.FailureRecordStatusFailed, maxRetryCount-1, model.FailureRecordStatusFailed),
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
	// 查询 id 对应的失败记录
	fr := FindRecordById(id)
	if fr == nil {
		return errors.New("failure record not found")
	}
	return db.Mdb.Model(&model.FailureRecord{}).Where("id = ?", fr.ID).Update("status", status).Error
}

// DeleteRetriedRecords 删除已有重试结果的记录信息 -- 逻辑删除。
func DeleteRetriedRecords() {
	if err := db.Mdb.Where("status IN ?", []int{model.FailureRecordStatusSuccess, model.FailureRecordStatusFailed}).Delete(&model.FailureRecord{}).Error; err != nil {
		log.Println("Delete failure record failed:", err)
	}
}

// TruncateRecordTable  截断 record table
func TruncateRecordTable() {
	err := db.Mdb.Exec(fmt.Sprintf("TRUNCATE Table %s", model.TableFailureRecord)).Error
	if err != nil {
		log.Println("TRUNCATE TABLE Error: ", err)
	}
}
