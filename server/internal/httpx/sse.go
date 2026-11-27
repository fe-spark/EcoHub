package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

const sseWriteTimeout = 15 * time.Second

// ErrSSEWrite 表示事件已序列化，但写给客户端失败。序列化失败不包这层错误。
var ErrSSEWrite = errors.New("sse write")

// SSEWriter 负责 SSE 响应头、并发写出与写超时保护。
type SSEWriter struct {
	w  http.ResponseWriter
	f  http.Flusher
	mu sync.Mutex
}

// OpenSSE 开始一条 SSE 响应。当前 Writer 不能刷新或握手写出失败时返回 false。
func OpenSSE(c *gin.Context) (*SSEWriter, bool) {
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
	writer := &SSEWriter{w: c.Writer, f: flusher}
	if err := writer.Comment("ready"); err != nil {
		return nil, false
	}
	return writer, true
}

// SendData 写出一条不带事件名的 data。
func (s *SSEWriter) SendData(v any) error {
	return s.send("", v)
}

// SendEvent 写出一条带事件名的 data。
func (s *SSEWriter) SendEvent(name string, v any) error {
	return s.send(name, v)
}

// Comment 写出 SSE 注释行，用于心跳。
func (s *SSEWriter) Comment(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = http.NewResponseController(s.w).SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return fmt.Errorf("%w: %s", ErrSSEWrite, err.Error())
	}
	s.f.Flush()
	return nil
}

func (s *SSEWriter) send(event string, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = http.NewResponseController(s.w).SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	var writeErr error
	if event == "" {
		_, writeErr = fmt.Fprintf(s.w, "data: %s\n\n", payload)
	} else {
		_, writeErr = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, payload)
	}
	if writeErr != nil {
		return fmt.Errorf("%w: %s", ErrSSEWrite, writeErr.Error())
	}
	s.f.Flush()
	return nil
}
