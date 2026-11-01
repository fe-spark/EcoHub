package repository

import (
	"errors"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SaveFilmScrape 在同一事务里写入影片正文和 TMDB 词条。
func SaveFilmScrape(mid int64, updates map[string]any, scrape model.FilmTMDBScrape) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.FilmIndex{}).Where("mid = ?", mid).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&scrape).Error
	})
}
