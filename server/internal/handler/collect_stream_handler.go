package handler

import (
	"errors"
	"time"

	"server/internal/service"
	"server/internal/spider/progress"

	"github.com/gin-gonic/gin"
)

// ProgressStream 采集中心实时进度与分类状态 SSE 长连接端点
func (h *CollectHandler) ProgressStream(c *gin.Context) {
	stream, ok := openSSE(c)
	if !ok {
		return
	}

	progressCh := progress.Subscribe(c.Request.Context())
	categoryCh := progress.SubscribeCategory(c.Request.Context())
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// 连接建立时下发全量采集站分类同步状态快照
	statusMap := service.CollectSvc.GetCategoryStatusMap()
	if err := stream.SendEvent("category_snapshot", statusMap); err != nil {
		if errors.Is(err, errSSEWrite) {
			return
		}
	}

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case list, ok := <-progressCh:
			if !ok {
				return
			}
			if err := stream.SendEvent("progress", list); err != nil {
				if errors.Is(err, errSSEWrite) {
					return
				}
			}
		case evt, ok := <-categoryCh:
			if !ok {
				return
			}
			if err := stream.SendEvent("category_status", evt); err != nil {
				if errors.Is(err, errSSEWrite) {
					return
				}
			}
		case <-heartbeat.C:
			if err := stream.Comment("ping"); err != nil {
				return
			}
		}
	}
}
