package collectctl

import (
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/model/dto"

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
		Page:        page,
		PageSize:    pageSize,
		Trigger:     trigger,
		Status:      status,
		BatchID:     strings.TrimSpace(c.Query("batchId")),
		UpdatedOnly: c.DefaultQuery("updated", "") == "1",
		FailedOnly:  c.DefaultQuery("failed", "") == "1",
	}

	begin := strings.TrimSpace(c.DefaultQuery("beginTime", ""))
	if begin != "" {
		if t, err := time.ParseInLocation(time.DateTime, begin, time.Local); err == nil {
			query.BeginTime = t
		} else if t, err := time.ParseInLocation(time.DateOnly, begin, time.Local); err == nil {
			query.BeginTime = t
		}
	}
	end := strings.TrimSpace(c.DefaultQuery("endTime", ""))
	if end != "" {
		if t, err := time.ParseInLocation(time.DateTime, end, time.Local); err == nil {
			query.EndTime = t
		} else if t, err := time.ParseInLocation(time.DateOnly, end, time.Local); err == nil {
			query.EndTime = t.Add(24*time.Hour - time.Nanosecond)
		}
	}

	list, total, err := CollectRecordSvc.GetHistoryList(query)
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

// HistoryFilms 分页获取某批次某站点入库/更新的全部影片清单
func (h *CollectRecordHandler) HistoryFilms(c *gin.Context) {
	batchID := strings.TrimSpace(c.Query("batchId"))
	if batchID == "" {
		dto.Failed("缺少批次号参数", c)
		return
	}
	sourceID := strings.TrimSpace(c.Query("sourceId"))
	afterSource := strings.TrimSpace(c.Query("afterSource"))
	afterMid, _ := strconv.ParseInt(c.Query("afterMid"), 10, 64)
	afterName := c.Query("afterName")
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}
	keyword := strings.TrimSpace(c.Query("keyword"))

	page, err := CollectRecordSvc.GetBatchFilms(model.CollectRecordFilmQuery{
		BatchID:     batchID,
		SourceID:    sourceID,
		AfterSource: afterSource,
		AfterName:   afterName,
		AfterMid:    afterMid,
		PageSize:    pageSize,
		Keyword:     keyword,
	})
	if err != nil {
		dto.Failed("获取批次影片清单失败: "+err.Error(), c)
		return
	}
	if page.List == nil {
		page.List = []model.CollectRecordFilm{}
	}

	dto.Success(gin.H{
		"list":            page.List,
		"total":           page.Total,
		"pageSize":        pageSize,
		"nextAfterSource": page.NextAfterSource,
		"nextAfterName":   page.NextAfterName,
		"nextAfterMid":    page.NextAfterMid,
		"hasMore":         page.HasMore,
		"truncated":       page.Truncated,
	}, "获取批次影片清单成功", c)
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

	if err := CollectRecordSvc.DeleteHistory(req.Ids); err != nil {
		dto.Failed("删除采集历史记录失败: "+err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("删除成功", c)
}

// HistoryClear 清空全部采集历史记录
func (h *CollectRecordHandler) HistoryClear(c *gin.Context) {
	if err := CollectRecordSvc.ClearHistory(); err != nil {
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
	result, err := CollectRecordSvc.RetryBatchFailedSources(req)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(result, "重试任务已开启", c)
}
