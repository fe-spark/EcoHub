package db

import (
	"testing"

	"server/internal/config"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// MySQL 连不上时 InitMysql 只返回错误，不替换、不关闭已有连接池。
func TestInitMysql_KeepsExistingPoolOnFailure(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	prevDsn, prevHost, prevPort, prevMdb := config.MysqlDsn, config.MysqlHost, config.MysqlPort, Mdb
	config.MysqlDsn = "u:p@tcp(127.0.0.1:1)/x?timeout=1s"
	config.MysqlHost, config.MysqlPort = "127.0.0.1", "1"
	Mdb = gdb
	t.Cleanup(func() {
		config.MysqlDsn, config.MysqlHost, config.MysqlPort, Mdb = prevDsn, prevHost, prevPort, prevMdb
	})

	if err := InitMysql(); err == nil {
		t.Fatal("expected connect error")
	}
	if Mdb != gdb {
		t.Fatal("existing pool must not be replaced")
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("existing pool must stay open: %v", err)
	}
}
