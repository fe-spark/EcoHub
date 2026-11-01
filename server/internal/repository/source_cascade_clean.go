package repository

import (
	"errors"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// CascadeCleanSourceData 在一个事务里删除该站档案。失败则全部回滚。
func CascadeCleanSourceData(id string) ([]int64, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	var owned []int64
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		var err error
		owned, err = cascadeCleanSourceDataTx(tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return owned, nil
}

func cascadeCleanSourceDataTx(tx *gorm.DB, id string) ([]int64, error) {
	id = strings.TrimSpace(id)
	if id == "" || tx == nil {
		return nil, errors.New("采集站不存在")
	}
	var owned []int64
	if err := tx.Model(&model.FilmIndex{}).Where("first_source_id = ?", id).Pluck("mid", &owned).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("source_id = ?", id).Delete(&model.FilmSourcePlaylist{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("source_id = ?", id).Delete(&model.MovieSourceMapping{}).Error; err != nil {
		return nil, err
	}
	if err := deleteOwnedFilmsTx(tx, owned); err != nil {
		return nil, err
	}
	return owned, nil
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
