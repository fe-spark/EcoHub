package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	return db.Mdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "batch_id"}, {Name: "source_id"}, {Name: "mid"}},
		DoNothing: true,
	}).CreateInBatches(records, 1000).Error
}

func collectRecordFilmScope(batchID, sourceID, keyword string) *gorm.DB {
	tx := db.Mdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", batchID)
	if sourceID != "" {
		tx = tx.Where("source_id = ?", sourceID)
	}
	if keyword != "" {
		pattern := "%" + escapeLikePattern(keyword) + "%"
		tx = tx.Where("mid IN (SELECT mid FROM "+model.TableFilmIndex+" WHERE name LIKE ?)", pattern)
	}
	return tx
}

// GetCollectRecordFilms 分页查询某批次的影片列表并回填片名。
// 指定站点时按该站的行分页；全部站点按 mid 去重，总数与批次 filmCount 对齐。
func GetCollectRecordFilms(q model.CollectRecordFilmQuery) ([]model.CollectRecordFilm, int64, error) {
	if db.Mdb == nil {
		return nil, 0, errors.New("数据库未连接")
	}
	batchID := strings.TrimSpace(q.BatchID)
	sourceID := strings.TrimSpace(q.SourceID)
	if batchID == "" {
		return nil, 0, errors.New("批次号不能为空")
	}
	keyword := strings.TrimSpace(q.Keyword)

	var total int64
	countTx := collectRecordFilmScope(batchID, sourceID, keyword)
	if sourceID == "" {
		countTx = countTx.Distinct("mid")
	}
	if err := countTx.Count(&total).Error; err != nil {
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

	var mids []int64
	listTx := collectRecordFilmScope(batchID, sourceID, keyword)
	if sourceID == "" {
		if err := listTx.Distinct("mid").Order("mid ASC").
			Offset((page-1)*pageSize).Limit(pageSize).
			Pluck("mid", &mids).Error; err != nil {
			return nil, 0, err
		}
	} else {
		var rows []model.CollectRecordFilmItem
		if err := listTx.Order("id ASC").
			Offset((page - 1) * pageSize).Limit(pageSize).
			Find(&rows).Error; err != nil {
			return nil, 0, err
		}
		mids = make([]int64, 0, len(rows))
		for _, row := range rows {
			mids = append(mids, row.Mid)
		}
	}

	names, err := FilmNamesByIDs(mids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.CollectRecordFilm, 0, len(mids))
	for _, mid := range mids {
		name := names[mid]
		if name == "" {
			name = fmt.Sprintf("影片 #%d", mid)
		}
		out = append(out, model.CollectRecordFilm{Mid: mid, Name: name})
	}
	return out, total, nil
}

// escapeLikePattern 转义 LIKE 通配符，避免用户输入的 % 和 _ 被当成模式。
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

