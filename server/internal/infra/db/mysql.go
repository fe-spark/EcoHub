package db

import (
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

var Mdb *gorm.DB

var mysqlMu sync.Mutex

func InitMysql() (err error) {
	mysqlMu.Lock()
	defer mysqlMu.Unlock()

	userDsn := config.MysqlDsn
	manageDsn := config.GetRootMysqlDsn()

	userConn, userErr := openSQLConn(userDsn)
	manageConn, manageErr := openSQLConn(manageDsn)

	if userConn != nil {
		defer userConn.Close()
	}
	if manageConn != nil {
		defer manageConn.Close()
	}

	if userErr != nil {
		if manageErr != nil {
			return userErr
		}
		if err = EnsureDatabase(manageConn, config.MysqlDBName); err != nil {
			return err
		}
	}

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

	// 前台读链路已切快照表，连接池保持保守上限，避免采集期把 MySQL 连接打满。
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetConnMaxLifetime(time.Minute * 30)
	sqlDB.SetConnMaxIdleTime(time.Minute * 5)
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return err
	}

	old := Mdb
	Mdb = newDB
	if old != nil {
		if oldSQL, closeErr := old.DB(); closeErr == nil {
			_ = oldSQL.Close()
		}
	}

	return nil
}

func StartMysqlHealthCheck() {
	go func() {
		ticker := time.NewTicker(time.Second * 15)
		defer ticker.Stop()
		for range ticker.C {
			mysqlMu.Lock()
			current := Mdb
			mysqlMu.Unlock()

			if current == nil {
				if err := InitMysql(); err != nil {
					log.Printf("[MySQL] 重建连接失败: %v", err)
				}
				continue
			}
			sqlDB, err := current.DB()
			if err != nil || sqlDB.Ping() != nil {
				if err != nil {
					log.Printf("[MySQL] 获取连接池失败，尝试重建连接: %v", err)
				} else {
					log.Printf("[MySQL] 健康检查失败，尝试重建连接")
				}
				if rebuildErr := InitMysql(); rebuildErr != nil {
					log.Printf("[MySQL] 重建连接失败: %v", rebuildErr)
				}
			}
		}
	}()
}

func openSQLConn(dsn string) (*sql.DB, error) {
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err = conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
