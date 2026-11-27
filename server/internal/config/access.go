package config

import (
	"crypto/sha256"
	"net"
	"net/netip"
	"os"
	"strings"
)

const (
	// AccessKeyPrefix 访问分析 Redis 前缀
	AccessKeyPrefix = RedisKeyPrefix + ":Access:"
)

var (
	// AccessRecentLimit 最近访问流水条数上限，默认 100
	AccessRecentLimit = 100
	// TrustedProxies 默认仅信任本地环回代理；生产部署通过 ECOHUB_TRUSTED_PROXIES 显式配置受信任反代 CIDR/IP。
	TrustedProxies = []string{"127.0.0.1", "::1"}
	AccessIPSalt   []byte
)

func loadAccessRuntimeConfig() {
	// IP 脱敏 Salt 自动基于 JwtSecret 派生，无需手动配置
	sum := sha256.Sum256([]byte("ecohub-access-ip:" + JwtSecret))
	AccessIPSalt = sum[:]
	LoadTrustedProxiesFromEnv()
}

// LoadTrustedProxiesFromEnv 从 ECOHUB_TRUSTED_PROXIES 加载显式配置的可信代理网段或 IP。
func LoadTrustedProxiesFromEnv() {
	raw := strings.TrimSpace(os.Getenv("ECOHUB_TRUSTED_PROXIES"))
	if raw == "" {
		return
	}
	var parsed []string
	for _, item := range strings.Split(raw, ",") {
		entry := strings.TrimSpace(item)
		if entry == "" {
			continue
		}
		if _, err := netip.ParsePrefix(entry); err == nil {
			parsed = append(parsed, entry)
			continue
		}
		if _, err := netip.ParseAddr(entry); err == nil {
			parsed = append(parsed, entry)
		}
	}
	if len(parsed) > 0 {
		TrustedProxies = parsed
	}
}

// IsTrustedProxyPeer 仅当直连对端为空（单测场景）或精确命中 TrustedProxies 时才采信 X-Forwarded-* 请求头。
func IsTrustedProxyPeer(remoteAddr string) bool {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return true
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, entry := range TrustedProxies {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if prefix, pErr := netip.ParsePrefix(entry); pErr == nil {
			if prefix.Contains(addr) {
				return true
			}
			continue
		}
		if target, aErr := netip.ParseAddr(entry); aErr == nil && target.Unmap() == addr {
			return true
		}
	}
	return false
}
