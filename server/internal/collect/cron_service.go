package collect

import (
	"fmt"
	"time"

	"server/internal/model"
)

type CronService struct{}

var CronSvc = new(CronService)

// GetFilmCrontab 获取所有定时任务信息
func (s *CronService) GetFilmCrontab() ([]model.CronTaskVo, error) {
	var l []model.CronTaskVo
	tl, err := GetAllFilmTask()
	if err != nil {
		return nil, err
	}

	for _, t := range tl {
		e := GetEntryByTaskId(t.Id)
		var preV, nextV string
		// 只有任务开启时，Next 才有意义
		if t.State && !e.Next.IsZero() {
			nextV = e.Next.In(time.Local).Format(time.DateTime)
		}
		// 上次执行时间是任务真正开工的时间。入队、让行和跳过不算。
		if prev := TaskPrev(t.Id); !prev.IsZero() {
			preV = prev.In(time.Local).Format(time.DateTime)
		}
		taskVo := model.CronTaskVo{
			FilmCollectTask: t,
			PreV:            preV,
			Next:            nextV,
			Running:         IsCronTaskRunning(t.Id),
			Queued:          IsCronTaskQueued(t.Id),
		}

		l = append(l, taskVo)
	}
	return l, nil
}

// GetFilmCrontabById 通过ID获取对应的定时任务信息
func (s *CronService) GetFilmCrontabById(id string) (model.FilmCollectTask, error) {
	t, err := GetFilmTaskById(id)
	return t, err
}

// ChangeFilmCrontab 改变定时任务的状态 开启 | 停止
func (s *CronService) ChangeFilmCrontab(id string, state bool) error {
	ft, err := GetFilmTaskById(id)
	if err != nil {
		return fmt.Errorf("定时任务状态切换失败: %w", err)
	}
	prevState := ft.State
	ft.State = state
	if err := UpdateFilmTask(ft); err != nil {
		return fmt.Errorf("定时任务状态保存失败: %w", err)
	}
	// 同步重载运行时引擎，失败时回滚数据库状态
	if err := ReloadCronTask(id); err != nil {
		ft.State = prevState
		_ = UpdateFilmTask(ft)
		return fmt.Errorf("定时任务重载失败: %w", err)
	}
	return nil
}

// UpdateFilmCron 更新定时任务的状态信息
func (s *CronService) UpdateFilmCron(t model.FilmCollectTask) error {
	if !ValidTaskModel(t.Model) {
		return fmt.Errorf("不支持的定时任务类型: %d", t.Model)
	}
	if t.Model == model.TaskModelAutoCollect && t.Time != -1 && t.Time <= 0 {
		return fmt.Errorf("自动采集时长必须大于 0 或为 -1（全量）")
	}
	prev, prevErr := GetFilmTaskById(t.Id)
	if err := UpdateFilmTask(t); err != nil {
		return fmt.Errorf("定时任务保存失败: %w", err)
	}
	// 同步重载运行时引擎，失败时回滚旧配置
	if err := ReloadCronTask(t.Id); err != nil {
		if prevErr == nil {
			_ = UpdateFilmTask(prev)
		}
		return fmt.Errorf("定时任务重载失败: %w", err)
	}
	return nil
}

// RunFilmCronTaskOnce 立即手动执行一次定时任务。返回给用户的提示：已触发，或维护任务已排队。
func (s *CronService) RunFilmCronTaskOnce(id string) (string, error) {
	return RunTaskOnceWithStatus(id)
}
