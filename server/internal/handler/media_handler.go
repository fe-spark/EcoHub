package handler

import (
	"encoding/base64"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type MediaHandler struct{}

var MediaHd = new(MediaHandler)

// 全局 64 并发槽位控制，防止流媒体连接过多打满资源
var mediaStreamSem = make(chan struct{}, 64)

// 上游 WebDAV 专用 HTTP 客户端：长连接保活、快拨测、大文件 Body 无总超时。
// 不自动跟随重定向：小雅/Alist 会对文件 GET 返回 307 到阿里云盘 CDN，
// Go 客户端跟随后常被 OSS 以 403 拒绝；改为把 307 交给浏览器直连。
var mediaHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:       utils.WebDAVInsecureTLS(),
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       60 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   32,
	},
}

// Stream 处理媒体网关播放与切片流（GET / HEAD /api/media/stream）
func (h *MediaHandler) Stream(c *gin.Context) {
	start := time.Now()

	// 并发信号量控制
	select {
	case mediaStreamSem <- struct{}{}:
		defer func() { <-mediaStreamSem }()
	default:
		c.Header("Retry-After", "2")
		c.String(http.StatusServiceUnavailable, "流媒体网关繁忙，请稍后重试")
		return
	}

	sidStr := strings.TrimSpace(c.Query("sid"))
	pEncoded := strings.TrimSpace(c.Query("p"))
	ext := strings.ToLower(strings.TrimSpace(c.Query("ext")))
	sign := strings.TrimSpace(c.Query("sign"))

	if sidStr == "" || pEncoded == "" || sign == "" {
		c.String(http.StatusBadRequest, "缺少必要参数")
		return
	}

	sid, err := strconv.ParseInt(sidStr, 10, 64)
	if err != nil || sid <= 0 {
		c.String(http.StatusBadRequest, "无效的资源站点标识")
		return
	}

	rawBytes, err := base64.RawURLEncoding.DecodeString(pEncoded)
	if err != nil {
		rawBytes, err = base64.URLEncoding.DecodeString(pEncoded)
		if err != nil {
			c.String(http.StatusBadRequest, "无效的路径参数")
			return
		}
	}

	canonicalRelPath, err := service.CanonicalizeWebDAVRelPath(string(rawBytes))
	if err != nil || canonicalRelPath == "" {
		c.String(http.StatusBadRequest, "无效的媒体相对路径")
		return
	}

	// 签名验签
	if !service.VerifyMediaStreamSign(sid, canonicalRelPath, sign) {
		c.String(http.StatusForbidden, "播放签名无效或已过期")
		return
	}

	// 查找对应 WebDAV 采集源
	source := repository.FindCollectSourceById(strconv.FormatInt(sid, 10))
	if source == nil || source.SourceType != model.SourceTypeWebdav || !source.State {
		c.String(http.StatusNotFound, "资源站不存在或已被禁用")
		return
	}

	cfg, err := source.GetWebdavConfig()
	if err != nil || cfg.ServerURL == "" {
		c.String(http.StatusInternalServerError, "资源站配置异常")
		return
	}

	fileURL, err := service.BuildWebDAVFileURL(cfg.ServerURL, cfg.RootPath, canonicalRelPath)
	if err != nil || fileURL == nil {
		c.String(http.StatusInternalServerError, "资源站 WebDAV 地址格式错误")
		return
	}
	targetURL := fileURL.String()

	// 客户端 Context 联动取消：当客户端 Seek、暂停或断开连接时，立即 Cancel 上游请求并释放槽位
	ctx := c.Request.Context()

	if c.Request.Method == http.MethodHead {
		headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, targetURL, nil)
		if err != nil {
			c.String(http.StatusInternalServerError, "创建预检请求失败")
			return
		}
		if cfg.Username != "" {
			headReq.SetBasicAuth(cfg.Username, cfg.Password)
		}

		headResp, err := mediaHTTPClient.Do(headReq)
		if err == nil && headResp != nil {
			if handled := handleUpstreamRedirect(c, sid, headReq.URL, headResp, start); handled {
				return
			}
			if headResp.StatusCode == http.StatusOK || headResp.StatusCode == http.StatusPartialContent {
				defer headResp.Body.Close()
				copyMediaHeaders(c, headResp, ext)
				c.Status(headResp.StatusCode)
				return
			}
			_ = headResp.Body.Close()
		}

		// 上游不支持 HEAD 时回退到 Range: bytes=0-0 的 GET 探测
		getProbeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			c.String(http.StatusInternalServerError, "创建探测请求失败")
			return
		}
		if cfg.Username != "" {
			getProbeReq.SetBasicAuth(cfg.Username, cfg.Password)
		}
		getProbeReq.Header.Set("Range", "bytes=0-0")
		getProbeResp, err := mediaHTTPClient.Do(getProbeReq)
		if err != nil {
			c.String(http.StatusBadGateway, "探测上游网关失败")
			return
		}
		if handled := handleUpstreamRedirect(c, sid, getProbeReq.URL, getProbeResp, start); handled {
			return
		}
		defer getProbeResp.Body.Close()
		_, _ = io.Copy(io.Discard, getProbeResp.Body)
		writeHeadFromRangeProbe(c, getProbeResp, ext)
		return
	}

	// GET 数据流转发
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "创建上游请求失败")
		return
	}
	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}
	if rangeHeader := c.Request.Header.Get("Range"); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	if ifRange := c.Request.Header.Get("If-Range"); ifRange != "" {
		req.Header.Set("If-Range", ifRange)
	}

	resp, err := mediaHTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return // 客户端已断开
		}
		c.String(http.StatusBadGateway, "连接上游 WebDAV 失败")
		return
	}
	if handled := handleUpstreamRedirect(c, sid, req.URL, resp, start); handled {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		log.Printf("[MediaStream] sid=%d upstream=%s status=%d cost=%v", sid, targetURL, resp.StatusCode, time.Since(start))
		c.String(http.StatusBadGateway, "上游 WebDAV 拒绝访问，请检查路径编码或账号权限")
		return
	}
	if resp.StatusCode == http.StatusNotFound {
		log.Printf("[MediaStream] sid=%d upstream=%s status=404 cost=%v", sid, targetURL, time.Since(start))
		c.String(http.StatusNotFound, "上游文件不存在")
		return
	}

	copyMediaHeaders(c, resp, ext)
	c.Status(resp.StatusCode)

	written, _ := io.Copy(c.Writer, resp.Body)
	log.Printf("[MediaStream] sid=%d status=%d bytes=%d ext=%s cost=%v", sid, resp.StatusCode, written, ext, time.Since(start))
}

