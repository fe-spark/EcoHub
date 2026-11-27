package tmdb

import (
	"errors"
	"fmt"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SaveFilmScrape 在 FOR UPDATE 行锁保护下基于最新档案计算并写入影片正文及 TMDB 词条，防止并发丢锁或制造孤儿词条。
func SaveFilmScrape(mid int64, buildUpdates func(existing model.FilmIndex) map[string]any, scrape model.FilmTMDBScrape) (int64, error) {
	if db.Mdb == nil {
		return 0, errors.New("数据库未连接")
	}
	var lockedPid int64
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		var existing model.FilmIndex
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("mid = ?", mid).
			First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("未找到对应的影片信息 (mid=%d): %w", mid, err)
			}
			return err
		}
		lockedPid = existing.Pid
		updates := buildUpdates(existing)
		res := tx.Model(&model.FilmIndex{}).Where("mid = ?", mid).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("影片不存在或已被删除 (mid=%d)", mid)
		}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&scrape).Error
	})
	return lockedPid, err
}
