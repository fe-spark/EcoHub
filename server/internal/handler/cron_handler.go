package handler

import (
	"fmt"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/spider"

	"github.com/gin-gonic/gin"
)

type CronHandler struct{}

var CronHd = new(CronHandler)

// FilmCronTaskList 获取所有的定时任务信息
func (h *CronHandler) FilmCronTaskList(c *gin.Context) {
	tl := service.CronSvc.GetFilmCrontab()
	dto.Success(tl, "定时任务列表获取成功", c)
}

// GetFilmCronTask 通过Id获取对应的定时任务信息
func (h *CronHandler) GetFilmCronTask(c *gin.Context) {
	id := c.DefaultQuery("id", "")
	if id == "" {
		dto.Failed("定时任务信息获取失败,任务Id不能为空", c)
		return
	}
	task, err := service.CronSvc.GetFilmCrontabById(id)
	if err != nil {
		dto.Failed(fmt.Sprint("定时任务信息获取失败", err.Error()), c)
		return
	}
	dto.Success(task, "定时任务详情获取成功!!!", c)
}

// FilmCronUpdate 更新定时任务信息
func (h *CronHandler) FilmCronUpdate(c *gin.Context) {
	t := model.FilmCollectTask{}
	if err := c.ShouldBindJSON(&t); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	task, err := service.CronSvc.GetFilmCrontabById(t.Id)
	if err != nil {
		dto.Failed(fmt.Sprint("更新失败: ", err.Error()), c)
		return
	}
	spec := strings.TrimSpace(t.Spec)
	if spec == "" {
		dto.Failed("参数校验失败, Cron表达式不能为空", c)
		return
	}
	if err := spider.ValidSpec(spec); err != nil {
		dto.Failed(fmt.Sprint("参数校验失败 cron表达式校验失败: ", err.Error()), c)
		return
	}
	task.Spec = spec
	if err := service.CronSvc.UpdateFilmCron(task); err != nil {
		dto.Failed(fmt.Sprint("更新失败: ", err.Error()), c)
		return
	}
	dto.SuccessOnlyMsg(fmt.Sprintf("定时任务[%s]更新成功", task.Id), c)
}

// ChangeTaskState 开启 | 关闭Id 对应的定时任务
func (h *CronHandler) ChangeTaskState(c *gin.Context) {
	t := model.FilmCollectTask{}
	if err := c.ShouldBindJSON(&t); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	if err := service.CronSvc.ChangeFilmCrontab(t.Id, t.State); err != nil {
		dto.Failed(fmt.Sprint("更新失败: ", err.Error()), c)
		return
	}
	dto.SuccessOnlyMsg(fmt.Sprintf("定时任务[%s]更新成功", t.Id), c)
}
