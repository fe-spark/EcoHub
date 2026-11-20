package film

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
)

// countPrimaryCategories 当前主站已映射、且展示分类还在的分类数。
func countPrimaryCategories(primary string) (int64, error) {
	var n int64
	err := db.Mdb.Model(&model.CategoryMapping{}).
		Where("source_id = ? AND category_id > 0", primary).
		Where("category_id IN (?)", db.Mdb.Model(&model.Category{}).Select("id")).
		Distinct("category_id").
		Count(&n).Error
	return n, err
}

type sourceCount struct {
	SourceID string `gorm:"column:source_id"`
	N        int64  `gorm:"column:n"`
}

// countCategoriesBySource 各采集站已映射、且展示分类还在的分类数。
func countCategoriesBySource() (map[string]int64, error) {
	var rows []sourceCount
	err := db.Mdb.Model(&model.CategoryMapping{}).
		Select("source_id, COUNT(DISTINCT category_id) AS n").
		Where("category_id > 0").
		Where("category_id IN (?)", db.Mdb.Model(&model.Category{}).Select("id")).
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
