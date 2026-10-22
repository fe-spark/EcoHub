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
	tl, err := service.CronSvc.GetFilmCrontab()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
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

// FilmCronSave 保存定时任务信息
func (h *CronHandler) FilmCronSave(c *gin.Context) {
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
	if remark := strings.TrimSpace(t.Remark); remark != "" {
		task.Remark = remark
	}
	if err := service.CronSvc.UpdateFilmCron(task); err != nil {
		dto.Failed(fmt.Sprint("更新失败: ", err.Error()), c)
		return
	}
	dto.SuccessOnlyMsg(fmt.Sprintf("定时任务[%s]更新成功", task.Id), c)
}

// ChangeTaskState 统一批量/单任务启停状态
func (h *CronHandler) ChangeTaskState(c *gin.Context) {
	var req struct {
		Ids   []string `json:"ids"`
		State bool     `json:"state"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	cleanIds := make([]string, 0, len(req.Ids))
	for _, id := range req.Ids {
		id = strings.TrimSpace(id)
		if id != "" {
			cleanIds = append(cleanIds, id)
		}
	}
	if len(cleanIds) == 0 {
		dto.Failed("任务ID列表不能为空", c)
		return
	}
	for _, id := range cleanIds {
		if err := service.CronSvc.ChangeFilmCrontab(id, req.State); err != nil {
			dto.Failed(fmt.Sprint("更新失败: ", err.Error()), c)
			return
		}
	}
	dto.SuccessOnlyMsg("定时任务状态更新成功", c)
}

// RunFilmCronTask 手动执行定时任务（批量/单任务，数组入参）
func (h *CronHandler) RunFilmCronTask(c *gin.Context) {
	var req struct {
		Ids []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	cleanIds := make([]string, 0, len(req.Ids))
	for _, id := range req.Ids {
		id = strings.TrimSpace(id)
		if id != "" {
			cleanIds = append(cleanIds, id)
		}
	}
	if len(cleanIds) == 0 {
		dto.Failed("任务ID不能为空", c)
		return
	}
	for _, id := range cleanIds {
		if err := service.CronSvc.RunFilmCronTaskOnce(id); err != nil {
			dto.Failed(fmt.Sprint("手动执行失败: ", err.Error()), c)
			return
		}
	}
	dto.SuccessOnlyMsg("定时任务已触发，执行结果请查看运行日志", c)
}
