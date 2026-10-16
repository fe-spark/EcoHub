package handler

import (
	"fmt"
	"net/http"
	"strings"

	"server/internal/config"
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
		result := service.SpiderSvc.BatchCollect(cp.Time, cp.Ids)
		msg := batchOutcomeMessage(result, "已开启", "没有可采集的站点")
		if len(result.Started) == 0 {
			dto.FailedWithData(result, msg, c)
			return
		}
		dto.Success(result, msg, c)
		return
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

// ClearAllFilm 校验通过后以 SSE 推送数据重置进度。重置在后台跑完，连接断开不中止。
func (h *SpiderHandler) ClearAllFilm(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
		return
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "权限不足，仅超级管理员可执行数据重置", c)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	if !verifyManagePassword(c, req.Password) {
		dto.Failed("重置失败, 密钥校验失败!!!", c)
		return
	}
	if service.SpiderSvc.SiteResetRunning() {
		dto.Failed("站点数据正在重置中，请勿重复操作", c)
		return
	}
	stream, ok := openSSE(c)
	if !ok {
		return
	}
	if err := service.SpiderSvc.StreamSiteReset(c.Request.Context(), stream.SendData); err != nil {
		_ = stream.SendData(map[string]any{"running": false, "error": err.Error()})
	}
}

// ResetProgressStream 订阅正在进行的数据重置进度。刷新页面后靠它接上。
func (h *SpiderHandler) ResetProgressStream(c *gin.Context) {
	stream, ok := openSSE(c)
	if !ok {
		return
	}
	_ = service.SpiderSvc.FollowSiteReset(c.Request.Context(), stream.SendData)
}

// InventoryStats 返回工作台片库规模。scope=sources 时只返回各采集站。
func (h *SpiderHandler) InventoryStats(c *gin.Context) {
	dto.Success(service.SpiderSvc.InventoryStats(c.Query("scope")), "获取成功", c)
}

// UpdateSpider 影片主站更新采集（兼容单个与批量）
func (h *SpiderHandler) UpdateSpider(c *gin.Context) {
	var req struct {
		Ids []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数格式异常", c)
		return
	}
	if len(req.Ids) == 0 {
		dto.Failed("缺少影片 ID", c)
		return
	}
	service.SpiderSvc.CollectFilms(req.Ids)
	dto.SuccessOnlyMsg("更新任务已开启", c)
}

// StopAllTasks 一键终止所有采集中任务
func (h *SpiderHandler) StopAllTasks(c *gin.Context) {
	service.SpiderSvc.StopAllTasks()
	dto.SuccessOnlyMsg("已发送终止指令，所有采集任务正在停止", c)
}
