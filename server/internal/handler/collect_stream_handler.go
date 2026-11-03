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
	deleteCh := progress.SubscribeSourceDelete(c.Request.Context())
	searchIndexCh := progress.SubscribeSearchIndex(c.Request.Context())
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// 连接建立时下发全量采集站分类同步状态快照
	statusMap, err := service.CollectSvc.GetCategoryStatusMap()
	if err != nil {
		return
	}
	if err := stream.SendEvent("category_snapshot", statusMap); err != nil {
		if errors.Is(err, errSSEWrite) {
			return
		}
	}

	// 连接建立时下发当前删除中的站点快照
	deletingIDs := progress.GetDeletingSourceIDs()
	if len(deletingIDs) > 0 {
		if err := stream.SendEvent("delete_snapshot", deletingIDs); err != nil {
			if errors.Is(err, errSSEWrite) {
				return
			}
		}
	}

	// 慢查询期间通道里会积压旧进度，快照必须读最新一份。
	discardSearchIndexProgress(searchIndexCh)
	latest := service.CollectSvc.SearchIndexProgress()
	if err := stream.SendEvent("search_index_progress", latest); err != nil {
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
		case evt, ok := <-deleteCh:
			if !ok {
				return
			}
			if err := stream.SendEvent("source_delete", evt); err != nil {
				if errors.Is(err, errSSEWrite) {
					return
				}
			}
		case evt, ok := <-searchIndexCh:
			if !ok {
				return
			}
			if err := stream.SendEvent("search_index_progress", evt); err != nil {
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

func discardSearchIndexProgress(ch <-chan progress.SearchIndexProgressEvent) {
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		default:
			return
		}
	}
}
