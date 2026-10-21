package handler

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/spider"
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
		case <-heartbeat.C:
			if err := stream.Comment("ping"); err != nil {
				return
			}
		}
	}
}

const recordStreamIDLimit = 500

func parseRecordStreamIDs(raw string) ([]uint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("缺少当前页记录")
	}
	parts := strings.Split(raw, ",")
	if len(parts) > recordStreamIDLimit {
		return nil, errors.New("当前页记录过多")
	}
	ids := make([]uint, 0, len(parts))
	seen := make(map[uint]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil || n == 0 {
			return nil, errors.New("记录编号无效")
		}
		id := uint(n)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, errors.New("缺少当前页记录")
	}
	return ids, nil
}

// RecordStream 只推当前页失败记录的重试状态。
func (h *CollectHandler) RecordStream(c *gin.Context) {
	ids, err := parseRecordStreamIDs(c.Query("ids"))
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	stream, ok := openSSE(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	events := spider.SubscribeFailureRetry(ctx, ids)
	inflight := make([]uint, 0)
	for _, ev := range spider.FailureRetrySnapshot(ids) {
		inflight = append(inflight, ev.ID)
	}
	if err := stream.SendEvent("record_snapshot", gin.H{"ids": inflight}); err != nil && errors.Is(err, errSSEWrite) {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case batch, ok := <-events:
			if !ok {
				return
			}
			for _, ev := range batch {
				if err := stream.SendEvent("record", ev); err != nil && errors.Is(err, errSSEWrite) {
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
