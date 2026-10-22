package handler

import (
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

// StopTask 停止采集任务（统一批量 ids，单站传包含单个 id 的数组）
func (h *SpiderHandler) StopTask(c *gin.Context) {
	var req struct {
		Ids []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
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
		dto.Failed("参数异常，采集站标识不能为空", c)
		return
	}
	service.SpiderSvc.StopTasks(cleanIds)
	dto.SuccessOnlyMsg("已发送停止指令，采集任务正在停止", c)
}

// StarSpider 开启并执行采集任务（统一批量 ids，单站传包含单个 id 的数组）
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
	cleanIds := make([]string, 0, len(cp.Ids))
	for _, id := range cp.Ids {
		id = strings.TrimSpace(id)
		if id != "" {
			cleanIds = append(cleanIds, id)
		}
	}
	if len(cleanIds) == 0 {
		dto.Failed("采集开启失败, 关联的资源站信息为空", c)
		return
	}

	result := service.SpiderSvc.BatchCollect(cp.Time, cleanIds)
	msg := batchOutcomeMessage(result, "已开启", "没有可采集的站点")
	if len(result.Started) == 0 {
		dto.FailedWithData(result, msg, c)
		return
	}
	dto.Success(result, msg, c)
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
	stats, err := service.SpiderSvc.InventoryStats(c.Query("scope"))
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(stats, "获取成功", c)
}
