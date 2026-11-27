package ledger

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"
)

const (
	sourceIncidentTripAfter   = 3
	collectRecordLookbackDays = 30
	maxIncidentScanPages      = 50
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
	if now.IsZero() {
		now = time.Now()
	}
	incidents, err := listOpenSourceIncidents(now, true)
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
// 采用键集游标分批检索 + 状态闭合早停：站点一旦自愈或连续失败已经截断，就不再往下扫。
func ListOpenSourceIncidents(now time.Time) ([]model.SourceIncident, error) {
	return listOpenSourceIncidents(now, false)
}

func listOpenSourceIncidents(now time.Time, todayTripOnly bool) ([]model.SourceIncident, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	if now.IsZero() {
		now = time.Now()
	}
	since := now.AddDate(0, 0, -collectRecordLookbackDays)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	activeSourceSet := make(map[string]struct{})
	if sources, err := sourcecatalog.GetCollectSourceListDB(db.Mdb); err == nil {
		collectedSet, hasStats := loadCollectedSourceIDSet()
		for _, s := range sources {
			id := strings.TrimSpace(s.Id)
			if !s.State || id == "" {
				continue
			}
			// 若运行库已有采集统计记录，未采过任何一轮的启用新站不可能有历史故障，排除出早停阻塞集合。
			if hasStats {
				if _, collected := collectedSet[id]; !collected {
					continue
				}
			}
			activeSourceSet[id] = struct{}{}
		}
	}

	states := make(map[string]*incidentAcc)
	order := make([]string, 0)
	const batchPageSize = 100
	var (
		hasCursor    bool
		cursorAt     time.Time
		cursorID     uint
		scannedPages int
	)

	for scannedPages < maxIncidentScanPages {
		scannedPages++
		var pageRows []model.CollectRecord
		q := db.Mdb.Select("id", "batch_id", "started_at", "detail_json").
			Where("started_at >= ? AND `trigger` != ?", since, model.CollectTriggerSingleUpdate)
		if hasCursor {
			q = q.Where("started_at < ? OR (started_at = ? AND id < ?)", cursorAt, cursorAt, cursorID)
		}
		if err := q.Order("started_at DESC, id DESC").Limit(batchPageSize).Find(&pageRows).Error; err != nil {
			return nil, err
		}
		if len(pageRows) == 0 {
			break
		}

		processIncidentRecords(pageRows, states, &order)

		last := pageRows[len(pageRows)-1]
		hasCursor = true
		cursorAt = last.StartedAt
		cursorID = last.ID

		// 当日熔断判定越过今天 0 点后，仅需追完今天已失败且连续次数尚未闭合的站点即可停止。
		if todayTripOnly && last.StartedAt.Before(dayStart) {
			needMore := false
			for _, st := range states {
				if st.status == model.CollectRecordStatusFailed && sameDay(st.at, now) && !st.closed && !st.streakDone && st.consecutive < sourceIncidentTripAfter {
					needMore = true
					break
				}
			}
			if !needMore {
				break
			}
		}

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

func loadCollectedSourceIDSet() (map[string]struct{}, bool) {
	var collected []string
	if err := db.Mdb.Model(&model.CollectSourceStats{}).
		Where("last_collect_time IS NOT NULL").
		Pluck("source_id", &collected).Error; err != nil {
		return nil, false
	}
	if len(collected) == 0 {
		return nil, false
	}
	out := make(map[string]struct{}, len(collected))
	for _, id := range collected {
		if id = strings.TrimSpace(id); id != "" {
			out[id] = struct{}{}
		}
	}
	return out, len(out) > 0
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

// CountTodayCollectFailures 统计北京时间当天采集的失败页。
// 失败请求没有返回影片列表，库里没有失败部数，所以按失败页合计。进行中的批次也算。
func CountTodayCollectFailures(now time.Time) (int64, map[string]int64, error) {
	return sourcecatalog.CountTodayCollectFailuresDB(db.Mdb, now)
}
