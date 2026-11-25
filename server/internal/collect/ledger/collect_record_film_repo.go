package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"

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

var collectFilmChunkSize = 2000

func collectRecordFilmRows(batchID string, itemsBySource map[string][]int64) []model.CollectRecordFilmItem {
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
	return records
}

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
	records := collectRecordFilmRows(batchID, itemsBySource)
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

// SaveCollectRecordFilmsChunked 按几千行一批写入清单。每一批单独提交，任一批失败就停止。
func SaveCollectRecordFilmsChunked(batchID string, itemsBySource map[string][]int64) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	records := collectRecordFilmRows(batchID, itemsBySource)
	if len(records) == 0 {
		return nil
	}
	batchSize := collectFilmChunkSize
	if batchSize < 1 {
		batchSize = 2000
	}
	for start := 0; start < len(records); start += batchSize {
		end := start + batchSize
		if end > len(records) {
			end = len(records)
		}
		if err := insertCollectFilmChunk(records[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// DeleteCollectRecordFilms 删掉这一批已经写入的清单。结算失败时用来清半截数据。
func DeleteCollectRecordFilms(batchID string) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return errors.New("批次号不能为空")
	}
	return db.Mdb.Where("batch_id = ?", batchID).Delete(&model.CollectRecordFilmItem{}).Error
}

// ListCollectRecordMids 按批次取出清单里的影片编号，编号升序且不重复。
func ListCollectRecordMids(batchIDs []string) (map[string][]int64, error) {
	out := make(map[string][]int64)
	if db.Mdb == nil || len(batchIDs) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(batchIDs))
	seen := make(map[string]struct{}, len(batchIDs))
	for _, id := range batchIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []model.CollectRecordFilmItem
	if err := db.Mdb.Select("batch_id", "mid").
		Where("batch_id IN ?", ids).
		Order("mid ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	have := make(map[string]map[int64]struct{}, len(ids))
	for _, row := range rows {
		if row.Mid <= 0 {
			continue
		}
		set := have[row.BatchID]
		if set == nil {
			set = map[int64]struct{}{}
			have[row.BatchID] = set
		}
		if _, ok := set[row.Mid]; ok {
			continue
		}
		set[row.Mid] = struct{}{}
		out[row.BatchID] = append(out[row.BatchID], row.Mid)
	}
	return out, nil
}

// GetCollectRecordFilms 按游标取一页，并只给这一页补片名。
// 没搜片名时，总数用记录上的部数，按清单编号翻页。
// 搜片名时只在这一批里按名字开头匹配，总数是实际命中行数。
func GetCollectRecordFilms(q model.CollectRecordFilmQuery) ([]model.CollectRecordFilm, int64, error) {
	page, err := QueryCollectRecordFilms(q)
	return page.List, page.Total, err
}

func QueryCollectRecordFilms(q model.CollectRecordFilmQuery) (model.CollectRecordFilmPage, error) {
	page := model.CollectRecordFilmPage{List: []model.CollectRecordFilm{}}
	if db.Mdb == nil {
		return page, errors.New("数据库未连接")
	}
	batchID := strings.TrimSpace(q.BatchID)
	sourceID := strings.TrimSpace(q.SourceID)
	if batchID == "" {
		return page, errors.New("批次号不能为空")
	}
	ready, err := sourcecatalog.CollectRecordHasLedger(batchID)
	if err != nil || !ready {
		return page, err
	}
	keyword := strings.TrimSpace(q.Keyword)
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}

	var tx *gorm.DB
	if keyword != "" {
		return searchCollectRecordFilms(batchID, sourceID, keyword, q, pageSize)
	} else {
		total, err := collectRecordListedFilmTotal(batchID, sourceID)
		if err != nil || total == 0 {
			return page, err
		}
		page.Total = total
		tx = filmQuery(batchID, sourceID)
		if q.AfterID > 0 {
			tx = tx.Where("id > ?", q.AfterID)
		}
	}

	var rows []model.CollectRecordFilmItem
	if err := tx.Order("id ASC").Limit(pageSize + 1).Find(&rows).Error; err != nil {
		return page, err
	}
	if len(rows) > pageSize {
		page.HasMore = true
		rows = rows[:pageSize]
	}
	if len(rows) > 0 {
		page.NextAfterID = rows[len(rows)-1].ID
	}
	mids := make([]int64, 0, len(rows))
	for _, row := range rows {
		mids = append(mids, row.Mid)
	}
	names, err := FilmNamesByIDs(mids)
	if err != nil {
		return page, err
	}
	page.List = make([]model.CollectRecordFilm, 0, len(rows))
	for _, row := range rows {
		name := names[row.Mid]
		if name == "" {
			name = fmt.Sprintf("影片 #%d", row.Mid)
		}
		page.List = append(page.List, model.CollectRecordFilm{
			ID: row.ID, Mid: row.Mid, Name: name, SourceID: row.SourceID,
		})
	}
	return page, nil
}

