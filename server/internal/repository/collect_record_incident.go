package repository

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

const (
	sourceIncidentTripAfter   = 3
	collectRecordLookbackDays = 30
)

type incidentAcc struct {
	name        string
	status      string
	consecutive int
	at          time.Time
	errMsg      string
	batchID     string
	closed      bool
}

func sameDay(a, b time.Time) bool {
	a = a.In(b.Location())
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// TrippedSourceIDsToday 返回当日已熔断的站点集合
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

// ListOpenSourceIncidents 追溯各采集站未自愈的异常状态。
// 采用分批检索 + 状态闭合早停机制：正常情况下扫描最近 1~2 批即可提前退出，杜绝全表扫描与硬编码截断。
func ListOpenSourceIncidents(now time.Time) ([]model.SourceIncident, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	if now.IsZero() {
		now = time.Now()
	}
	since := now.AddDate(0, 0, -collectRecordLookbackDays)

	activeSourceSet := make(map[string]struct{})
	if sources, err := GetCollectSourceList(); err == nil {
		for _, s := range sources {
			if s.State && strings.TrimSpace(s.Id) != "" {
				activeSourceSet[strings.TrimSpace(s.Id)] = struct{}{}
			}
		}
	}

	states := make(map[string]*incidentAcc)
	order := make([]string, 0)
	const batchPageSize = 100
	offset := 0

	for {
		var pageRows []model.CollectRecord
		err := db.Mdb.Select("batch_id", "started_at", "detail_json").
			Where("started_at >= ? AND `trigger` != ?", since, model.CollectTriggerSingleUpdate).
			Order("started_at DESC, id DESC").
			Offset(offset).
			Limit(batchPageSize).
			Find(&pageRows).Error
		if err != nil {
			return nil, err
		}
		if len(pageRows) == 0 {
			break
		}

		processIncidentRecords(pageRows, states, &order)

		// 检查是否所有活跃站点都已完成状态结算（已闭合判定自愈或已确定连续失败）
		if len(activeSourceSet) > 0 {
			allResolved := true
			for sid := range activeSourceSet {
				st := states[sid]
				if st == nil || !st.closed {
					allResolved = false
					break
				}
			}
			if allResolved {
				break
			}
		}

		if len(pageRows) < batchPageSize {
			break
		}
		offset += batchPageSize
	}

	out := make([]model.SourceIncident, 0)
	for _, id := range order {
		st := states[id]
		if st == nil || st.status != "failed" {
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

func processIncidentRecords(rows []model.CollectRecord, states map[string]*incidentAcc, order *[]string) {
	for _, rec := range rows {
		raw := strings.TrimSpace(rec.DetailJson)
		if raw == "" || raw == "[]" || raw == "null" {
			continue
		}
		var details []model.CollectRecordSourceDetail
		if err := json.Unmarshal([]byte(raw), &details); err != nil {
			continue
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
				*order = append(*order, id)
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
}
