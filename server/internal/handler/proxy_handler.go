package handler

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ProxyHandler struct {
	mu          sync.RWMutex
	proxyClient *http.Client
}

var ProxyHd = &ProxyHandler{}

const proxyUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"

func (h *ProxyHandler) getClient() *http.Client {
	h.mu.RLock()
	if h.proxyClient != nil {
		client := h.proxyClient
		h.mu.RUnlock()
		return client
	}
	h.mu.RUnlock()

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.proxyClient != nil {
		return h.proxyClient
	}

	h.proxyClient = &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConnsPerHost:   32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
	return h.proxyClient
}

func (h *ProxyHandler) ProxyVideo(c *gin.Context) {
	target := c.Query("url")
	if target == "" {
		c.AbortWithStatus(400)
		return
	}
	base, _ := url.Parse(target)

	req, _ := http.NewRequestWithContext(c.Request.Context(), "GET", target, nil)
	req.Header.Set("User-Agent", proxyUA)
	// 强制不压缩，确保 upstream 支持 Range (206 Partial Content)
	req.Header.Set("Accept-Encoding", "identity")

	// 只有 .ts 视频分片需要 Range 支持，M3U8 必须请求完整文件以便重写
	isM3U8 := strings.Contains(target, ".m3u8")
	if r := c.GetHeader("Range"); r != "" && !isM3U8 {
		req.Header.Set("Range", r)
	}

	resp, err := h.getClient().Do(req)
	if err != nil {
		c.String(502, "proxy upstream request failed")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		c.Status(resp.StatusCode)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	// 只有 M3U8 需要特殊解析重写
	isM3U8 = isM3U8 || strings.Contains(contentType, "mpegurl")

	if !isM3U8 {
		copyProxyHeaders(c.Writer.Header(), resp.Header, []string{
			"Content-Type",
			"Content-Length",
			"Accept-Ranges",
			"Content-Range",
			"Cache-Control",
			"ETag",
			"Last-Modified",
			"Expires",
		})
		c.Status(resp.StatusCode)
		_, _ = io.Copy(c.Writer, resp.Body)
		return
	}

	// M3U8 路径补全重写：大道至简
	c.Status(resp.StatusCode)
	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.Header("Cache-Control", "no-cache")
	scanner := bufio.NewScanner(resp.Body)
	// 针对含有内联 KEY 的超长行扩大缓冲区
	scanner.Buffer(make([]byte, 512*1024), 512*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			fmt.Fprintln(c.Writer, "")
			continue
		}

		// 1. 匹配分片/播放列表行 (非 # 开头)
		if !strings.HasPrefix(line, "#") {
			line = h.wrap(base, line)
		} else if strings.Contains(line, "URI=\"") {
			// 2. 匹配标签内的 URI (如 #EXT-X-KEY:URI="...")
			line = h.rewriteTagURI(base, line)
		}
		fmt.Fprintln(c.Writer, line)
	}
}

func copyProxyHeaders(dst http.Header, src http.Header, keys []string) {
	for _, k := range keys {
		if v := src.Get(k); v != "" {
			dst.Set(k, v)
		}
	}
}

// wrap 将原始 URL 转换为代理 URL，自动处理相对/绝对路径
func (h *ProxyHandler) wrap(base *url.URL, ref string) string {
	if ref == "" || strings.HasPrefix(ref, "data:") {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	// 在生成的代理链接末尾添加 &.ts 辅助识别
	suffix := ""
	if strings.Contains(ref, ".ts") {
		suffix = "&.ts"
	}
	return "/api/proxy/video?url=" + url.QueryEscape(base.ResolveReference(u).String()) + suffix
}

// rewriteTagURI 查找并替换字符串中的 URI="..." 部分
func (h *ProxyHandler) rewriteTagURI(base *url.URL, line string) string {
	start := strings.Index(line, "URI=\"") + 5
	end := strings.Index(line[start:], "\"") + start
	if start < 5 || end <= start {
		return line
	}
	uri := line[start:end]
	return line[:start] + h.wrap(base, uri) + line[end:]
}
