package repository

import (
	"errors"
	"log"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
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
