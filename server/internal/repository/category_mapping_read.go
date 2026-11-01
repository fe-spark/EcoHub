package repository

import (
	"errors"

	"server/internal/infra/db"
	"server/internal/model"
)

// ListCategoryMappings 读取全部分类映射。
func ListCategoryMappings() ([]model.CategoryMapping, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	var mappings []model.CategoryMapping
	err := db.Mdb.Find(&mappings).Error
	return mappings, err
}
