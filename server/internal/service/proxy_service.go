package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/utils"
)

type ProxyService struct{}

var ProxySvc = new(ProxyService)

const defaultProxyTestTarget = "https://cloudflare.com/cdn-cgi/trace"

// GetConfig 获取当前代理配置
func (s *ProxyService) GetConfig() model.ProxyConfig {
	return repository.GetProxyConfig()
}

// UpdateConfig 更新代理配置
func (s *ProxyService) UpdateConfig(cfg model.ProxyConfig) error {
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	if cfg.Enabled {
		if cfg.ProxyURL == "" {
			return errors.New("启用代理时代理服务器地址不能为空")
		}
		proxyStr := cfg.ProxyURL
		if !strings.HasPrefix(proxyStr, "http://") &&
			!strings.HasPrefix(proxyStr, "https://") &&
			!strings.HasPrefix(proxyStr, "socks5://") {
			proxyStr = "http://" + proxyStr
		}
		u, err := url.Parse(proxyStr)
		if err != nil || u.Host == "" {
			return errors.New("代理地址格式无效，请输入正确的 http://、https:// 或 socks5:// 地址")
		}
	}
	return repository.SaveProxyConfig(cfg)
}

// TestProxy 测试代理连通性
func (s *ProxyService) TestProxy(proxyURL, target string) (int64, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return 0, errors.New("代理地址不能为空")
	}
	if !strings.HasPrefix(proxyURL, "http://") &&
		!strings.HasPrefix(proxyURL, "https://") &&
		!strings.HasPrefix(proxyURL, "socks5://") {
		proxyURL = "http://" + proxyURL
	}

	target = strings.TrimSpace(target)
	if target == "" {
		target = defaultProxyTestTarget
	}

	transport := utils.GetOrCreateProxyTransport(proxyURL)
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "EcoHub-Proxy-Checker/1.0")

	resp, err := client.Do(req)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		return duration, fmt.Errorf("代理连通失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return duration, fmt.Errorf("目标返回异常状态码: %d", resp.StatusCode)
	}

	return duration, nil
}

// ResolveSourceProxy 查询指定采集站是否启用代理
func (s *ProxyService) ResolveSourceProxy(sourceID string) (bool, string) {
	return repository.ResolveSourceProxy(sourceID)
}
