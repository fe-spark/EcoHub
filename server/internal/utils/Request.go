package utils

import (
	"crypto/tls"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gocolly/colly/v2"
)

/*
网络请求, 数据爬取
*/

var Client = CreateClient()

// RequestInfo 请求参数结构体
type RequestInfo struct {
	Uri    string      `json:"uri"`    // 请求url地址
	Params url.Values  `json:"param"`  // 请求参数
	Header http.Header `json:"header"` // 请求头数据
	Resp   []byte      `json:"resp"`   // 响应结果数据
	Err    string      `json:"err"`    // 错误信息
}

// userAgents 现代主流浏览器 UA 池（Chrome / Firefox / Edge，定期更新版本号即可）
var userAgents = []string{
	// Chrome Windows
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	// Chrome macOS
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	// Edge Windows
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36 Edg/122.0.0.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36 Edg/121.0.0.0",
	// Firefox Windows
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:123.0) Gecko/20100101 Firefox/123.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:122.0) Gecko/20100101 Firefox/122.0",
	// Firefox macOS
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 14.3; rv:123.0) Gecko/20100101 Firefox/123.0",
}

// randomUA 从 UA 池中随机选取一个
func randomUA() string {
	return userAgents[rand.Intn(len(userAgents))]
}

// setBrowserHeaders 为请求设置完整的浏览器伪装头部
func setBrowserHeaders(req *colly.Request, referer string) {
	ua := randomUA()
	req.Headers.Set("User-Agent", ua)
	req.Headers.Set("Accept", "application/json, text/plain, */*")
	req.Headers.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	// 不声明 br：Go 标准库不支持 Brotli 解压，声明后服务端可能回 br 编码导致乱码
	req.Headers.Set("Accept-Encoding", "gzip, deflate")
	req.Headers.Set("Connection", "keep-alive")
	req.Headers.Set("Cache-Control", "no-cache")

	// 同域 Referer（仅 host 相同时注入，避免 Sec-Fetch 校验失败）
	if referer != "" && referer != req.URL.String() {
		refHost := extractHost(referer)
		if refHost != "" && refHost == req.URL.Host {
			req.Headers.Set("Referer", referer)
		}
	}
}

// extractHost 从 rawURL 中提取 host，解析失败时返回空字符串
func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// CreateClient 初始化请求客户端（不挂载 OnRequest，由各调用方按需设置）
// 跳过 TLS 证书验证：采集站部分使用过期/自签证书，浏览器会忽略警告继续访问，此处与之对齐
func CreateClient() *colly.Collector {
	c := colly.NewCollector()
	c.MaxDepth = 1
	c.AllowURLRevisit = true
	c.SetRequestTimeout(20 * time.Second)
	c.WithTransport(&http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	})
	return c
}

// ApiGet 请求数据的方法
func ApiGet(r *RequestInfo) {
	c := Client.Clone()

	if r.Header != nil {
		if t, _ := strconv.Atoi(r.Header.Get("timeout")); t > 0 {
			c.SetRequestTimeout(time.Duration(t) * time.Second)
		}
	}

	// 记录本次请求的 referer（局部变量，无竞态）
	var lastURL string
	var targetURL string
	loggedError := false

	c.OnRequest(func(req *colly.Request) {
		setBrowserHeaders(req, lastURL)
	})

	c.OnError(func(response *colly.Response, err error) {
		if err != nil {
			r.Err = err.Error()
		}
		if response != nil && response.Request != nil && response.Request.URL != nil {
			if r.Err == "" {
				r.Err = "request failed"
			}
			r.Err = r.Err + ", status=" + strconv.Itoa(response.StatusCode) + ", url=" + response.Request.URL.String()
			log.Printf("请求异常: URL: %s Error: %s\n", response.Request.URL, err)
			loggedError = true
			return
		}
		if r.Err == "" && targetURL != "" {
			r.Err = "request failed, url=" + targetURL
		}
		log.Printf("请求异常: URL: %s Error: %v\n", targetURL, err)
		loggedError = true
	})

	c.OnResponse(func(response *colly.Response) {
		if (response.StatusCode == 200 || (response.StatusCode >= 300 && response.StatusCode <= 399)) && len(response.Body) > 0 {
			r.Resp = response.Body
			r.Err = ""
		} else {
			r.Resp = []byte{}
			r.Err = "unexpected response status=" + strconv.Itoa(response.StatusCode) + ", url=" + response.Request.URL.String()
		}
		lastURL = response.Request.URL.String()
	})

	targetUrl := buildUrl(r.Uri, r.Params)
	targetURL = targetUrl
	err := c.Visit(targetUrl)
	if err != nil {
		if r.Err == "" {
			r.Err = err.Error()
		}
		if !loggedError {
			log.Println("获取数据失败: ", err)
		}
	}
}

func IsRateLimitedErr(err error) bool {
	if err == nil {
		return false
	}
	return containsStr(err.Error(), "Too Many Requests")
}

// ApiTest 处理API请求后的数据, 主测试
func ApiTest(r *RequestInfo) error {
	c := CreateClient()

	c.OnRequest(func(req *colly.Request) {
		setBrowserHeaders(req, "")
	})

	c.OnResponse(func(response *colly.Response) {
		if (response.StatusCode == 200 || (response.StatusCode >= 300 && response.StatusCode <= 399)) && len(response.Body) > 0 {
			r.Resp = response.Body
		} else {
			r.Resp = []byte{}
		}
	})

	targetUrl := buildUrl(r.Uri, r.Params)
	err := c.Visit(targetUrl)
	if err != nil {
		log.Printf("ApiTest 访问失败: %s, Error: %v\n", targetUrl, err)
	}
	return err
}

// buildUrl 安全地拼接 URL 和参数
func buildUrl(base string, params url.Values) string {
	if len(params) == 0 {
		return base
	}
	u, err := url.Parse(base)
	if err != nil {
		if containsStr(base, "?") {
			return base + "&" + params.Encode()
		}
		return base + "?" + params.Encode()
	}
	q := u.Query()
	for k, v := range params {
		for _, val := range v {
			q.Set(k, val)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
