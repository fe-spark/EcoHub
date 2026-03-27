package db

import (
	"context"
	"fmt"
	"server/internal/config"
	"time"

	"github.com/redis/go-redis/v9"
)

/*
redis 工具类
*/
var Rdb *redis.Client
var Cxt = context.Background()

// InitRedisConn 初始化redis客户端
func InitRedisConn() error {

	Rdb = redis.NewClient(&redis.Options{
		Addr:        config.RedisAddr,
		Password:    config.RedisPassword,
		DB:          config.RedisDBNo,
		PoolSize:    10,               // 最大连接数
		DialTimeout: time.Second * 10, // 超时时间
	})
	// 测试连接是否正常
	_, err := Rdb.Ping(Cxt).Result()
	if err != nil {
		panic(err)
	}

	// 开发模式下清空 Redis
	if config.IsDevMode {
		Rdb.FlushAll(Cxt)
		fmt.Println("[Init] 开发模式：Redis 缓存已清空")
	}

	return nil
}

// CloseRedis 关闭redis连接
func CloseRedis() error {
	if Rdb != nil {
		return Rdb.Close()
	}
	return nil
}
