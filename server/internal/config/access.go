package config

import (
	"crypto/sha256"
)

const (
	// AccessKeyPrefix 访问分析 Redis 前缀
	AccessKeyPrefix = RedisKeyPrefix + ":Access:"
)

var (
	// AccessRecentLimit 最近访问流水条数上限，默认 100
	AccessRecentLimit = 100
	TrustedProxies    = []string{"127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	AccessIPSalt      []byte
)

func loadAccessRuntimeConfig() {
	// IP 脱敏 Salt 自动基于 JwtSecret 派生，无需手动配置
	sum := sha256.Sum256([]byte("ecohub-access-ip:" + JwtSecret))
	AccessIPSalt = sum[:]
}
