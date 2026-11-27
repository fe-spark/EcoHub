package film

import (
	"context"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type sourceCount struct {
	SourceID string `gorm:"column:source_id"`
	N        int64  `gorm:"column:n"`
}

// openInventoryReadDB 在 MySQL 环境下使用长读超时独立连接，避免采集期大表 COUNT(*) 撞 30 秒 readTimeout。
func openInventoryReadDB(fallback *gorm.DB) (*gorm.DB, func()) {
	if fallback == nil || fallback.Dialector == nil || fallback.Dialector.Name() != "mysql" || strings.TrimSpace(config.MysqlDsn) == "" {
		return fallback, func() {}
	}
	sqlDB, err := db.OpenLongReadConn(config.MysqlDsn)
	if err != nil {
		return fallback, func() {}
	}
	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		_ = sqlDB.Close()
		return fallback, func() {}
	}
	return gdb, func() { _ = sqlDB.Close() }
}

// countFilmsBySource 按采集站直查未删除的真实已采集影片数（走 first_source_id, deleted_at 覆盖索引）。
func countFilmsBySource(ctx context.Context, connDB *gorm.DB, sourceIDs []string) (map[string]int64, error) {
	if len(sourceIDs) == 0 {
		return map[string]int64{}, nil
	}
	readDB, cleanup := openInventoryReadDB(connDB)
	defer cleanup()

	var rows []sourceCount
	err := readDB.WithContext(ctx).Model(&model.FilmIndex{}).
		Select("first_source_id AS source_id, COUNT(*) AS n").
		Where("first_source_id IN ?", sourceIDs).
		Group("first_source_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return indexSourceCounts(rows), nil
}

// countCategoriesBySource 各采集站已映射、且展示分类还在的分类数。
func countCategoriesBySource(ctx context.Context, connDB *gorm.DB) (map[string]int64, error) {
	var rows []sourceCount
	err := connDB.WithContext(ctx).Model(&model.CategoryMapping{}).
		Select("source_id, COUNT(DISTINCT category_id) AS n").
		Where("category_id > 0").
		Where("category_id IN (?)", connDB.Model(&model.Category{}).Select("id")).
		Group("source_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return indexSourceCounts(rows), nil
}

func indexSourceCounts(rows []sourceCount) map[string]int64 {
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(row.SourceID)
		if id == "" {
			continue
		}
		out[id] = row.N
	}
	return out
}
