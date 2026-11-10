package db

import (
	"context"
	"database/sql"
	"log"
	"server/internal/config"
	"sync"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// Mdb 业务连接池。只在启动时由 InitMysql 赋值一次，之后不再替换：
// 业务协程无锁读取它，运行期替换会产生数据竞争，并会打断还在用旧池的查询。
// database/sql 连接池自带重连：坏连接会被丢弃，下次取用时重新拨号，不需要换池。
var Mdb *gorm.DB

var mysqlMu sync.Mutex

// InitMysql 确保数据库存在并建立业务连接池。必须在启动其他协程之前调用（见 cmd/server）。
// 已经建好连接池时只做数据库存在性检查，不会替换 Mdb。
func InitMysql() (err error) {
	mysqlMu.Lock()
	defer mysqlMu.Unlock()

	if err := ensureMysqlDatabase(); err != nil {
		return err
	}
	if Mdb != nil {
		return nil
	}

	userDsn := config.MysqlDsn
	// 统一在数据库生命周期处理完成后，再建立业务连接池
	newDB, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       userDsn,
		DefaultStringSize:         255,   //string类型字段默认长度
		DisableDatetimePrecision:  true,  // 禁用 datetime 精度
		DontSupportRenameIndex:    true,  // 重命名索引时采用删除并新建的方式
		DontSupportRenameColumn:   true,  // 用change 重命名列
		SkipInitializeWithVersion: false, // 根据当前Mysql版本自动配置
	}), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true, //是否使用 结构体名称作为表名 (关闭自动变复数)
		},
		Logger: logger.Default.LogMode(logger.Silent), //完全关闭 GORM SQL 日志输出
	})

	if err != nil {
		return err
	}

	sqlDB, err := newDB.DB()
	if err != nil {
		return err
	}

	// 采集写库上限 16 个写事务，再留前台查询与管理接口的连接余量。
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(48)
	sqlDB.SetConnMaxLifetime(time.Minute * 30)
	sqlDB.SetConnMaxIdleTime(time.Minute * 5)
	if err := pingSQL(sqlDB); err != nil {
		_ = sqlDB.Close()
		return err
	}

	Mdb = newDB
	return nil
}

// ensureMysqlDatabase 业务账号连不上时，用管理账号确保数据库存在（库被删掉后可自动重建）。
func ensureMysqlDatabase() error {
	userConn, userErr := openSQLConn(config.MysqlDsn)
	if userConn != nil {
		defer userConn.Close()
	}
	if userErr == nil {
		return nil
	}
	manageConn, manageErr := openSQLConn(config.GetRootMysqlDsn())
	if manageConn != nil {
		defer manageConn.Close()
	}
	if manageErr != nil {
		return userErr
	}
	return EnsureDatabase(manageConn, config.MysqlDBName)
}

// StartMysqlHealthCheck 每 15 秒探活一次。失败只记录日志并尝试确保数据库存在，不替换连接池：
// 连接池会自己丢弃坏连接、重新拨号；替换全局 Mdb 会和业务协程的读取竞争，立刻关闭旧池还会打断进行中的查询。
func StartMysqlHealthCheck() {
	mysqlMu.Lock()
	current := Mdb
	mysqlMu.Unlock()
	if current == nil {
		log.Printf("[MySQL] 连接池未初始化，跳过健康检查")
		return
	}
	go func() {
		ticker := time.NewTicker(time.Second * 15)
		defer ticker.Stop()
		healthy := true
		for range ticker.C {
			if err := pingGormDB(current); err != nil {
				log.Printf("[MySQL] 健康检查失败，等待连接池自动重连: %v", err)
				healthy = false
				mysqlMu.Lock()
				if ensureErr := ensureMysqlDatabase(); ensureErr != nil {
					log.Printf("[MySQL] 检查数据库失败: %v", ensureErr)
				}
				mysqlMu.Unlock()
				continue
			}
			if !healthy {
				log.Printf("[MySQL] 连接已恢复")
				healthy = true
			}
		}
	}()
}

// mysqlPingTimeout 单次 Ping 的上限。库挂死时不让启动或健康检查一直卡住。
const mysqlPingTimeout = 10 * time.Second

func pingSQL(conn *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), mysqlPingTimeout)
	defer cancel()
	return conn.PingContext(ctx)
}

func pingGormDB(g *gorm.DB) error {
	sqlDB, err := g.DB()
	if err != nil {
		return err
	}
	return pingSQL(sqlDB)
}

func openSQLConn(dsn string) (*sql.DB, error) {
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err = pingSQL(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
