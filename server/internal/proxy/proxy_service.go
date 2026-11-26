package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"server/internal/repobase"
	"strings"
	"time"

	"server/internal/admin/upgrade"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/utils"
)

type ProxyService struct{}

var ProxySvc = new(ProxyService)

const defaultProxyTestTarget = "https://cloudflare.com/cdn-cgi/trace"

func (s *ProxyService) GetConfig() (model.ProxyConfig, error) {
	return repobase.GetProxyConfig()
}

// PublicConfig 去掉代理地址里的账号密码，并标明是否已配置认证凭据，供管理端读取。
func (s *ProxyService) PublicConfig() (model.PublicProxyConfig, error) {
	cfg, err := s.GetConfig()
	if err != nil {
		return model.PublicProxyConfig{}, err
	}
	hasAuth := proxyURLHasUser(cfg.ProxyURL)
	cfg.ProxyURL = RedactProxyURL(cfg.ProxyURL)
	return model.PublicProxyConfig{
		ProxyConfig: cfg,
		HasAuth:     hasAuth,
	}, nil
}

func resolveEffectiveProxyURL(raw string, preserveAuth bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if !preserveAuth || raw == "" || proxyURLHasUser(raw) {
		return raw, nil
	}
	stored, err := repobase.GetProxyConfig()
	if err != nil {
		return "", err
	}
	if RedactProxyURL(raw) == RedactProxyURL(stored.ProxyURL) {
		return stored.ProxyURL, nil
	}
	return raw, nil
}

func normalizeAndValidateProxyURL(raw string) (string, error) {
	proxyStr := strings.TrimSpace(raw)
	if proxyStr == "" {
		return "", errors.New("代理服务器地址不能为空")
	}
	if !strings.Contains(proxyStr, "://") {
		proxyStr = "http://" + proxyStr
	}
	u, err := url.Parse(proxyStr)
	if err != nil || u.Host == "" {
		return "", errors.New("代理地址格式无效，请输入正确的 http://、https://、socks5:// 或 socks5h:// 地址")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
		u.Scheme = strings.ToLower(u.Scheme)
		return u.String(), nil
	default:
		return "", errors.New("代理地址格式无效，请输入正确的 http://、https://、socks5:// 或 socks5h:// 地址")
	}
}

func ValidateAndNormalizeProxyConfig(cfg model.ProxyConfig) (model.ProxyConfig, error) {
	cfg = repobase.NormalizeProxyConfig(cfg)
	if cfg.Enabled && cfg.ProxyURL == "" {
		return cfg, errors.New("启用代理时代理服务器地址不能为空")
	}
	if cfg.ProxyURL != "" {
		normalized, err := normalizeAndValidateProxyURL(cfg.ProxyURL)
		if err != nil {
			return cfg, err
		}
		cfg.ProxyURL = normalized
	}
	return cfg, nil
}

func (s *ProxyService) UpdateConfig(cfg model.ProxyConfig, preserveAuth bool) error {
	resolved, err := resolveEffectiveProxyURL(cfg.ProxyURL, preserveAuth)
	if err != nil {
		return err
	}
	cfg.ProxyURL = resolved
	cfg, err = ValidateAndNormalizeProxyConfig(cfg)
	if err != nil {
		return err
	}
	if err := repobase.SaveProxyConfig(cfg); err != nil {
		return err
	}
	utils.ResetProxyTransports()
	notify.NotifySvc.EnsureBotPoller()
	upgrade.TriggerReleasePoll()
	return nil
}

// TestProxy 测试代理连通性（固定请求受控公网端点，杜绝 SSRF）
func (s *ProxyService) TestProxy(proxyURL string, preserveAuth bool) (int64, error) {
	resolved, err := resolveEffectiveProxyURL(proxyURL, preserveAuth)
	if err != nil {
		return 0, err
	}
	if resolved == "" {
		return 0, errors.New("代理地址不能为空")
	}
	normalized, err := normalizeAndValidateProxyURL(resolved)
	if err != nil {
		return 0, err
	}
	proxyURL = normalized

	target := defaultProxyTestTarget

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

func RedactProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	proxyStr := raw
	if !strings.Contains(proxyStr, "://") {
		proxyStr = "http://" + proxyStr
	}
	u, err := url.Parse(proxyStr)
	if err != nil || u.Host == "" {
		return raw
	}
	u.User = nil
	return u.String()
}

func proxyURLHasUser(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	return err == nil && u.User != nil
}
