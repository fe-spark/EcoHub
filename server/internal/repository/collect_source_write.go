package repository

import (
	"errors"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// DeleteFailureRecordsByOrigin 删除一个采集站的失败记录。
func DeleteFailureRecordsByOrigin(originID string) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return DeleteFailureRecordsByOriginIdTx(db.Mdb, originID)
}

// ClearSourceCategoryData 清空一个采集站的分类副本。
func ClearSourceCategoryData(sourceID string) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return ClearSourceCategoryDataTx(db.Mdb, sourceID)
}

// EnsureDefaultPosterSource 保证存在海报源站。
func EnsureDefaultPosterSource() error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return EnsureDefaultPosterSourceTx(db.Mdb)
}

// UpdateCollectSource 更新采集站。
func UpdateCollectSource(s model.FilmSource) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		return UpdateCollectSourceTx(tx, s)
	})
}
