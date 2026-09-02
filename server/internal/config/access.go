package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	// AccessKeyPrefix 访问分析 Redis 前缀
	AccessKeyPrefix = RedisKeyPrefix + ":Access:"
	// DefaultTrustedProxies All-in-One / 本机与内网反向代理 CIDR
	DefaultTrustedProxies = "127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
)

var (
	AccessLogEnabled        = true
	AccessSlowMs      int64 = 500
	AccessRecentLimit       = 2000
	TrustedProxies          = []string{"127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	AccessIPSalt      []byte
)

func loadAccessRuntimeConfig() {
	AccessLogEnabled = parseEnvBool("ACCESS_LOG_ENABLED", true)
	AccessSlowMs = parseEnvInt64("ACCESS_SLOW_MS", 500)
	if AccessSlowMs < 1 {
		AccessSlowMs = 500
	}
	AccessRecentLimit = int(parseEnvInt64("ACCESS_RECENT_LIMIT", 2000))
	if AccessRecentLimit < 100 {
		AccessRecentLimit = 100
	}
	if AccessRecentLimit > 5000 {
		AccessRecentLimit = 5000
	}
	TrustedProxies = ParseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
	if salt := strings.TrimSpace(os.Getenv("ACCESS_IP_SALT")); salt != "" {
		AccessIPSalt = []byte(salt)
	} else {
		sum := sha256.Sum256([]byte("ecohub-access-ip:" + JwtSecret))
		AccessIPSalt = sum[:]
	}
	fmt.Printf("[Config] 访问分析 enabled=%v slowMs=%d recent=%d proxies=%s\n",
		AccessLogEnabled, AccessSlowMs, AccessRecentLimit, strings.Join(TrustedProxies, ","))
}

func parseEnvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseEnvInt64(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

// ParseTrustedProxies 解析逗号分隔的信任代理列表；空或全无效时回退默认。
func ParseTrustedProxies(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultTrustedProxies
	}
	out := make([]string, 0, 4)
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return ParseTrustedProxies(DefaultTrustedProxies)
	}
	return out
}
