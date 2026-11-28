package store

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"server/internal/analytics/types"
	"server/internal/infra/db"
)

// ParseDay 解析 YYYY-MM-DD 格式日期，空字符串默认为本地今日零点
func ParseDay(day string, now time.Time) (time.Time, error) {
	day = strings.TrimSpace(day)
	if day == "" {
		return StartOfLocalDay(now), nil
	}
	t, err := time.ParseInLocation("2006-01-02", day, now.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day")
	}
	return StartOfLocalDay(t), nil
}

// StartOfLocalDay 归零至本地时间当天 00:00:00
func StartOfLocalDay(t time.Time) time.Time {
	local := t.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

// IsLocalToday 判断目标日期是否为本地今天
func IsLocalToday(target, now time.Time) bool {
	return StartOfLocalDay(target).Equal(StartOfLocalDay(now))
}

// VisibleSlotCount 返回目标日期需要渲染的 15 分钟时间槽数量
func VisibleSlotCount(target, now time.Time) int {
	if IsLocalToday(target, now) {
		slot := BucketSlotIndex(now) + 1
		if slot < 1 {
			return 1
		}
		if slot > SlotsPerDay {
			return SlotsPerDay
		}
		return slot
	}
	return SlotsPerDay
}

// FoldBucketMap 将单日 Bucket Hash 折叠为有序的 15 分钟时间序列点
func FoldBucketMap(target time.Time, now time.Time, raw map[string]string) ([]types.SeriesPoint, int64) {
	nSlots := VisibleSlotCount(target, now)
	start := StartOfLocalDay(target)
	vals := ParseIntMap(raw)
	series := make([]types.SeriesPoint, 0, nSlots)
	var totalPV int64
	for i := 0; i < nSlots; i++ {
		t := start.Add(time.Duration(i*SlotMinutes) * time.Minute)
		prefix := strconv.Itoa(i) + ":"
		pv := vals[prefix+"pv"]
		ppv := vals[prefix+"provide_pv"]
		pt := types.SeriesPoint{
			T:         t.Format(time.RFC3339),
			PV:        pv + ppv,
			Err4:      vals[prefix+"err4"] + vals[prefix+"provide_err4"],
			Err5:      vals[prefix+"err5"] + vals[prefix+"provide_err5"],
			ProvidePV: ppv,
			WebPV:     vals[prefix+"web_pv"],
			AppPV:     vals[prefix+"app_pv"],
			AndroidPV: vals[prefix+"android_pv"],
			HarmonyPV: vals[prefix+"harmony_pv"],
			IosPV:     vals[prefix+"ios_pv"],
		}
		totalPV += pt.PV
		series = append(series, pt)
	}
	return series, totalPV
}

// LoadBucketSeries 从 Redis 读取单日 Bucket Hash 并折叠时间序列
func LoadBucketSeries(target, now time.Time) ([]types.SeriesPoint, int64, error) {
	if db.Rdb == nil {
		return []types.SeriesPoint{}, 0, nil
	}
	raw, err := db.Rdb.HGetAll(db.Cxt, BucketKey(target.Format("20060102"))).Result()
	if err != nil {
		return nil, 0, err
	}
	series, totalPV := FoldBucketMap(target, now, raw)
	return series, totalPV, nil
}

// ParseIntMap 将 Redis Hash 字符串值转换为 int64 映射
func ParseIntMap(m map[string]string) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		n, _ := strconv.ParseInt(v, 10, 64)
		out[k] = n
	}
	return out
}

// EstimateP95 从延迟直方图估算 P95 耗时（毫秒）
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

func MarshalIntMap(m map[string]int64) string {
	if len(m) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func UnmarshalIntMap(raw string) map[string]int64 {
	out := map[string]int64{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return map[string]int64{}
	}
	return out
}

func MarshalNestedIntMap(m map[string]map[string]int64) string {
	if len(m) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func UnmarshalNestedIntMap(raw string) map[string]map[string]int64 {
	out := map[string]map[string]int64{}
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return map[string]map[string]int64{}
	}
	return out
}

func MergeNestedIntMap(m map[string]map[string]int64) map[string]int64 {
	merged := map[string]int64{}
	for _, inner := range m {
		for k, v := range inner {
			merged[k] += v
		}
	}
	return merged
}

func MarshalSeries(series []types.SeriesPoint) string {
	if len(series) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(series)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func UnmarshalSeries(raw string) []types.SeriesPoint {
	if strings.TrimSpace(raw) == "" {
		return []types.SeriesPoint{}
	}
	var out []types.SeriesPoint
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []types.SeriesPoint{}
	}
	return out
}
