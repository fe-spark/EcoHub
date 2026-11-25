package collectctl

import (
	"strings"

	"server/internal/model"
	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
	"server/internal/collect"
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
	go collect.SpiderSvc.StopTasks(cleanIds)
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

	result := collect.SpiderSvc.BatchCollect(cp.Time, cleanIds)
	msg := collect.BatchOutcomeMessage(result, "已开启", "没有可采集的站点")
	if len(result.Started) == 0 {
		dto.FailedWithData(result, msg, c)
		return
	}
	dto.Success(result, msg, c)
}

// InventoryStats 返回工作台片库规模。refresh=1 时跳过短缓存触发实时重算。
func (h *SpiderHandler) InventoryStats(c *gin.Context) {
	refresh := c.Query("refresh") == "1"
	stats, err := collect.SpiderSvc.InventoryStats(c.Request.Context(), refresh)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(stats, "获取成功", c)
}
