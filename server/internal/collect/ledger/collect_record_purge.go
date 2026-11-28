package ledger

import (
	"encoding/json"
	"errors"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"

	"gorm.io/gorm"
)

// PurgeSourceCollectHistoryBatched 按独立短事务分批清理指定站点的采集历史，避免单一长事务锁表与 OOM。
func PurgeSourceCollectHistoryBatched(sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if db.Mdb == nil || sourceID == "" {
		return nil
	}
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		return sourcecatalog.DeleteCollectSourceStatsTx(tx, sourceID)
	}); err != nil {
		return err
	}
	var aliveIDs []string
	if err := db.Mdb.Model(&model.FilmSource{}).Pluck("id", &aliveIDs).Error; err != nil {
		return err
	}
	alive := buildAliveSourceSet(aliveIDs)

	const scanChunk = 200
	var cursor uint
	for {
		var done bool
		if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			nextCursor, hasMore, err := purgeSourceCollectHistoryChunk(tx, sourceID, cursor, scanChunk, alive)
			if err != nil {
				return err
			}
			cursor = nextCursor
			done = !hasMore
			return nil
		}); err != nil {
			return err
		}
		if done {
			break
		}
	}
	return nil
}

func buildAliveSourceSet(aliveIDs []string) map[string]struct{} {
	alive := make(map[string]struct{}, len(aliveIDs))
	for _, id := range aliveIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			alive[id] = struct{}{}
		}
	}
	return alive
}

// PurgeSourceCollectHistoryTx 删站时在既有事务内清掉该站的统计与历史记录。
func PurgeSourceCollectHistoryTx(tx *gorm.DB, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if tx == nil || sourceID == "" {
		return nil
	}
	if err := sourcecatalog.DeleteCollectSourceStatsTx(tx, sourceID); err != nil {
		return err
	}
	var aliveIDs []string
	if err := tx.Model(&model.FilmSource{}).Pluck("id", &aliveIDs).Error; err != nil {
		return err
	}
	alive := buildAliveSourceSet(aliveIDs)
	const scanChunk = 200
	var cursor uint
	for {
		nextCursor, hasMore, err := purgeSourceCollectHistoryChunk(tx, sourceID, cursor, scanChunk, alive)
		if err != nil {
			return err
		}
		cursor = nextCursor
		if !hasMore {
			break
		}
	}
	return nil
}

func purgeSourceCollectHistoryChunk(tx *gorm.DB, sourceID string, cursor uint, scanChunk int, alive map[string]struct{}) (uint, bool, error) {
	var rows []model.CollectRecord
	if err := tx.Select("id", "batch_id", "status", "error_msg", "detail_json").
		Where("id > ? AND detail_json LIKE ?", cursor, "%"+sourceID+"%").
		Order("id ASC").
		Limit(scanChunk).
		Find(&rows).Error; err != nil {
		return cursor, false, err
	}
	if len(rows) == 0 {
		return cursor, false, nil
	}
	nextCursor := rows[len(rows)-1].ID
	dropIDs := make([]uint, 0)
	chunkKeep := make([]model.CollectRecord, 0)
	for _, row := range rows {
		if !strings.Contains(row.DetailJson, sourceID) {
			continue
		}
		if collectRecordOnlySource(row.DetailJson, sourceID, alive) {
			if row.ID > 0 {
				dropIDs = append(dropIDs, row.ID)
			}
			continue
		}
		chunkKeep = append(chunkKeep, row)
	}
	if len(dropIDs) > 0 {
		if err := tx.Where("id IN ?", dropIDs).Delete(&model.CollectRecord{}).Error; err != nil {
			return nextCursor, false, err
		}
	}
	for _, row := range chunkKeep {
		if _, err := stripSourceFromCollectRecord(tx, row, sourceID); err != nil {
			return nextCursor, false, err
		}
	}
	return nextCursor, len(rows) >= scanChunk, nil
}

