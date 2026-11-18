package repository

import (
	"errors"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// CascadeCleanSourceData 分短事务批量删除该站档案与挂载数据，避免大站长事务锁库。
func CascadeCleanSourceData(id string) ([]int64, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	return cleanSourceDataBatched(id)
}

// CascadeCleanSourceDataTx 在调用方事务内删除该站档案和挂载数据。
// 返回被删除的档案 mid；其他站的档案不删。
func CascadeCleanSourceDataTx(tx *gorm.DB, id string) ([]int64, error) {
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
	// 该站的轮播整组删除；其他站轮播里的该站影片随档案一起删。
	if err := DeleteBannerSlotsBySource(tx, id); err != nil {
		return nil, err
	}
	if err := deleteOwnedFilmsTx(tx, owned); err != nil {
		return nil, err
	}
	if err := SetFilmCountTx(tx, id, 0); err != nil {
		return nil, err
	}
	return owned, nil
}

func cleanSourceDataBatched(id string) ([]int64, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("采集站不存在")
	}
	var owned []int64
	if err := db.Mdb.Model(&model.FilmIndex{}).Where("first_source_id = ?", id).Pluck("mid", &owned).Error; err != nil {
		return nil, err
	}
	if err := deleteBySourceBatched(&model.FilmSourcePlaylist{}, model.TableFilmSourcePlaylist, "source_id", id); err != nil {
		return nil, err
	}
	if err := deleteBySourceBatched(&model.MovieSourceMapping{}, model.TableMovieSourceMapping, "source_id", id); err != nil {
		return nil, err
	}
	if err := DeleteBannerSlotsBySource(db.Mdb, id); err != nil {
		return nil, err
	}
	if err := deleteOwnedFilmsBatched(id, owned); err != nil {
		return nil, err
	}
	if err := SetFilmCountTx(db.Mdb, id, 0); err != nil {
		return nil, err
	}
	return owned, nil
}

func deleteBySourceBatched(modelPtr any, tableName, col, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if db.Mdb == nil || sourceID == "" {
		return nil
	}
	if db.Mdb.Dialector == nil || db.Mdb.Dialector.Name() != "mysql" {
		return db.Mdb.Where(col+" = ?", sourceID).Delete(modelPtr).Error
	}
	const batchLimit = 3000
	query := "DELETE FROM " + tableName + " WHERE " + col + " = ? LIMIT ?"
	for {
		res := db.Mdb.Exec(query, sourceID, batchLimit)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected < batchLimit {
			return nil
		}
	}
}

func deleteOwnedFilmsTx(tx *gorm.DB, mids []int64) error {
	for i := 0; i < len(mids); i += 1000 {
		if err := deleteOwnedFilmChunk(tx, mids[i:min(i+1000, len(mids))]); err != nil {
			return err
		}
	}
	return nil
}

func deleteOwnedFilmsBatched(sourceID string, mids []int64) error {
	for i := 0; i < len(mids); i += 1000 {
		chunk := mids[i:min(i+1000, len(mids))]
		if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			if err := deleteOwnedFilmChunk(tx, chunk); err != nil {
				return err
			}
			return AddFilmCountTx(tx, sourceID, -int64(len(chunk)))
		}); err != nil {
			return err
		}
	}
	return nil
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
	if err := DeleteBannerSlotsByMids(tx, mids); err != nil {
		return err
	}
	if err := tx.Where("mid IN ?", mids).Delete(&model.FilmTMDBScrape{}).Error; err != nil {
		return err
	}
	return tx.Unscoped().Where("mid IN ?", mids).Delete(&model.FilmIndex{}).Error
}
