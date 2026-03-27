package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// EnsureDatabase 在管理连接上保证数据库存在
func EnsureDatabase(conn *sql.DB, dbName string) error {
	log.Printf("[Init] 正在确保数据库 `%s` 存在...\n", dbName)
	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName)
	_, err := conn.Exec(query)
	return err
}

// PhysicalRebuild 执行最彻底的物理删库重建
// 需要具有 DROP DATABASE 和 CREATE DATABASE 权限
func PhysicalRebuild(conn *sql.DB, dbName string) error {
	log.Printf("[Rebuild] 正在物理删除数据库 `%s`...\n", dbName)
	if _, err := conn.Exec(fmt.Sprintf("DROP DATABASE `%s`;", dbName)); err != nil {
		return err
	}

	log.Printf("[Rebuild] 正在重新创建数据库 `%s`...\n", dbName)
	query := fmt.Sprintf("CREATE DATABASE `%s` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName)
	if _, err := conn.Exec(query); err != nil {
		return err
	}

	// 给文件系统一点反应时间
	time.Sleep(500 * time.Millisecond)
	return nil
}

// LogicalWipe 获取所有表并 TRUNCATE，模拟删号重来的效果
// 在没有删库权限时的降级方案
func LogicalWipe(conn *sql.DB) error {
	rows, err := conn.Query("SHOW TABLES")
	if err != nil {
		return err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err == nil {
			tables = append(tables, table)
		}
	}

	if len(tables) == 0 {
		return nil
	}

	// 禁用外键检查并清空
	conn.Exec("SET FOREIGN_KEY_CHECKS = 0;")
	for _, table := range tables {
		if _, err := conn.Exec(fmt.Sprintf("TRUNCATE TABLE `%s`;", table)); err != nil {
			return err
		}
	}
	conn.Exec("SET FOREIGN_KEY_CHECKS = 1;")
	log.Printf("[Reset] 数据库所有表已通过逻辑清空复位\n")
	return nil
}
