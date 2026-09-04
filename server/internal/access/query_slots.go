package access

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"server/internal/infra/db"

	"github.com/redis/go-redis/v9"
)

type minSlot struct {
	t   time.Time
	cmd *redis.MapStringStringCmd
}

type minuteTotals struct {
	pv, err4, err5, providePV, provideErr4, provideErr5 int64
}

func parseDay(day string, now time.Time) (time.Time, error) {
	day = strings.TrimSpace(day)
	if day == "" {
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil
	}
	t, err := time.ParseInLocation("2006-01-02", day, now.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day")
	}
	return t, nil
}

func dayStartLocal(target time.Time) time.Time {
	t := target.In(time.Local)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

func minuteSlotCount(target, now time.Time) int {
	start := dayStartLocal(target)
	now = now.In(time.Local)
	if !now.Before(start.Add(24*time.Hour + ttlMinute)) {
		return 0
	}
	if start.Year() == now.Year() && start.YearDay() == now.YearDay() {
		n := now.Hour()*60 + now.Minute() + 1
		if n < 1 {
			return 1
		}
		if n > 1440 {
			return 1440
		}
		return n
	}
	return 1440
}

func queueMinuteSlots(pipe redis.Pipeliner, target time.Time, nMin int) []minSlot {
	if nMin <= 0 || pipe == nil {
		return nil
	}
	start := dayStartLocal(target)
	slots := make([]minSlot, 0, nMin)
	ctx := db.Cxt
	for i := 0; i < nMin; i++ {
		t := start.Add(time.Duration(i) * time.Minute)
		slots = append(slots, minSlot{t: t, cmd: pipe.HGetAll(ctx, minKey(t))})
	}
	return slots
}

func foldMinuteSlots(slots []minSlot) ([]SeriesPoint, minuteTotals) {
	var tot minuteTotals
	series := make([]SeriesPoint, 0, (len(slots)+14)/15)
	var fold *SeriesPoint
	flush := func() {
		if fold != nil {
			series = append(series, *fold)
			fold = nil
		}
	}
	for _, slot := range slots {
		vals := parseIntMap(slot.cmd.Val())
		pv := vals["pv"]
		err4 := vals["err4"]
		err5 := vals["err5"]
		ppv := vals["provide_pv"]
		wpv := vals["web_pv"]
		apv := vals["app_pv"]
		anpv := vals["app_pv_android"]
		hmpv := vals["app_pv_harmony"]
		iospv := vals["app_pv_ios"]

		perr4 := vals["provide_err4"]
		perr5 := vals["provide_err5"]

		tot.pv += pv + ppv
		tot.err4 += err4 + perr4
		tot.err5 += err5 + perr5
		tot.providePV += ppv
		tot.provideErr4 += perr4
		tot.provideErr5 += perr5

		label := slot.t.Truncate(15 * time.Minute).Format(time.RFC3339)
		if fold == nil || fold.T != label {
			flush()
			fold = &SeriesPoint{T: label}
		}
		fold.PV += pv + ppv
		fold.Err4 += err4 + perr4
		fold.Err5 += err5 + perr5
		fold.ProvidePV += ppv
		fold.WebPV += wpv
		fold.AppPV += apv
		fold.AndroidPV += anpv
		fold.HarmonyPV += hmpv
		fold.IosPV += iospv
	}
	flush()
	return series, tot
}

func collectMinuteSeries(target, now time.Time) []SeriesPoint {
	if db.Rdb == nil {
		return []SeriesPoint{}
	}
	nMin := minuteSlotCount(target, now)
	if nMin <= 0 {
		return []SeriesPoint{}
	}
	pipe := db.Rdb.Pipeline()
	slots := queueMinuteSlots(pipe, target, nMin)
	if _, err := pipe.Exec(db.Cxt); err != nil && err != redis.Nil {
		return []SeriesPoint{}
	}
	series, _ := foldMinuteSlots(slots)
	if series == nil {
		return []SeriesPoint{}
	}
	return series
}

func marshalSeries(series []SeriesPoint) string {
	if len(series) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(series)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func unmarshalSeries(raw string) []SeriesPoint {
	if strings.TrimSpace(raw) == "" {
		return []SeriesPoint{}
	}
	var out []SeriesPoint
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []SeriesPoint{}
	}
	return out
}

func parseIntMap(m map[string]string) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		n, _ := strconv.ParseInt(v, 10, 64)
		out[k] = n
	}
	return out
}

func EstimateP95(hist map[string]int64) int64 {
	order := []struct {
		key   string
		upper int64
	}{
		{"bInf", 1000},
		{"b1000", 1000},
		{"b500", 500},
		{"b200", 200},
		{"b100", 100},
		{"b50", 50},
	}
	var total int64
	for _, b := range order {
		total += hist[b.key]
	}
	if total <= 0 {
		return 0
	}
	need := (total*5 + 99) / 100
	if need < 1 {
		need = 1
	}
	var acc int64
	for _, b := range order {
		acc += hist[b.key]
		if acc >= need {
			return b.upper
		}
	}
	return 50
}
