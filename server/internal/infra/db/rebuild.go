package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/go-sql-driver/mysql"
)

// EnsureDatabase 在管理连接上保证数据库存在
func EnsureDatabase(conn *sql.DB, dbName string) error {
	log.Printf("[Init] 正在确保数据库 `%s` 存在...\n", dbName)
	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName)
	_, err := conn.Exec(query)
	return err
}
