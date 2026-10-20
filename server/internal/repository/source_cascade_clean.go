package repository

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// CascadeCleanSourceData 按块提交删除该站档案。中途失败再删一次会接着清完。
func CascadeCleanSourceData(id string) ([]int64, error) {
	id = strings.TrimSpace(id)
	if id == "" || db.Mdb == nil {
		return nil, nil
	}
	var owned []int64
	if err := db.Mdb.Model(&model.FilmIndex{}).Where("first_source_id = ?", id).Pluck("mid", &owned).Error; err != nil {
		return nil, err
	}
	if err := db.Mdb.Where("source_id = ?", id).Delete(&model.FilmSourcePlaylist{}).Error; err != nil {
		return nil, err
	}
	if err := db.Mdb.Where("source_id = ?", id).Delete(&model.MovieSourceMapping{}).Error; err != nil {
		return nil, err
	}
	const chunkSize = 1000
	deleted := make([]int64, 0, len(owned))
	for i := 0; i < len(owned); i += chunkSize {
		chunk := owned[i:min(i+chunkSize, len(owned))]
		if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			return deleteOwnedFilmChunk(tx, chunk)
		}); err != nil {
			return deleted, err
		}
		deleted = append(deleted, chunk...)
	}
	return deleted, nil
}

func deleteOwnedFilmChunk(tx *gorm.DB, mids []int64) error {
	if len(mids) == 0 {
		return nil
	}
	if err := tx.Where("mid IN ?", mids).Delete(&model.FilmSourcePlaylist{}).Error; err != nil {
		return err
	}
	if err := tx.Where("mid IN ?", mids).Delete(&model.MovieMatchKey{}).Error; err != nil {
		return err
	}
	if err := tx.Where("global_mid IN ?", mids).Delete(&model.MovieSourceMapping{}).Error; err != nil {
		return err
	}
	if err := tx.Where("mid IN ?", mids).Delete(&model.Banner{}).Error; err != nil {
		return err
	}
	if err := tx.Where("mid IN ?", mids).Delete(&model.FilmTMDBScrape{}).Error; err != nil {
		return err
	}
	return tx.Unscoped().Where("mid IN ?", mids).Delete(&model.FilmIndex{}).Error
}
