package repobase

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"server/internal/model"

	"gorm.io/gorm"
)

// CountTodayCollectFailuresDB 使用指定数据库连接统计北京时间当天采集的失败页。
func CountTodayCollectFailuresDB(connDB *gorm.DB, now time.Time) (int64, map[string]int64, error) {
	bySource := map[string]int64{}
	if connDB == nil {
		return 0, bySource, errors.New("数据库未连接")
	}
	start, end := shanghaiDayRange(now)
	var rows []model.CollectRecord
	err := connDB.Select("page_failed", "detail_json").
		Where("started_at >= ? AND started_at < ?", start, end).
		Find(&rows).Error
	if err != nil {
		return 0, bySource, err
	}
	var total int64
	for _, rec := range rows {
		pages, attributed := failurePagesBySource(rec.DetailJson)
		if !attributed {
			if rec.PageFailed > 0 {
				total += int64(rec.PageFailed)
			}
			continue
		}
		for id, n := range pages {
			if n <= 0 {
				continue
			}
			bySource[id] += n
			total += n
		}
	}
	return total, bySource, nil
}

func shanghaiDayRange(now time.Time) (time.Time, time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.Local
	}
	now = now.In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	return start, start.AddDate(0, 0, 1)
}

func failurePagesBySource(raw string) (map[string]int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" || raw == "null" {
		return nil, false
	}
	var details []model.CollectRecordSourceDetail
	if err := json.Unmarshal([]byte(raw), &details); err != nil || len(details) == 0 {
		return nil, false
	}
	out := map[string]int64{}
	for _, d := range details {
		id := strings.TrimSpace(d.SourceID)
		if id == "" {
			continue
		}
		out[id] += int64(failurePageCount(d))
	}
	return out, true
}

func failurePageCount(d model.CollectRecordSourceDetail) int {
	n := d.FailedCnt
	if pages := len(d.FailedPages); pages > n {
		n = pages
	}
	if n < 0 {
		return 0
	}
	return n
}
