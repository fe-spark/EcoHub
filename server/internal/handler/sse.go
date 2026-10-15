package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

// errSSEWrite 表示事件已序列化，但写给客户端失败。序列化失败不包这层错误。
var errSSEWrite = errors.New("sse write")

// sseWriter 负责 SSE 响应头、并发写出和刷新。
type sseWriter struct {
	w  http.ResponseWriter
	f  http.Flusher
	mu sync.Mutex
}

// openSSE 开始一条 SSE 响应。当前 Writer 不能刷新时已写失败响应。
func openSSE(c *gin.Context) (*sseWriter, bool) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		dto.Failed("当前环境不支持流式推送", c)
		return nil, false
	}
	flusher.Flush()
	return &sseWriter{w: c.Writer, f: flusher}, true
}

// SendData 写出一条不带事件名的 data。
func (s *sseWriter) SendData(v any) error {
	return s.send("", v)
}

// SendEvent 写出一条带事件名的 data。
func (s *sseWriter) SendEvent(name string, v any) error {
	return s.send(name, v)
}

// Comment 写出 SSE 注释行，用于心跳。
func (s *sseWriter) Comment(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return fmt.Errorf("%w: %s", errSSEWrite, err.Error())
	}
	s.f.Flush()
	return nil
}

func (s *sseWriter) send(event string, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var writeErr error
	if event == "" {
		_, writeErr = fmt.Fprintf(s.w, "data: %s\n\n", payload)
	} else {
		_, writeErr = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, payload)
	}
	if writeErr != nil {
		return fmt.Errorf("%w: %s", errSSEWrite, writeErr.Error())
	}
	s.f.Flush()
	return nil
}
