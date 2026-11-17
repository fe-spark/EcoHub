package repository

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// AddFilmCountTx 增减采集站档案数。调用方和影片写入或删除在同一个事务里。
func AddFilmCountTx(tx *gorm.DB, sourceID string, delta int64) error {
	sourceID = strings.TrimSpace(sourceID)
	if tx == nil || sourceID == "" || delta == 0 {
		return nil
	}
	return tx.Model(&model.FilmSource{}).Where("id = ?", sourceID).
		Update("film_count", gorm.Expr("CASE WHEN film_count + ? < 0 THEN 0 ELSE film_count + ? END", delta, delta)).Error
}

// SetFilmCountTx 把采集站档案数写成确定值。清空该站档案后用 0。
func SetFilmCountTx(tx *gorm.DB, sourceID string, n int64) error {
	sourceID = strings.TrimSpace(sourceID)
	if tx == nil || sourceID == "" {
		return nil
	}
	if n < 0 {
		n = 0
	}
	return tx.Model(&model.FilmSource{}).Where("id = ?", sourceID).Update("film_count", n).Error
}

// ZeroFilmCounts 片库被整表清空后，各站档案数归零。
func ZeroFilmCounts(tx *gorm.DB) error {
	if tx == nil {
		return nil
	}
	return tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Model(&model.FilmSource{}).Update("film_count", 0).Error
}

// SubtractFilmCountsByMidsTx 按即将删除的档案扣减所属站计数。必须在删除之前调用。
func SubtractFilmCountsByMidsTx(tx *gorm.DB, mids []int64) error {
	if tx == nil || len(mids) == 0 {
		return nil
	}
	var rows []filmCountRow
	if err := tx.Model(&model.FilmIndex{}).
		Select("first_source_id AS source_id, COUNT(*) AS n").
		Where("mid IN ?", mids).
		Group("first_source_id").
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := AddFilmCountTx(tx, row.SourceID, -row.N); err != nil {
			return err
		}
	}
	return nil
}

// RefreshFilmCounts 按未删除档案重写各站影片数。启动时对齐一次，平时增删走加减。
func RefreshFilmCounts() error {
	if db.Mdb == nil {
		return nil
	}
	if err := ZeroFilmCounts(db.Mdb); err != nil {
		return err
	}
	var rows []filmCountRow
	if err := db.Mdb.Model(&model.FilmIndex{}).
		Select("first_source_id AS source_id, COUNT(*) AS n").
		Where("first_source_id <> ?", "").
		Group("first_source_id").
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := SetFilmCountTx(db.Mdb, row.SourceID, row.N); err != nil {
			return err
		}
	}
	return nil
}

type filmCountRow struct {
	SourceID string `gorm:"column:source_id"`
	N        int64  `gorm:"column:n"`
}
