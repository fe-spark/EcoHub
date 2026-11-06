package repository

import (
	"encoding/json"
	"strings"

	"server/internal/model"

	"gorm.io/gorm"
)

// purgeSourceCollectHistoryTx 删站时清掉该站的清单行和统计。只有这一站的批次整条删掉，多站批次保留。
func purgeSourceCollectHistoryTx(tx *gorm.DB, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if tx == nil || sourceID == "" {
		return nil
	}
	if err := tx.Where("source_id = ?", sourceID).Delete(&model.CollectRecordFilmItem{}).Error; err != nil {
		return err
	}
	if err := DeleteCollectSourceStatsTx(tx, sourceID); err != nil {
		return err
	}
	var aliveIDs []string
	if err := tx.Model(&model.FilmSource{}).Pluck("id", &aliveIDs).Error; err != nil {
		return err
	}
	alive := make(map[string]struct{}, len(aliveIDs))
	for _, id := range aliveIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			alive[id] = struct{}{}
		}
	}
	var rows []model.CollectRecord
	if err := tx.Select("id", "batch_id", "status", "error_msg", "detail_json").
		Where("detail_json LIKE ?", "%"+sourceID+"%").
		Find(&rows).Error; err != nil {
		return err
	}
	drop := make([]model.CollectRecord, 0)
	for _, row := range rows {
		if !strings.Contains(row.DetailJson, sourceID) {
			continue
		}
		if collectRecordOnlySource(row.DetailJson, sourceID, alive) {
			drop = append(drop, row)
			continue
		}
		if _, err := stripSourceFromCollectRecord(tx, row, sourceID); err != nil {
			return err
		}
	}
	return removeRecordsAndFilms(tx, drop)
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
	filmCount, bySource, err := filmRowCounts(tx, row.BatchID)
	if err != nil {
		return "", err
	}
	for i := range kept {
		kept[i].FilmsTotal = bySource[kept[i].SourceID]
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
	// 进行中的批次留给结算写终态。先改成终态后，检查点和结算都不再更新这条记录。
	if row.Status == model.CollectRecordStatusRunning {
		status = row.Status
		errMsg = row.ErrorMsg
	} else if status == model.CollectRecordStatusInterrupted && errMsg == "" {
		errMsg = row.ErrorMsg
	}
	err = tx.Model(&model.CollectRecord{}).Where("id = ?", row.ID).Updates(map[string]any{
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
	}).Error
	if err != nil {
		return "", err
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
