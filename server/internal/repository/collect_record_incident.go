package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

const (
	sourceIncidentTripAfter   = 3
	collectRecordLookbackDays = 30
)

func ListCollectRecordsSince(since time.Time) ([]model.CollectRecord, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	var list []model.CollectRecord
	err := db.Mdb.Select("batch_id", "started_at", "detail_json").
		Where("started_at >= ?", since).
		Order("started_at DESC, id DESC").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

func ListOpenSourceIncidents(now time.Time) ([]model.SourceIncident, error) {
	if now.IsZero() {
		now = time.Now()
	}
	rows, err := ListCollectRecordsSince(now.AddDate(0, 0, -collectRecordLookbackDays))
	if err != nil {
		return nil, err
	}
	return foldSourceIncidents(rows)
}

func TrippedSourceIDsToday(now time.Time) (map[string]struct{}, error) {
	incidents, err := ListOpenSourceIncidents(now)
	if err != nil {
		return nil, err
	}
	skip := make(map[string]struct{})
	for _, inc := range incidents {
		if inc.Tripped && sameDay(inc.LatestFailedAt, now) {
			skip[inc.SourceID] = struct{}{}
		}
	}
	return skip, nil
}

func sameDay(a, b time.Time) bool {
	a = a.In(b.Location())
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

type incidentAcc struct {
	name        string
	status      string
	consecutive int
	at          time.Time
	errMsg      string
	batchID     string
	closed      bool
}

func foldSourceIncidents(rows []model.CollectRecord) ([]model.SourceIncident, error) {
	states := make(map[string]*incidentAcc)
	order := make([]string, 0)
	for _, rec := range rows {
		raw := strings.TrimSpace(rec.DetailJson)
		if raw == "" || raw == "[]" || raw == "null" {
			continue
		}
		var details []model.CollectRecordSourceDetail
		if err := json.Unmarshal([]byte(raw), &details); err != nil {
			return nil, fmt.Errorf("批次 %s 明细损坏: %w", rec.BatchID, err)
		}
		for _, d := range details {
			id := strings.TrimSpace(d.SourceID)
			if id == "" {
				continue
			}
			st := states[id]
			if st == nil {
				st = &incidentAcc{}
				states[id] = st
				order = append(order, id)
			}
			if st.closed || st.status != "" && st.status != "failed" {
				continue
			}
			if st.status == "" {
				st.name = strings.TrimSpace(d.SourceName)
				st.status = d.Status
				st.at = rec.StartedAt
				st.errMsg = strings.TrimSpace(d.Error)
				st.batchID = rec.BatchID
				if d.Status == "failed" {
					st.consecutive = 1
				} else {
					st.closed = true
				}
				continue
			}
			if d.Status == "failed" {
				st.consecutive++
				continue
			}
			st.closed = true
		}
	}
	out := make([]model.SourceIncident, 0)
	for _, id := range order {
		st := states[id]
		if st.status != "failed" {
			continue
		}
		name := st.name
		if name == "" {
			name = id
		}
		out = append(out, model.SourceIncident{
			SourceID:            id,
			SourceName:          name,
			ConsecutiveFailures: st.consecutive,
			LatestFailedAt:      st.at,
			LatestError:         st.errMsg,
			LatestBatchID:       st.batchID,
			Tripped:             st.consecutive >= sourceIncidentTripAfter,
		})
	}
	return out, nil
}
