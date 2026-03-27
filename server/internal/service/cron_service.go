package service

import (
	"fmt"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
)

type CronService struct{}

var CronSvc = new(CronService)

// GetFilmCrontab 获取所有定时任务信息
func (s *CronService) GetFilmCrontab() []model.CronTaskVo {
	cst := time.FixedZone("UTC", 8*3600)
	var l []model.CronTaskVo
	tl := repository.GetAllFilmTask()
	for _, t := range tl {
		e := spider.GetEntryByTaskId(t.Id)
		var preV, nextV string
		// 只有任务开启时，Next 才有意义
		if t.State && !e.Next.IsZero() {
			nextV = e.Next.In(cst).Format(time.DateTime)
		}
		// 上次执行时间，如果从来没执行过则是零值
		if !e.Prev.IsZero() {
			preV = e.Prev.In(cst).Format(time.DateTime)
		}
		taskVo := model.CronTaskVo{FilmCollectTask: t, PreV: preV, Next: nextV}
		l = append(l, taskVo)
	}
	return l
}

// GetFilmCrontabById 通过ID获取对应的定时任务信息
func (s *CronService) GetFilmCrontabById(id string) (model.FilmCollectTask, error) {
	t, err := repository.GetFilmTaskById(id)
	return t, err
}

// ChangeFilmCrontab 改变定时任务的状态 开启 | 停止
func (s *CronService) ChangeFilmCrontab(id string, state bool) error {
	ft, err := repository.GetFilmTaskById(id)
	if err != nil {
		return fmt.Errorf("定时任务状态切换失败: %w", err)
	}
	ft.State = state
	repository.UpdateFilmTask(ft)
	// 同步重载运行时引擎
	if err := spider.ReloadCronTask(id); err != nil {
		return fmt.Errorf("定时任务重载失败: %w", err)
	}
	return nil
}

// UpdateFilmCron 更新定时任务的状态信息
func (s *CronService) UpdateFilmCron(t model.FilmCollectTask) error {
	repository.UpdateFilmTask(t)
	// 同步重载运行时引擎（可能修改了 Cron 表达式或采集站列表）
	if err := spider.ReloadCronTask(t.Id); err != nil {
		return fmt.Errorf("定时任务重载失败: %w", err)
	}
	return nil
}
