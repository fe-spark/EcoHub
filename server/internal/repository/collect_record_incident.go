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
	streakDone  bool
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
// 最近一次已结算为失败、部分失败或异常中断都算未恢复；成功、完成和用户停止才闭合。
// 采集中的进度不算恢复。熔断只看连续整站失败，部分失败不计入熔断。
// 采用分批检索 + 状态闭合早停：站点一旦自愈或连续失败已经截断，就不再往下扫。
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

		// 活跃站点都已自愈，或连续整站失败已经遇到非失败记录，就可以停。
		if len(activeSourceSet) > 0 {
			allResolved := true
			for sid := range activeSourceSet {
				if !incidentStateResolved(states[sid]) {
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
		if st == nil || st.closed || !isOpenIncidentStatus(st.status) {
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
			Tripped:             st.status == model.CollectRecordStatusFailed && st.consecutive >= sourceIncidentTripAfter,
		})
	}
	return out, nil
}

func incidentStateResolved(st *incidentAcc) bool {
	return st != nil && st.status != "" && (st.closed || st.streakDone || st.status != model.CollectRecordStatusFailed)
}

func isInFlightDetailStatus(status string) bool {
	switch status {
	case model.CollectRecordStatusRunning, "starting", "stopping", "sealing", "waiting", "syncing":
		return true
	default:
		return false
	}
}

func isOpenIncidentStatus(status string) bool {
	switch status {
	case model.CollectRecordStatusFailed, model.CollectRecordStatusPartial, model.CollectRecordStatusInterrupted:
		return true
	default:
		return false
	}
}

func isHealedDetailStatus(status string) bool {
	switch status {
	case model.CollectRecordStatusSuccess, model.CollectRecordStatusStopped, "done":
		return true
	default:
		return false
	}
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
			status := strings.TrimSpace(d.Status)
			if id == "" || status == "" || isInFlightDetailStatus(status) {
				continue
			}
			st := states[id]
			if st == nil {
				st = &incidentAcc{}
				states[id] = st
				*order = append(*order, id)
			}
			if st.closed || st.streakDone {
				continue
			}
			if st.status == "" {
				st.name = strings.TrimSpace(d.SourceName)
				st.status = status
				st.at = rec.StartedAt
				st.errMsg = strings.TrimSpace(d.Error)
				st.batchID = rec.BatchID
				switch {
				case status == model.CollectRecordStatusFailed:
					st.consecutive = 1
				case isOpenIncidentStatus(status):
					st.consecutive = 1
					st.streakDone = true
				case isHealedDetailStatus(status):
					st.closed = true
				default:
					st.streakDone = true
				}
				continue
			}
			if status == model.CollectRecordStatusFailed {
				st.consecutive++
				continue
			}
			st.streakDone = true
		}
	}
}
