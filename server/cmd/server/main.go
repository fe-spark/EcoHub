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
	"server/internal/spider"

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
	access.StartCollector()
	notify.EnsureBotPoller()

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

	// 退出信号
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)
	<-stopCh
	log.Printf("[Shutdown] 收到退出信号，开始退出")

	// 1. 关闭 HTTP 服务接收通道，确保在途请求能安全读取现有快照（严禁清理快照状态，保护滚动更新与在途请求）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[Shutdown] HTTP 优雅停机超时: %v", err)
	}

	// 2. 快速停止采集任务（非阻塞通知）
	spider.StopAllTasks()

	// 3. 等待在途采集写调度队列优雅排空落盘
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer writeCancel()
	if err := spider.WaitPendingWrites(writeCtx); err != nil {
		log.Printf("[Shutdown] 等待采集写队列排空超时或失败: %v", err)
	}

	// 4. 停止 Telegram Bot 轮询
	notify.StopBotPoller()

	// 5. 等待在途异步通知发送协程排空
	notifyCtx, notifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer notifyCancel()
	if err := notify.WaitPendingPublishes(notifyCtx); err != nil {
		log.Printf("[Shutdown] 等待在途通知排空超时或失败: %v", err)
	}

	log.Printf("[Shutdown] 退出完成")
}