func searchCollectRecordFilms(batchID, sourceID, keyword string, q model.CollectRecordFilmQuery, pageSize int) (model.CollectRecordFilmPage, error) {
	page := model.CollectRecordFilmPage{List: []model.CollectRecordFilm{}}
	pattern := likePrefix(keyword)
	base := func() *gorm.DB {
		tx := db.Mdb.Table(model.TableCollectRecordFilm+" AS f").
			Joins("INNER JOIN "+model.TableFilmIndex+" AS i ON i.mid = f.mid").
			Where("f.batch_id = ? AND i.name LIKE ? ESCAPE '!'", batchID, pattern)
		if sourceID != "" {
			tx = tx.Where("f.source_id = ?", sourceID)
		}
		return tx
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return page, err
	}
	page.Total = total
	if total == 0 {
		return page, nil
	}
	listQ := base().Select("f.id AS id, f.mid AS mid, f.source_id AS source_id, i.name AS name")
	if q.AfterName != "" || q.AfterMid > 0 || q.AfterID > 0 {
		listQ = listQ.Where(
			"i.name > ? OR (i.name = ? AND f.mid > ?) OR (i.name = ? AND f.mid = ? AND f.id > ?)",
			q.AfterName, q.AfterName, q.AfterMid, q.AfterName, q.AfterMid, q.AfterID,
		)
	}
	var rows []struct {
		ID       uint
		Mid      int64
		SourceID string `gorm:"column:source_id"`
		Name     string
	}
	if err := listQ.Order("i.name ASC, f.mid ASC, f.id ASC").Limit(pageSize + 1).Find(&rows).Error; err != nil {
		return page, err
	}
	if len(rows) > pageSize {
		page.HasMore = true
		rows = rows[:pageSize]
	}
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		page.NextAfterID = last.ID
		page.NextAfterName = last.Name
		page.NextAfterMid = last.Mid
	}
	page.List = make([]model.CollectRecordFilm, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			name = fmt.Sprintf("影片 #%d", row.Mid)
		}
		page.List = append(page.List, model.CollectRecordFilm{
			ID: row.ID, Mid: row.Mid, Name: name, SourceID: row.SourceID,
		})
	}
	return page, nil
}

func filmQuery(batchID, sourceID string) *gorm.DB {
	tx := db.Mdb.Model(&model.CollectRecordFilmItem{}).Where("batch_id = ?", batchID)
	if sourceID != "" {
		tx = tx.Where("source_id = ?", sourceID)
	}
	return tx
}

// collectRecordListedFilmTotal 全部站点用记录上的清单行数，单站用明细里的行数。不扫清单表。
// 记录不存在或该站不在明细里则总数为 0。明细损坏直接返回错误。
func collectRecordListedFilmTotal(batchID, sourceID string) (int64, error) {
	var rec model.CollectRecord
	err := db.Mdb.Select("ledger", "film_count", "detail_json").Where("batch_id = ?", batchID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !rec.Ledger {
		return 0, nil
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
