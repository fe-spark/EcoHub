package handler

import (
	"fmt"
	"strings"
	"sync/atomic"

	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type SpiderHandler struct{}

var SpiderHd = new(SpiderHandler)

// StopTask 停止单个采集任务（仅停止任务，不禁用采集站）。
func (h *SpiderHandler) StopTask(c *gin.Context) {
	var req struct {
		Id string `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if strings.TrimSpace(req.Id) == "" {
		dto.Failed("参数异常，采集站标识不能为空", c)
		return
	}
	if err := service.SpiderSvc.StopTask(req.Id); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("已发送停止指令，采集任务正在停止", c)
}

// StarSpider 开启并执行采集任务
func (h *SpiderHandler) StarSpider(c *gin.Context) {
	var cp model.CollectParams
	if err := c.ShouldBindJSON(&cp); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	if cp.Time == 0 {
		dto.Failed("采集开启失败,采集时长不能为0", c)
		return
	}

	if cp.Batch {
		if len(cp.Ids) <= 0 {
			dto.Failed("批量采集开启失败, 关联的资源站信息为空", c)
			return
		}
		if err := service.SpiderSvc.BatchCollect(cp.Time, cp.Ids); err != nil {
			dto.Failed(err.Error(), c)
			return
		}
	} else {
		if len(cp.Id) <= 0 {
			dto.Failed("批量采集开启失败, 资源站Id获取失败", c)
			return
		}
		if err := service.SpiderSvc.StartCollect(cp.Id, cp.Time); err != nil {
			dto.Failed(fmt.Sprint("采集任务开启失败: ", err.Error()), c)
			return
		}
	}
	dto.SuccessOnlyMsg("采集任务已成功开启!!!", c)
}

// resetSiteDataRunning 防止并发重复触发站点数据重置（重置为长耗时异步任务）
var resetSiteDataRunning atomic.Bool

// ClearAllFilm 删除所有film信息：密钥校验通过后立即返回成功，重置在后台异步执行。
func (h *SpiderHandler) ClearAllFilm(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	if !verifyPassword(c, req.Password) {
		dto.Failed("重置失败, 密钥校验失败!!!", c)
		return
	}
	if !resetSiteDataRunning.CompareAndSwap(false, true) {
		dto.Failed("站点数据正在重置中，请勿重复操作", c)
		return
	}
	go func() {
		defer resetSiteDataRunning.Store(false)
		if err := service.SpiderSvc.ClearFilms(); err != nil {
			syslog.Errorf("[数据重置] 重置失败: %v", err)
			return
		}
		syslog.Infof("[数据重置] 重置完成")
	}()
	dto.SuccessOnlyMsg("数据重置已开始，正在清空影视与采集数据", c)
}

// ResetProgress 返回数据重置实时进度（供前端轮询）
func (h *SpiderHandler) ResetProgress(c *gin.Context) {
	dto.Success(service.SpiderSvc.ResetProgress(), "获取成功", c)
}

// ResetImpactStats 返回数据重置影响面统计（将清空的数据量）
func (h *SpiderHandler) ResetImpactStats(c *gin.Context) {
	dto.Success(service.SpiderSvc.ResetImpactStats(), "获取成功", c)
}

// SingleUpdateSpider 单一影片主站更新采集
func (h *SpiderHandler) SingleUpdateSpider(c *gin.Context) {
	var req struct {
		Ids string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	ids := req.Ids
	if ids == "" {
		dto.Failed("参数异常, 资源标识ID信息缺失", c)
		return
	}
	service.SpiderSvc.SyncCollect(ids)
	dto.SuccessOnlyMsg("主站影片更新任务已成功开启!!!", c)
}

// StopAllTasks 一键终止所有采集中任务
func (h *SpiderHandler) StopAllTasks(c *gin.Context) {
	service.SpiderSvc.StopAllTasks()
	dto.SuccessOnlyMsg("已发送终止指令，所有采集任务正在停止", c)
}

func verifyPassword(c *gin.Context, password string) bool {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.Failed("操作失败,登录信息异常!!!", c)
		return false
	}
	uc := v.(*utils.UserClaims)
	return service.UserSvc.VerifyUserPassword(uc.UserID, password)
}
