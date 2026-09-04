package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"server/internal/access"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/notify"
	"server/internal/router"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

func init() {
	if config.IsUpgradeHelper() {
		return
	}
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
	if config.IsUpgradeHelper() {
		if err := service.RunUpgradeHelper(os.Args[1:]); err != nil {
			log.SetOutput(os.Stderr)
			log.Fatal(err)
		}
		return
	}
	start()
}

func start() {
	log.Printf("[Init] EcoHub server version=%s", config.Version)

	db.StartRedisHealthCheck()
	db.StartMysqlHealthCheck()

	service.InitSvc.DefaultDataInit()
	// Telegram：/search 指令 + 更新列表翻页（需已配置 Bot Token；Worker 纯读节点由 EnsureBotPoller 内部跳过）
	notify.EnsureBotPoller()
	access.StartCollector()

	r := router.SetupRouter()
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", config.ListenerPort),
		Handler: r,
	}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Fatalf("[Init] HTTP 监听失败: %v", err)
	}
	log.Printf("[Init] EcoHub HTTP server listening on :%s", config.ListenerPort)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Shutdown] HTTP 服务异常退出: %v", err)
		}
	}()

	// 优雅停机：停止接收新请求 → 排空接口审计队列并最终刷盘
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)
	<-stopCh
	log.Printf("[Shutdown] 收到退出信号，开始优雅停机")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[Shutdown] HTTP 优雅停机超时: %v", err)
	}

	access.StopApiLogWorker()
	select {
	case <-access.ApiLogWorkerDone():
		log.Printf("[Shutdown] 接口访问日志已排空落盘")
	case <-time.After(3 * time.Second):
		log.Printf("[Shutdown] 等待接口日志落盘超时，强制退出")
	}
	log.Printf("[Shutdown] 退出完成")
}
