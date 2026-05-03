package db

import (
	"context"
	"log"
	"server/internal/config"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

/*
redis 工具类
*/
var Rdb *redis.Client
var Cxt = context.Background()

var redisMu sync.Mutex

// InitRedisConn 初始化redis客户端
func InitRedisConn() error {
	redisMu.Lock()
	defer redisMu.Unlock()

	client := redis.NewClient(&redis.Options{
		Addr:            config.RedisAddr,
		Password:        config.RedisPassword,
		DB:              config.RedisDBNo,
		PoolSize:        10,
		MinIdleConns:    2,
		MaxRetries:      3,
		DialTimeout:     time.Second * 10,
		ReadTimeout:     time.Second * 5,
		WriteTimeout:    time.Second * 5,
		PoolTimeout:     time.Second * 10,
		ConnMaxIdleTime: time.Minute * 5,
		ConnMaxLifetime: time.Hour,
	})
	// 测试连接是否正常
	_, err := client.Ping(Cxt).Result()
	if err != nil {
		_ = client.Close()
		return err
	}

	old := Rdb
	Rdb = client
	if old != nil {
		_ = old.Close()
	}

	return nil
}

// CloseRedis 关闭redis连接
func CloseRedis() error {
	redisMu.Lock()
	defer redisMu.Unlock()
	if Rdb != nil {
		return Rdb.Close()
	}
	return nil
}

func StartRedisHealthCheck() {
	go func() {
		ticker := time.NewTicker(time.Second * 15)
		defer ticker.Stop()
		for range ticker.C {
			redisMu.Lock()
			client := Rdb
			redisMu.Unlock()

			if client == nil {
				if err := InitRedisConn(); err != nil {
					log.Printf("[Redis] 重建连接失败: %v", err)
				}
				continue
			}
			if err := client.Ping(Cxt).Err(); err != nil {
				log.Printf("[Redis] 健康检查失败，尝试重建连接: %v", err)
				if rebuildErr := InitRedisConn(); rebuildErr != nil {
					log.Printf("[Redis] 重建连接失败: %v", rebuildErr)
				}
			}
		}
	}()
}
