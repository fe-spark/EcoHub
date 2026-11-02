package handler

import (
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

type CollectRecordHandler struct{}

var CollectRecordHd = new(CollectRecordHandler)

// HistoryList 分页获取采集运行历史记录
func (h *CollectRecordHandler) HistoryList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("current", c.DefaultQuery("page", "1")))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))
	trigger := strings.TrimSpace(c.DefaultQuery("trigger", ""))
	status := strings.TrimSpace(c.DefaultQuery("status", ""))

	query := model.CollectRecordQuery{
		Page:     page,
		PageSize: pageSize,
		Trigger:  trigger,
		Status:   status,
	}

	begin := strings.TrimSpace(c.DefaultQuery("beginTime", ""))
	if begin != "" {
		if t, err := time.ParseInLocation(time.DateTime, begin, time.Local); err == nil {
			query.BeginTime = t
		}
	}
	end := strings.TrimSpace(c.DefaultQuery("endTime", ""))
	if end != "" {
		if t, err := time.ParseInLocation(time.DateTime, end, time.Local); err == nil {
			query.EndTime = t
		}
	}

	list, total, err := service.CollectRecordSvc.GetHistoryList(query)
	if err != nil {
		dto.Failed("获取采集历史记录失败: "+err.Error(), c)
		return
	}

	dto.Success(gin.H{
		"list":     list,
		"total":    total,
		"current":  query.Page,
		"pageSize": query.PageSize,
	}, "获取采集历史记录成功", c)
}

func (h *CollectRecordHandler) HistoryIncidents(c *gin.Context) {
	list, err := service.CollectRecordSvc.ListOpenIncidents(time.Now())
	if err != nil {
		dto.Failed("获取异常站点失败: "+err.Error(), c)
		return
	}
	if list == nil {
		list = []model.SourceIncident{}
	}
	dto.Success(gin.H{"list": list}, "获取异常站点成功", c)
}

// HistoryDelete 批量删除采集历史记录
func (h *CollectRecordHandler) HistoryDelete(c *gin.Context) {
	var req struct {
		Ids []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if len(req.Ids) == 0 {
		dto.Failed("请选择需要删除的记录", c)
		return
	}

	if err := service.CollectRecordSvc.DeleteHistory(req.Ids); err != nil {
		dto.Failed("删除采集历史记录失败: "+err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("删除成功", c)
}

// HistoryClear 清空全部采集历史记录
func (h *CollectRecordHandler) HistoryClear(c *gin.Context) {
	if err := service.CollectRecordSvc.ClearHistory(); err != nil {
		dto.Failed("清空采集历史记录失败: "+err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("历史记录已清空", c)
}

// HistoryRetry 就地重试某批次中失败的采集站
func (h *CollectRecordHandler) HistoryRetry(c *gin.Context) {
	var req model.CollectRecordRetryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常: "+err.Error(), c)
		return
	}
	result, err := service.CollectRecordSvc.RetryBatchFailedSources(req)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(result, "重试任务已开启", c)
}
