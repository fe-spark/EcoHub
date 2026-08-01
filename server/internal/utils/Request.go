package utils

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
)

/*
网络请求, 数据爬取
*/

// 共享 HTTP Transport，复用 TCP 长连接与 DNS 缓存
var sharedTransport = &http.Transport{
	MaxIdleConns:        200,
	MaxIdleConnsPerHost: 30,
	IdleConnTimeout:     90 * time.Second,
	TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
}

// 共享单例 HTTP 客户端
var httpClient = &http.Client{
	Transport: sharedTransport,
	Timeout:   20 * time.Second,
}

var Client = CreateClient()

// RequestInfo 请求参数结构体
type RequestInfo struct {
	Uri    string      `json:"uri"`    // 请求url地址
	Params url.Values  `json:"param"`  // 请求参数
	Header http.Header `json:"header"` // 请求头数据
	Resp   []byte      `json:"resp"`   // 响应结果数据
	Err    string      `json:"err"`    // 错误信息
}

// userAgents 现代主流浏览器 UA 池（Chrome / Firefox / Edge）
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36 Edg/122.0.0.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36 Edg/121.0.0.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:123.0) Gecko/20100101 Firefox/123.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:122.0) Gecko/20100101 Firefox/122.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 14.3; rv:123.0) Gecko/20100101 Firefox/123.0",
}

func randomUA() string {
	return userAgents[rand.Intn(len(userAgents))]
}

func setHTTPRequestHeaders(req *http.Request, customHeader http.Header) {
	req.Header.Set("User-Agent", randomUA())
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Cache-Control", "no-cache")

	if customHeader != nil {
		for k, v := range customHeader {
			if strings.EqualFold(k, "timeout") {
				continue
			}
			for _, val := range v {
				req.Header.Add(k, val)
			}
		}
	}
}

// CreateClient 初始化 Colly 客户端（保持兼容）
func CreateClient() *colly.Collector {
	c := colly.NewCollector()
	c.MaxDepth = 1
	c.AllowURLRevisit = true
	c.SetRequestTimeout(20 * time.Second)
	c.WithTransport(sharedTransport)
	return c
}

// decompressBody 自动检测并解压 Gzip / Deflate 压缩数据
func decompressBody(resp *http.Response) ([]byte, error) {
	var reader io.Reader = resp.Body

	encoding := strings.ToLower(resp.Header.Get("Content-Encoding"))
	if encoding == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	} else if encoding == "deflate" {
		flateReader := flate.NewReader(resp.Body)
		defer flateReader.Close()
		reader = flateReader
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	// 魔法头双重校验：若未声明 Content-Encoding 但开头为 Gzip 标志位 \x1f\x8b，则强制解压
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(body))
		if err == nil {
			decompressed, err := io.ReadAll(gz)
			gz.Close()
			if err == nil {
				return decompressed, nil
			}
		}
	}

	return body, nil
}

// ApiGet 高性能原生 HTTP GET 请求（自动解压 Gzip/Deflate）
func ApiGet(r *RequestInfo) {
	targetUrl := buildUrl(r.Uri, r.Params)
	req, err := http.NewRequest("GET", targetUrl, nil)
	if err != nil {
		r.Resp = nil
		r.Err = fmt.Sprintf("create request failed: %v, url=%s", err, targetUrl)
		return
	}

	setHTTPRequestHeaders(req, r.Header)

	client := httpClient
	if r.Header != nil {
		if t, _ := strconv.Atoi(r.Header.Get("timeout")); t > 0 {
			client = &http.Client{
				Transport: sharedTransport,
				Timeout:   time.Duration(t) * time.Second,
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		r.Resp = nil
		r.Err = fmt.Sprintf("%v, url=%s", err, targetUrl)
		return
	}
	defer resp.Body.Close()

	body, err := decompressBody(resp)
	if err != nil {
		r.Resp = nil
		r.Err = fmt.Sprintf("decompress/read response failed: %v, url=%s", err, targetUrl)
		return
	}

	if (resp.StatusCode == 200 || (resp.StatusCode >= 300 && resp.StatusCode <= 399)) && len(body) > 0 {
		r.Resp = body
		r.Err = ""
	} else {
		r.Resp = nil
		if resp.StatusCode == http.StatusTooManyRequests {
			r.Err = fmt.Sprintf("Too Many Requests, status=%d, url=%s", resp.StatusCode, targetUrl)
		} else {
			r.Err = fmt.Sprintf("unexpected response status=%d, url=%s", resp.StatusCode, targetUrl)
		}
	}
}

func IsRateLimitedErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Too Many Requests") || strings.Contains(err.Error(), "status=429")
}

// ApiTest 处理 API 测试请求
func ApiTest(r *RequestInfo) error {
	ApiGet(r)
	if r.Err != "" {
		log.Printf("ApiTest 访问失败: %s, Error: %s\n", buildUrl(r.Uri, r.Params), r.Err)
		return fmt.Errorf("%s", r.Err)
	}
	return nil
}

// buildUrl 安全拼接 URL 和 Query 参数
func buildUrl(base string, params url.Values) string {
	if len(params) == 0 {
		return base
	}
	u, err := url.Parse(base)
	if err != nil {
		if strings.Contains(base, "?") {
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
