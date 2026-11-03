package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

// BatchSaveCollectRecordFilms 批量落库采集批次入库的影片编号清单
func BatchSaveCollectRecordFilms(batchID string, itemsBySource map[string][]int64) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	batchID = strings.TrimSpace(batchID)
	if batchID == "" || len(itemsBySource) == 0 {
		return nil
	}
	now := time.Now()
	var records []model.CollectRecordFilmItem
	for sourceID, mids := range itemsBySource {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			continue
		}
		for _, mid := range mids {
			if mid > 0 {
				records = append(records, model.CollectRecordFilmItem{
					BatchID:   batchID,
					SourceID:  sourceID,
					Mid:       mid,
					CreatedAt: now,
				})
			}
		}
	}
	if len(records) == 0 {
		return nil
	}
	return db.Mdb.CreateInBatches(records, 1000).Error
}

// GetCollectRecordFilms 分页查询某批次指定站点的影片列表（并回填片名）
func GetCollectRecordFilms(q model.CollectRecordFilmQuery) ([]model.CollectRecordFilm, int64, error) {
	if db.Mdb == nil {
		return nil, 0, errors.New("数据库未连接")
	}
	batchID := strings.TrimSpace(q.BatchID)
	sourceID := strings.TrimSpace(q.SourceID)
	if batchID == "" {
		return nil, 0, errors.New("批次号不能为空")
	}

	tx := db.Mdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", batchID)
	if sourceID != "" {
		tx = tx.Where("source_id = ?", sourceID)
	}

	keyword := strings.TrimSpace(q.Keyword)
	if keyword != "" {
		tx = tx.Where("mid IN (SELECT mid FROM "+model.TableFilmIndex+" WHERE name LIKE ?)", "%"+keyword+"%")
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.CollectRecordFilm{}, 0, nil
	}

	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}

	var rows []model.CollectRecordFilmItem
	err := tx.Order("id ASC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	mids := make([]int64, 0, len(rows))
	for _, r := range rows {
		mids = append(mids, r.Mid)
	}

	names, _ := FilmNamesByIDs(mids)
	out := make([]model.CollectRecordFilm, 0, len(rows))
	for _, r := range rows {
		name := names[r.Mid]
		if name == "" {
			name = fmt.Sprintf("影片 #%d", r.Mid)
		}
		out = append(out, model.CollectRecordFilm{
			Mid:  r.Mid,
			Name: name,
		})
	}
	return out, total, nil
}

// DeleteCollectRecordFilmsByBatchIDs 批量删除指定批次的影片关联记录
func DeleteCollectRecordFilmsByBatchIDs(batchIDs []string) error {
	if db.Mdb == nil || len(batchIDs) == 0 {
		return nil
	}
	return db.Mdb.Where("batch_id IN ?", batchIDs).Delete(&model.CollectRecordFilmItem{}).Error
}

// CleanOldCollectRecordFilms 清理指定天数前的采集批次影片关联记录
func CleanOldCollectRecordFilms(days int) error {
	if db.Mdb == nil {
		return nil
	}
	if days <= 0 {
		days = 30
	}
	threshold := time.Now().AddDate(0, 0, -days)
	return db.Mdb.Where("created_at < ?", threshold).Delete(&model.CollectRecordFilmItem{}).Error
}
