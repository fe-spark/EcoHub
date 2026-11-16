package film

import (
	"server/internal/infra/db"
	"server/internal/model"
)

// countFilms 当前主站档案数。软删除不计入。
func countFilms(primary string) (int64, error) {
	var n int64
	err := db.Mdb.Model(&model.FilmIndex{}).Where("first_source_id = ?", primary).Count(&n).Error
	return n, err
}

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