func stripSourceFromCollectRecord(tx *gorm.DB, row model.CollectRecord, sourceID string) (string, error) {
	var details []model.CollectRecordSourceDetail
	if err := json.Unmarshal([]byte(row.DetailJson), &details); err != nil {
		return "", err
	}
	kept := make([]model.CollectRecordSourceDetail, 0, len(details))
	for _, detail := range details {
		if strings.TrimSpace(detail.SourceID) == sourceID {
			continue
		}
		kept = append(kept, detail)
	}
	if len(kept) == len(details) {
		return row.DetailJson, nil
	}
	filmCount := 0
	for _, d := range kept {
		filmCount += d.FilmsTotal
	}
	raw, err := json.Marshal(kept)
	if err != nil {
		return "", err
	}
	done, partial, failed, stopped, interrupted := 0, 0, 0, 0, 0
	pageTotal, pageSuccess, pageFailed := 0, 0, 0
	var errSummary strings.Builder
	for _, detail := range kept {
		pageTotal += detail.PageTotal
		pageSuccess += detail.SuccessCnt
		pageFailed += detail.FailedCnt
		switch detail.Status {
		case "done", model.CollectRecordStatusSuccess:
			done++
		case model.CollectRecordStatusPartial:
			partial++
		case model.CollectRecordStatusFailed:
			failed++
		case model.CollectRecordStatusStopped:
			stopped++
		case model.CollectRecordStatusInterrupted:
			interrupted++
		}
		if msg := strings.TrimSpace(detail.Error); msg != "" {
			if errSummary.Len() > 0 {
				errSummary.WriteString("; ")
			}
			name := strings.TrimSpace(detail.SourceName)
			if name == "" {
				name = detail.SourceID
			}
			errSummary.WriteString(name + ": " + msg)
		}
	}
	status := model.CollectBatchStatus(done, partial, failed, stopped, interrupted, len(kept), pageSuccess, pageFailed)
	errMsg := errSummary.String()
	// 进行中的批次留给结算写终态。先改成终态后，结算不再更新这条记录。
	if row.Status == model.CollectRecordStatusRunning {
		status = row.Status
		errMsg = row.ErrorMsg
	} else if status == model.CollectRecordStatusInterrupted && errMsg == "" {
		errMsg = row.ErrorMsg
	}
	res := tx.Model(&model.CollectRecord{}).Where("id = ? AND status = ?", row.ID, row.Status).Updates(map[string]any{
		"detail_json":     string(raw),
		"status":          status,
		"source_count":    len(kept),
		"success_sources": done,
		"failed_sources":  failed + partial,
		"film_count":      filmCount,
		"page_total":      pageTotal,
		"page_success":    pageSuccess,
		"page_failed":     pageFailed,
		"error_msg":       errMsg,
	})
	if res.Error != nil {
		return "", res.Error
	}
	if res.RowsAffected == 0 {
		var fresh model.CollectRecord
		if err := tx.Select("id", "batch_id", "status", "error_msg", "detail_json").
			Where("id = ?", row.ID).First(&fresh).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", nil
			}
			return "", err
		}
		if fresh.Status != row.Status {
			return stripSourceFromCollectRecord(tx, fresh, sourceID)
		}
	}
	return string(raw), nil
}

// 明细里没有别的存活站点时，整条记录删掉。正在删的这一站不算存活。
func collectRecordOnlySource(detailJSON, sourceID string, alive map[string]struct{}) bool {
	raw := strings.TrimSpace(detailJSON)
	if raw == "" || raw == "[]" || raw == "null" {
		return false
	}
	var details []model.CollectRecordSourceDetail
	if err := json.Unmarshal([]byte(raw), &details); err != nil {
		return false
	}
	mentioned := false
	for _, detail := range details {
		id := strings.TrimSpace(detail.SourceID)
		if id == "" {
			continue
		}
		if id == sourceID {
			mentioned = true
			continue
		}
		if _, ok := alive[id]; ok {
			return false
		}
	}
	return mentioned
}