func copyMediaHeaders(c *gin.Context, resp *http.Response, ext string) {
	contentType := resolveContentTypeByExt(ext)
	if contentType == "" {
		contentType = resp.Header.Get("Content-Type")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)

	if cr := resp.Header.Get("Content-Range"); cr != "" {
		c.Header("Content-Range", cr)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		c.Header("Content-Length", cl)
	}
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		c.Header("Last-Modified", lm)
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		c.Header("ETag", etag)
	}
	c.Header("Accept-Ranges", "bytes")

	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Range, Authorization")
	c.Header("Access-Control-Expose-Headers", "Content-Range, Accept-Ranges, Content-Length")
}

func resolveContentTypeByExt(ext string) string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	switch ext {
	case "mkv":
		return "video/x-matroska"
	case "mp4":
		return "video/mp4"
	case "iso":
		return "application/octet-stream"
	case "ts", "m2ts":
		return "video/mp2t"
	case "avi":
		return "video/x-msvideo"
	case "mov":
		return "video/quicktime"
	case "flv":
		return "video/x-flv"
	case "webm":
		return "video/webm"
	default:
		return ""
	}
}

func isRedirectStatus(code int) bool {
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func handleUpstreamRedirect(c *gin.Context, sid int64, reqURL *url.URL, resp *http.Response, start time.Time) bool {
	if resp == nil || reqURL == nil || !isRedirectStatus(resp.StatusCode) {
		return false
	}
	defer resp.Body.Close()
	loc := strings.TrimSpace(resp.Header.Get("Location"))
	if loc == "" {
		return false
	}
	next, err := reqURL.Parse(loc)
	if err != nil || next.Host == "" || (next.Scheme != "http" && next.Scheme != "https") {
		return false
	}
	if strings.EqualFold(reqURL.Host, next.Host) {
		return false
	}
	log.Printf("[MediaStream] sid=%d upstream 307/302 -> %s cost=%v", sid, next.Host, time.Since(start))
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusTemporaryRedirect, next.String())
	return true
}

func writeHeadFromRangeProbe(c *gin.Context, probe *http.Response, ext string) {
	copyMediaHeaders(c, probe, ext)
	if total, ok := parseContentRangeTotal(probe.Header.Get("Content-Range")); ok {
		c.Header("Content-Length", strconv.FormatInt(total, 10))
		c.Writer.Header().Del("Content-Range")
		c.Status(http.StatusOK)
		return
	}
	c.Status(probe.StatusCode)
}

func parseContentRangeTotal(cr string) (int64, bool) {
	i := strings.LastIndex(cr, "/")
	if i < 0 || i+1 >= len(cr) {
		return 0, false
	}
	totalStr := strings.TrimSpace(cr[i+1:])
	if totalStr == "" || totalStr == "*" {
		return 0, false
	}
	n, err := strconv.ParseInt(totalStr, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// ResolveStreamBase 解析流媒体对外基准地址。
// 1. 若配置了 MEDIA_STREAM_PUBLIC_BASE 则强制使用（去尾斜杠绝对前缀）。
// 2. 否则从 Host / X-Forwarded-* 推导，并把 Next.js :3000 改写为 compose 映射的 :18080。
// 3. Docker 内部服务名对浏览器不可达，丢弃以免下发必死链接。
func ResolveStreamBase(c *gin.Context, requireAbsolute bool) string {
	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("MEDIA_STREAM_PUBLIC_BASE")), "/"); base != "" {
		return base
	}
	if base, err := resolveProvideBaseURL(c); err == nil && base != "" {
		return rewriteStreamBaseForBrowser(base)
	}
	return ""
}

func rewriteStreamBaseForBrowser(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		host = u.Host
		port = ""
	}
	if isDockerInternalStreamHost(host) {
		return ""
	}
	if port == "3000" {
		u.Host = net.JoinHostPort(host, "18080")
	}
	return strings.TrimRight(u.String(), "/")
}

func isDockerInternalStreamHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	switch h {
	case "server", "web", "mysql", "redis":
		return true
	}
	return strings.HasSuffix(h, ".internal")
}
