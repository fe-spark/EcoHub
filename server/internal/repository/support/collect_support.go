package support

import (
	"fmt"
	"log"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

func GetCollectSourceList() []model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var list []model.FilmSource
	if err := db.Mdb.Order("grade ASC").Find(&list).Error; err != nil {
		log.Println("GetCollectSourceList Error:", err)
		return nil
	}
	return list
}

// TruncateTable 统一跨数据库（MySQL TRUNCATE 与 SQLite DELETE）表截断与清空方言
func TruncateTable(conn *gorm.DB, table string) error {
	if conn == nil {
		return nil
	}
	if conn.Dialector != nil && conn.Dialector.Name() == "sqlite" {
		if !conn.Migrator().HasTable(table) {
			return nil
		}
		if err := conn.Exec(fmt.Sprintf("DELETE FROM %s", table)).Error; err != nil {
			return fmt.Errorf("truncate (sqlite delete) %s failed: %w", table, err)
		}
		return nil
	}
	if err := conn.Exec(fmt.Sprintf("TRUNCATE table %s", table)).Error; err != nil {
		return fmt.Errorf("truncate %s failed: %w", table, err)
	}
	return nil
}

func TruncateRecordTable() {
	if err := TruncateTable(db.Mdb, model.TableFailureRecord); err != nil {
		log.Println("TRUNCATE TABLE Error: ", err)
	}
}
