package repository

import (
	"errors"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm/clause"
)

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

	return db.Mdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"time", "spec", "task_model", "state", "remark", "updated_at"}),
	}).Create(&rec).Error
}

// GetAllFilmTask 获取所有的任务信息
func GetAllFilmTask() ([]model.FilmCollectTask, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	var records []model.CrontabRecord
	if err := db.Mdb.Find(&records).Error; err != nil {
		return nil, err
	}

	tl := make([]model.FilmCollectTask, 0, len(records))
	for _, r := range records {
		tl = append(tl, model.FilmCollectTask{
			Id:     r.TaskId,
			Time:   r.Time,
			Spec:   r.Spec,
			Model:  r.TaskModel,
			State:  r.State,
			Remark: r.Remark,
		})
	}
	return tl, nil
}

// GetFilmTaskById 通过 Id 获取当前任务信息
func GetFilmTaskById(id string) (model.FilmCollectTask, error) {
	if db.Mdb == nil {
		return model.FilmCollectTask{}, errors.New("database not initialized")
	}
	var r model.CrontabRecord
	if err := db.Mdb.Where("task_id = ?", id).First(&r).Error; err != nil {
		return model.FilmCollectTask{}, err
	}

	return model.FilmCollectTask{
		Id:     r.TaskId,
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

// DeleteFilmTask 物理删除定时任务，避免 task_id 唯一索引冲突。
func DeleteFilmTask(id string) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return db.Mdb.Unscoped().Where("task_id = ?", id).Delete(&model.CrontabRecord{}).Error
}
