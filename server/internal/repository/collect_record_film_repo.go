package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UnstoredFilmsError 某一批清单没写上。Films 只含失败的那一批，前面和后面已提交的不在里面。
type UnstoredFilmsError struct {
	Films map[string][]int64
	Err   error
}

func (e *UnstoredFilmsError) Error() string {
	if e == nil || e.Err == nil {
		return "保存采集影片清单失败"
	}
	return e.Err.Error()
}

func (e *UnstoredFilmsError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// UnstoredCollectFilms 取出写入失败的站点影片编号。不是分批失败时返回 nil。
func UnstoredCollectFilms(err error) map[string][]int64 {
	var target *UnstoredFilmsError
	if errors.As(err, &target) {
		return target.Films
	}
	return nil
}

var collectFilmChunkSize = 1000

var insertCollectFilmChunk = func(chunk []model.CollectRecordFilmItem) error {
	return db.Mdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "batch_id"}, {Name: "source_id"}, {Name: "mid"}},
		DoNothing: true,
	}).Create(&chunk).Error
}

// BatchSaveCollectRecordFilms 批量落库采集批次入库的影片编号清单。
// 每一批单独提交。某一批失败时继续写后面的批，并在错误里带回没写上的编号。
func BatchSaveCollectRecordFilms(batchID string, itemsBySource map[string][]int64) error {
	if db.Mdb == nil {
		if len(itemsBySource) == 0 {
			return errors.New("数据库未连接")
		}
		return &UnstoredFilmsError{Films: itemsBySource, Err: errors.New("数据库未连接")}
	}
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		if len(itemsBySource) == 0 {
			return nil
		}
		return &UnstoredFilmsError{Films: itemsBySource, Err: errors.New("批次号不能为空")}
	}
	if len(itemsBySource) == 0 {
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
	batchSize := collectFilmChunkSize
	if batchSize < 1 {
		batchSize = 1000
	}
	var failed []error
	unwritten := map[string][]int64{}
	for start := 0; start < len(records); start += batchSize {
		end := start + batchSize
		if end > len(records) {
			end = len(records)
		}
		chunk := records[start:end]
		if err := insertCollectFilmChunk(chunk); err != nil {
			failed = append(failed, err)
			for _, row := range chunk {
				unwritten[row.SourceID] = append(unwritten[row.SourceID], row.Mid)
			}
			continue
		}
	}
	if len(failed) > 0 {
		return &UnstoredFilmsError{Films: unwritten, Err: errors.Join(failed...)}
	}
	return nil
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
// 无关键词时总数取采集记录上的清单行数：全部站点是各站 FilmsTotal 之和，单站用该站的 FilmsTotal。
// 有关键词时才按片名过滤并统计过滤结果。列表按主键取当前页。
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

	var total int64
	var err error
	if keyword == "" {
		total, err = collectRecordListedFilmTotal(batchID, sourceID)
	} else {
		err = collectRecordFilmScope(batchID, sourceID, keyword).Count(&total).Error
	}
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.CollectRecordFilm{}, 0, nil
	}

	var rows []model.CollectRecordFilmItem
	if err := collectRecordFilmScope(batchID, sourceID, keyword).Order("id ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	mids := make([]int64, 0, len(rows))
	for _, row := range rows {
		mids = append(mids, row.Mid)
	}
	names, err := FilmNamesByIDs(mids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.CollectRecordFilm, 0, len(rows))
	for _, row := range rows {
		name := names[row.Mid]
		if name == "" {
			name = fmt.Sprintf("影片 #%d", row.Mid)
		}
		out = append(out, model.CollectRecordFilm{Mid: row.Mid, Name: name, SourceID: row.SourceID})
	}
	return out, total, nil
}

// collectRecordListedFilmTotal 全部站点用记录上的清单行数，单站用明细里的行数。不扫清单表。
// 记录不存在或该站不在明细里则总数为 0。明细损坏直接返回错误。
func collectRecordListedFilmTotal(batchID, sourceID string) (int64, error) {
	var rec model.CollectRecord
	err := db.Mdb.Select("film_count", "detail_json").Where("batch_id = ?", batchID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return int64(rec.FilmCount), nil
	}
	raw := strings.TrimSpace(rec.DetailJson)
	if raw == "" {
		return 0, nil
	}
	var details []model.CollectRecordSourceDetail
	if err := json.Unmarshal([]byte(raw), &details); err != nil {
		return 0, fmt.Errorf("批次明细损坏: %w", err)
	}
	for _, detail := range details {
		if detail.SourceID == sourceID {
			return int64(detail.FilmsTotal), nil
		}
	}
	return 0, nil
}

// filmRowCounts 按站点统计清单行数。总数是各站行数之和，同一影片在多站各算一行。
func filmRowCounts(tx *gorm.DB, batchID string) (int, map[string]int, error) {
	bySource := map[string]int{}
	batchID = strings.TrimSpace(batchID)
	if tx == nil || batchID == "" {
		return 0, bySource, nil
	}
	var rows []struct {
		SourceID string `gorm:"column:source_id"`
		N        int    `gorm:"column:n"`
	}
	if err := tx.Model(&model.CollectRecordFilmItem{}).
		Select("source_id, COUNT(*) AS n").
		Where("batch_id = ?", batchID).
		Group("source_id").
		Scan(&rows).Error; err != nil {
		return 0, nil, err
	}
	total := 0
	for _, row := range rows {
		bySource[row.SourceID] = row.N
		total += row.N
	}
	return total, bySource, nil
}

// applyStoredFilmTotals 把已经落库的影片数写回站点明细，并把还在跑的站标成中断。
func applyStoredFilmTotals(raw string, bySource map[string]int) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	var details []model.CollectRecordSourceDetail
	if err := json.Unmarshal([]byte(raw), &details); err != nil {
		return "", fmt.Errorf("批次明细损坏: %w", err)
	}
	changed := false
	for i := range details {
		if n, ok := bySource[details[i].SourceID]; ok && details[i].FilmsTotal != n {
			details[i].FilmsTotal = n
			changed = true
		}
		if details[i].Status == "running" || details[i].Status == "starting" {
			details[i].Status = model.CollectRecordStatusInterrupted
			changed = true
		}
	}
	if !changed {
		return "", nil
	}
	out, err := json.Marshal(details)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// escapeLikePattern 转义 LIKE 通配符，避免用户输入的 % 和 _ 被当成模式。
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}
