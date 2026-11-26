package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	collectFilmChunkSize = 2000
	searchFilmScanChunk  = 200
	maxSearchFilmScan    = 5000
	maxSearchFilmRounds  = 25
)

var insertCollectFilmChunk = func(chunk []model.CollectRecordFilmItem) error {
	return db.Mdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "record_id"}, {Name: "source_id"}, {Name: "mid"}},
		DoNothing: true,
	}).Create(&chunk).Error
}

// AppendCollectRecordFilms 按固定块大小顺序追加写入账本行，任一块失败立即返回。
func AppendCollectRecordFilms(items []model.CollectRecordFilmItem) error {
	if len(items) == 0 {
		return nil
	}
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	valid := make([]model.CollectRecordFilmItem, 0, len(items))
	for _, item := range items {
		sid := strings.TrimSpace(item.SourceID)
		if item.RecordID == 0 || sid == "" || item.Mid <= 0 {
			continue
		}
		valid = append(valid, model.CollectRecordFilmItem{
			RecordID: item.RecordID,
			SourceID: sid,
			Mid:      item.Mid,
		})
	}
	if len(valid) == 0 {
		return nil
	}
	batchSize := collectFilmChunkSize
	if batchSize < 1 {
		batchSize = 2000
	}
	for start := 0; start < len(valid); start += batchSize {
		end := start + batchSize
		if end > len(valid) {
			end = len(valid)
		}
		if err := insertCollectFilmChunk(valid[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// SaveCollectRecordFilms 按站点映射批量写入采集批次账本。
func SaveCollectRecordFilms(recordID uint, itemsBySource map[string][]int64) error {
	if recordID == 0 || len(itemsBySource) == 0 {
		return nil
	}
	var rows []model.CollectRecordFilmItem
	for sourceID, mids := range itemsBySource {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			continue
		}
		for _, mid := range mids {
			if mid > 0 {
				rows = append(rows, model.CollectRecordFilmItem{
					RecordID: recordID,
					SourceID: sourceID,
					Mid:      mid,
				})
			}
		}
	}
	return AppendCollectRecordFilms(rows)
}

// DeleteCollectRecordFilms 分批清理指定采集记录的账本行，避免全批大 DELETE 触发锁超时。
func DeleteCollectRecordFilms(recordID uint) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	if recordID == 0 {
		return errors.New("记录编号不能为空")
	}
	session := db.Mdb.Session(&gorm.Session{SkipDefaultTransaction: true})
	return deleteCollectFilmsByRecordIDs(session, []uint{recordID})
}

// ListCollectRecordMids 按记录主键取出清单里的影片编号，编号升序且不重复。
func ListCollectRecordMids(recordIDs []uint) (map[uint][]int64, error) {
	out := make(map[uint][]int64)
	if db.Mdb == nil || len(recordIDs) == 0 {
		return out, nil
	}
	ids := make([]uint, 0, len(recordIDs))
	seen := make(map[uint]struct{}, len(recordIDs))
	for _, id := range recordIDs {
		if id == 0 {
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
	if err := db.Mdb.Select("record_id", "mid").
		Where("record_id IN ?", ids).
		Order("mid ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	have := make(map[uint]map[int64]struct{}, len(ids))
	for _, row := range rows {
		if row.Mid <= 0 {
			continue
		}
		set := have[row.RecordID]
		if set == nil {
			set = map[int64]struct{}{}
			have[row.RecordID] = set
		}
		if _, ok := set[row.Mid]; ok {
			continue
		}
		set[row.Mid] = struct{}{}
		out[row.RecordID] = append(out[row.RecordID], row.Mid)
	}
	return out, nil
}

// QueryCollectRecordFilms 按聚簇主键游标取一页并仅对当前页补片名。
// 浏览态不扫大表 COUNT；搜索态沿 film_index 片名前缀索引顺序扫描并回账本主键早停，避免跨表 filesort 与全量 COUNT。
func QueryCollectRecordFilms(q model.CollectRecordFilmQuery) (model.CollectRecordFilmPage, error) {
	page := model.CollectRecordFilmPage{List: []model.CollectRecordFilm{}}
	if db.Mdb == nil {
		return page, errors.New("数据库未连接")
	}
	batchID := strings.TrimSpace(q.BatchID)
	if batchID == "" {
		return page, errors.New("批次号不能为空")
	}
	var rec model.CollectRecord
	err := db.Mdb.Select("id", "ledger", "film_count", "detail_json").Where("batch_id = ?", batchID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return page, nil
	}
	if err != nil {
		return page, err
	}
	if !rec.Ledger {
		return page, nil
	}

	sourceID := strings.TrimSpace(q.SourceID)
	keyword := strings.TrimSpace(q.Keyword)
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}

	if keyword != "" {
		return searchCollectRecordFilms(rec, sourceID, keyword, q, pageSize)
	}

	total, err := listedFilmTotal(rec, sourceID)
	if err != nil || total == 0 {
		return page, err
	}
	page.Total = total

	tx := db.Mdb.Model(&model.CollectRecordFilmItem{}).
		Select("source_id", "mid").
		Where("record_id = ?", rec.ID)
	if sourceID != "" {
		tx = tx.Where("source_id = ?", sourceID)
		if q.AfterMid > 0 {
			tx = tx.Where("mid > ?", q.AfterMid)
		}
		tx = tx.Order("mid ASC")
	} else {
		if q.AfterSource != "" || q.AfterMid > 0 {
			tx = tx.Where("source_id > ? OR (source_id = ? AND mid > ?)", q.AfterSource, q.AfterSource, q.AfterMid)
		}
		tx = tx.Order("source_id ASC, mid ASC")
	}

	var rows []model.CollectRecordFilmItem
	if err := tx.Limit(pageSize + 1).Find(&rows).Error; err != nil {
		return page, err
	}
	if len(rows) > pageSize {
		page.HasMore = true
		rows = rows[:pageSize]
	}
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		page.NextAfterSource = last.SourceID
		page.NextAfterMid = last.Mid
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
			Mid:      row.Mid,
			Name:     name,
			SourceID: row.SourceID,
		})
	}
	return page, nil
}

// searchCollectRecordFilms 沿 film_index 片名前缀索引有序扫描，再按账本聚簇主键 (record_id, source_id, mid) 批量点查并凑满一页即早停。
func searchCollectRecordFilms(rec model.CollectRecord, sourceID, keyword string, q model.CollectRecordFilmQuery, pageSize int) (model.CollectRecordFilmPage, error) {
	page := model.CollectRecordFilmPage{List: []model.CollectRecordFilm{}, Total: -1}
	targetSources, err := resolveSearchSources(rec, sourceID)
	if err != nil || len(targetSources) == 0 {
		return page, err
	}

	pattern := likePrefix(keyword)
	scanChunk := searchFilmScanChunk
	if scanChunk < 1 {
		scanChunk = 200
	}
	scanBudget := maxSearchFilmScan
	if scanBudget < scanChunk {
		scanBudget = scanChunk
	}
	roundBudget := maxSearchFilmRounds
	if roundBudget < 1 {
		roundBudget = 25
	}

	type filmCand struct {
		Mid  int64
		Name string
	}
	curName := q.AfterName
	curMid := q.AfterMid
	firstChunk := true
	scanned := 0
	rounds := 0
	matched := make([]model.CollectRecordFilm, 0, pageSize+1)
	rawNames := make([]string, 0, pageSize+1)

	for scanned < scanBudget && rounds < roundBudget {
		rounds++
		limit := scanChunk
		if remain := scanBudget - scanned; remain < limit {
			limit = remain
		}
		candQ := db.Mdb.Table(model.TableFilmIndex).
			Select("mid", "name").
			Where("name LIKE ? ESCAPE '!'", pattern)
		if firstChunk {
			if curName != "" || curMid > 0 {
				if q.AfterSource != "" {
					candQ = candQ.Where("name > ? OR (name = ? AND mid >= ?)", curName, curName, curMid)
				} else {
					candQ = candQ.Where("name > ? OR (name = ? AND mid > ?)", curName, curName, curMid)
				}
			}
			firstChunk = false
		} else {
			candQ = candQ.Where("name > ? OR (name = ? AND mid > ?)", curName, curName, curMid)
		}

		var cands []filmCand
		if err := candQ.Order("name ASC, mid ASC").Limit(limit).Find(&cands).Error; err != nil {
			return page, err
		}
		if len(cands) == 0 {
			break
		}
		scanned += len(cands)
		lastCand := cands[len(cands)-1]
		curName = lastCand.Name
		curMid = lastCand.Mid

		candMids := make([]int64, 0, len(cands))
		for _, c := range cands {
			if c.Mid > 0 {
				candMids = append(candMids, c.Mid)
			}
		}
		if len(candMids) > 0 {
			var hits []model.CollectRecordFilmItem
			if err := db.Mdb.Model(&model.CollectRecordFilmItem{}).
				Select("source_id", "mid").
				Where("record_id = ? AND source_id IN ? AND mid IN ?", rec.ID, targetSources, candMids).
				Order("source_id ASC").
				Find(&hits).Error; err != nil {
				return page, err
			}
			if len(hits) > 0 {
				byMid := make(map[int64][]string, len(hits))
				for _, h := range hits {
					byMid[h.Mid] = append(byMid[h.Mid], h.SourceID)
				}
				for _, c := range cands {
					sids := byMid[c.Mid]
					if len(sids) == 0 {
						continue
					}
					name := strings.TrimSpace(c.Name)
					if name == "" {
						name = fmt.Sprintf("影片 #%d", c.Mid)
					}
					for _, sid := range sids {
						// mid 为 film_index 主键，单靠 (mid, source_id) 即可精确跳过同片已展示过的站点，规避空格与大小写差异。
						if q.AfterSource != "" && c.Mid == q.AfterMid && sid <= q.AfterSource {
							continue
						}
						matched = append(matched, model.CollectRecordFilm{
							Mid:      c.Mid,
							Name:     name,
							SourceID: sid,
						})
						rawNames = append(rawNames, c.Name)
						if len(matched) > pageSize {
							page.HasMore = true
							matched = matched[:pageSize]
							rawNames = rawNames[:pageSize]
							last := matched[len(matched)-1]
							page.List = matched
							page.NextAfterName = rawNames[len(rawNames)-1]
							page.NextAfterMid = last.Mid
							page.NextAfterSource = last.SourceID
							return page, nil
						}
					}
				}
			}
		}

		if len(cands) < limit {
			break
		}
		if scanned >= scanBudget || rounds >= roundBudget {
			page.Truncated = true
			break
		}
	}

	page.List = matched
	if len(matched) > 0 {
		last := matched[len(matched)-1]
		page.NextAfterName = rawNames[len(rawNames)-1]
		page.NextAfterMid = last.Mid
		page.NextAfterSource = last.SourceID
	}
	return page, nil
}

func resolveSearchSources(rec model.CollectRecord, sourceID string) ([]string, error) {
	if sourceID != "" {
		return []string{sourceID}, nil
	}
	if raw := strings.TrimSpace(rec.DetailJson); raw != "" {
		var details []model.CollectRecordSourceDetail
		if err := json.Unmarshal([]byte(raw), &details); err != nil {
			return nil, fmt.Errorf("批次明细损坏: %w", err)
		}
		out := make([]string, 0, len(details))
		for _, d := range details {
			if sid := strings.TrimSpace(d.SourceID); sid != "" {
				out = append(out, sid)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	var sources []string
	if err := db.Mdb.Model(&model.CollectRecordFilmItem{}).
		Where("record_id = ?", rec.ID).
		Distinct("source_id").
		Order("source_id ASC").
		Pluck("source_id", &sources).Error; err != nil {
		return nil, err
	}
	return sources, nil
}

// listedFilmTotal 全部站点用记录上的清单行数，单站用明细里的行数，不扫大表。
func listedFilmTotal(rec model.CollectRecord, sourceID string) (int64, error) {
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

// FilmRowCounts 沿聚簇主键 (record_id, source_id, mid) 顺序聚合各站清单行数。
func FilmRowCounts(tx *gorm.DB, recordID uint) (int, map[string]int, error) {
	bySource := map[string]int{}
	if tx == nil || recordID == 0 {
		return 0, bySource, nil
	}
	var rows []struct {
		SourceID string `gorm:"column:source_id"`
		N        int    `gorm:"column:n"`
	}
	if err := tx.Model(&model.CollectRecordFilmItem{}).
		Select("source_id, COUNT(*) AS n").
		Where("record_id = ?", recordID).
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
