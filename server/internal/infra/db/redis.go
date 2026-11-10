package db

import (
	"context"
	"github.com/redis/go-redis/v9"
	"log"
	"server/internal/config"
	"sync"
	"time"
)

/*
redis 工具类
*/

// Rdb Redis 客户端。只在启动时由 InitRedisConn 赋值一次，之后不再替换：
// 业务协程无锁读取它，运行期替换会产生数据竞争。go-redis 连接池自带重连，不需要换客户端。
var Rdb *redis.Client
var Cxt = context.Background()

var redisMu sync.Mutex

// InitRedisConn 初始化 redis 客户端。必须在启动其他协程之前调用（见 cmd/server）。
// 已经建好客户端时只探活，不会替换 Rdb。
func InitRedisConn() error {
	redisMu.Lock()
	defer redisMu.Unlock()

	if Rdb != nil {
		return Rdb.Ping(Cxt).Err()
	}

	client := redis.NewClient(&redis.Options{
		Addr:            config.RedisAddr,
		Password:        config.RedisPassword,
		DB:              config.RedisDBNo,
		PoolSize:        64,
		MinIdleConns:    10,
		MaxRetries:      -1, // 0 会被 go-redis 当成默认 3 次，INCR 会整段重放
		DialTimeout:     time.Second * 10,
		ReadTimeout:     time.Second * 5,
		WriteTimeout:    time.Second * 5,
		PoolTimeout:     time.Second * 10,
		ConnMaxIdleTime: time.Minute * 5,
		ConnMaxLifetime: time.Hour,
	})
	// 测试连接是否正常
	if err := client.Ping(Cxt).Err(); err != nil {
		_ = client.Close()
		return err
	}
	Rdb = client
	return nil
}

// StartRedisHealthCheck 每 15 秒探活一次。失败只记录日志，由 go-redis 连接池自动重连，不替换 Rdb。
func StartRedisHealthCheck() {
	redisMu.Lock()
	client := Rdb
	redisMu.Unlock()
	if client == nil {
		log.Printf("[Redis] 客户端未初始化，跳过健康检查")
		return
	}
	go func() {
		ticker := time.NewTicker(time.Second * 15)
		defer ticker.Stop()
		healthy := true
		for range ticker.C {
			if err := client.Ping(Cxt).Err(); err != nil {
				log.Printf("[Redis] 健康检查失败，等待连接池自动重连: %v", err)
				healthy = false
				continue
			}
			if !healthy {
				log.Printf("[Redis] 连接已恢复")
				healthy = true
			}
		}
	}()
}
