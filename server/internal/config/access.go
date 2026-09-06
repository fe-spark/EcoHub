package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

const (
	// AccessKeyPrefix 访问分析 Redis 前缀
	AccessKeyPrefix = RedisKeyPrefix + ":Access:"
	// DefaultTrustedProxies All-in-One / 本机与内网反向代理 CIDR
	DefaultTrustedProxies = "127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
)

var (
	// AccessLogEnabled 唯一对外暴露的数据分析开关，默认关闭 (false)
	AccessLogEnabled = false
	// AccessRecentLimit 最近访问流水条数上限，默认 100
	AccessRecentLimit = 100
	TrustedProxies    = []string{"127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	AccessIPSalt      []byte
)

func loadAccessRuntimeConfig() {
	// 唯一对外暴露的环境变量：默认不开启数据分析
	AccessLogEnabled = parseEnvBool("ACCESS_ANALYTICS_ENABLED", false)

	// IP 脱敏 Salt 自动基于 JwtSecret 派生，无需手动配置
	sum := sha256.Sum256([]byte("ecohub-access-ip:" + JwtSecret))
	AccessIPSalt = sum[:]

	fmt.Printf("[Config] 数据分析 enabled=%v\n", AccessLogEnabled)
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
