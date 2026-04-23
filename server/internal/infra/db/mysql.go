package db

import (
	"database/sql"
	"server/internal/config"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

var Mdb *gorm.DB

func InitMysql() (err error) {
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
	Mdb, err = gorm.Open(mysql.New(mysql.Config{
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

	sqlDB, err := Mdb.DB()
	if err != nil {
		return err
	}

	// 设置连接池
	sqlDB.SetMaxIdleConns(10)                  // 最大空闲连接数
	sqlDB.SetMaxOpenConns(50)                  // 最大打开连接数
	sqlDB.SetConnMaxLifetime(time.Hour)        // 连接最大复用时间
	sqlDB.SetConnMaxIdleTime(time.Minute * 10) // 空闲连接最大存活时间

	return nil
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
