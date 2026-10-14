package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"server/internal/model/dto"
	"server/internal/spider/progress"

	"github.com/gin-gonic/gin"
)

// ProgressStream 采集中心实时进度 SSE 长连接端点
func (h *CollectHandler) ProgressStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		dto.Failed("当前环境不支持流式推送", c)
		return
	}
	flusher.Flush()

	subCh := progress.Subscribe(c.Request.Context())
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case list, ok := <-subCh:
			if !ok {
				return
			}
			payload, err := json.Marshal(list)
			if err != nil {
				continue
			}
			_, err = fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", payload)
			if err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			_, err := fmt.Fprintf(c.Writer, ": ping\n\n")
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
