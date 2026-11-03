package handler

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/spider"
	"server/internal/spider/progress"

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

// HistoryFilms 分页获取某批次某站点入库/更新的全部影片清单
func (h *CollectRecordHandler) HistoryFilms(c *gin.Context) {
	batchID := strings.TrimSpace(c.Query("batchId"))
	if batchID == "" {
		dto.Failed("缺少批次号参数", c)
		return
	}
	sourceID := strings.TrimSpace(c.Query("sourceId"))
	page, _ := strconv.Atoi(c.DefaultQuery("current", c.DefaultQuery("page", "1")))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	keyword := strings.TrimSpace(c.Query("keyword"))

	q := model.CollectRecordFilmQuery{
		BatchID:  batchID,
		SourceID: sourceID,
		Page:     page,
		PageSize: pageSize,
		Keyword:  keyword,
	}

	list, total, err := service.CollectRecordSvc.GetBatchFilms(q)
	if err != nil {
		dto.Failed("获取批次影片清单失败: "+err.Error(), c)
		return
	}
	if list == nil {
		list = []model.CollectRecordFilm{}
	}

	dto.Success(gin.H{
		"list":     list,
		"total":    total,
		"current":  q.Page,
		"pageSize": q.PageSize,
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

// HistoryStream 当前页采集任务实时进度与状态 SSE 长连接推送
func (h *CollectRecordHandler) HistoryStream(c *gin.Context) {
	rawBatchIDs := strings.TrimSpace(c.Query("batchIds"))
	if rawBatchIDs == "" {
		dto.Failed("缺少批次标识参数", c)
		return
	}
	parts := strings.Split(rawBatchIDs, ",")
	batchIDSet := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			batchIDSet[p] = struct{}{}
		}
	}
	if len(batchIDSet) == 0 {
		dto.Failed("批次列表为空", c)
		return
	}

	stream, ok := openSSE(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	progressCh := progress.Subscribe(ctx)
	completionCh := spider.SubscribeRecordCompletion(ctx)

	initialLive := spider.GetActiveBatchesLiveProgress(batchIDSet)
	runningIDs := make([]string, 0, len(initialLive))
	for _, l := range initialLive {
		runningIDs = append(runningIDs, l.BatchID)
	}
	if err := stream.SendEvent("record_snapshot", gin.H{"batchIds": runningIDs}); err != nil && errors.Is(err, errSSEWrite) {
		return
	}
	for _, l := range initialLive {
		if err := stream.SendEvent("record_progress", l); err != nil && errors.Is(err, errSSEWrite) {
			return
		}
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-progressCh:
			if !ok {
				return
			}
			liveList := spider.GetActiveBatchesLiveProgress(batchIDSet)
			for _, l := range liveList {
				if err := stream.SendEvent("record_progress", l); err != nil && errors.Is(err, errSSEWrite) {
					return
				}
			}
		case comp, ok := <-completionCh:
			if !ok {
				return
			}
			if comp != nil {
				if _, ok := batchIDSet[comp.BatchID]; ok {
					if err := stream.SendEvent("record_finished", comp); err != nil && errors.Is(err, errSSEWrite) {
						return
					}
				}
			}
		case <-heartbeat.C:
			if err := stream.Comment("ping"); err != nil {
				return
			}
		}
	}
}
