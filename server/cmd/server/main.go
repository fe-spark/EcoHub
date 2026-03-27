package main

import (
	"fmt"
	"log"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/router"
	"server/internal/service"
)

func init() {
	if err := waitForRedis(30, 2*time.Second); err != nil {
		panic(err)
	}
	if err := waitForMySQL(30, 2*time.Second); err != nil {
		panic(err)
	}
}

func waitForRedis(maxRetries int, interval time.Duration) error {
	var err error
	for i := 1; i <= maxRetries; i++ {
		err = db.InitRedisConn()
		if err == nil {
			log.Printf("[Init] Redis 连接成功 (第 %d 次尝试)", i)
			return nil
		}
		log.Printf("[Init] Redis 连接失败 (%d/%d): %v", i, maxRetries, err)
		time.Sleep(interval)
	}
	return fmt.Errorf("Redis 连接失败，已重试 %d 次: %w", maxRetries, err)
}

func waitForMySQL(maxRetries int, interval time.Duration) error {
	var err error
	for i := 1; i <= maxRetries; i++ {
		err = db.InitMysql()
		if err == nil {
			log.Printf("[Init] MySQL 连接成功 (第 %d 次尝试)", i)
			return nil
		}
		log.Printf("[Init] MySQL 连接失败 (%d/%d): %v", i, maxRetries, err)
		time.Sleep(interval)
	}
	return fmt.Errorf("MySQL 连接失败，已重试 %d 次: %w", maxRetries, err)
}

func main() {
	start()
}

func start() {
	service.InitSvc.DefaultDataInit()

	r := router.SetupRouter()
	_ = r.Run(fmt.Sprintf(":%s", config.ListenerPort))
}
