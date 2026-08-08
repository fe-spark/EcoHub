package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/notify"
	"server/internal/router"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

func init() {
	setupLogging()
	if err := waitForRedis(30, 2*time.Second); err != nil {
		panic(err)
	}
	if err := waitForMySQL(30, 2*time.Second); err != nil {
		panic(err)
	}
}

func setupLogging() {
	if err := syslog.Init(); err != nil {
		// Init 失败时仍打到 stdout，避免完全静默
		log.SetOutput(os.Stdout)
		log.Printf("[Init] 系统日志初始化失败: %v", err)
		return
	}
	// 级别在写入时确定：默认 log/gin 输出为 INFO；gin 错误流为 ERROR。
	// syslog.Writer 内部已镜像 stdout + 落盘，勿再套 MultiWriter 以免双写。
	log.SetOutput(syslog.Writer())
	gin.DefaultWriter = syslog.Writer()
	gin.DefaultErrorWriter = syslog.LevelWriter(syslog.LevelError)
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
	log.Printf("[Init] EcoHub server version=%s", config.Version)

	db.StartRedisHealthCheck()
	db.StartMysqlHealthCheck()

	service.InitSvc.DefaultDataInit()
	// Telegram：/search 指令 + 更新列表翻页（需已配置 Bot Token）
	notify.EnsureBotPoller()

	r := router.SetupRouter()
	_ = r.Run(fmt.Sprintf(":%s", config.ListenerPort))
}
