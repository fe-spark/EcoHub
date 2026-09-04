package access

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/infra/db"

	"github.com/redis/go-redis/v9"
)

const overviewCacheTTL = 5 * time.Second

type overviewCacheEntry struct {
	at time.Time
	ov *Overview
}

var overviewCache sync.Map

func overviewCacheKey(day, module, platform string) string {
	return strings.TrimSpace(day) + "|" + strings.ToLower(strings.TrimSpace(module)) + "|" + strings.ToLower(strings.TrimSpace(platform))
}

func loadOverviewCache(key string) (*Overview, bool) {
	val, ok := overviewCache.Load(key)
	if !ok {
		return nil, false
	}
	ent, ok := val.(overviewCacheEntry)
	if !ok || time.Since(ent.at) >= overviewCacheTTL || ent.ov == nil {
		return nil, false
	}
	return ent.ov, true
}

func storeOverviewCache(key string, ov *Overview) {
	if ov == nil {
		return
	}
	overviewCache.Store(key, overviewCacheEntry{at: time.Now(), ov: ov})
}

type Overview struct {
	Day       string           `json:"day"`
	PV        int64            `json:"pv"`
	UV        int64            `json:"uv"`
	Err4      int64            `json:"err4"`
	Err5      int64            `json:"err5"`
	P95Ms     int64            `json:"p95Ms"`
	Dropped   int64            `json:"dropped"`
	Provide   ProvideStats     `json:"provide"`
	Client    map[string]int64 `json:"client"`
	Action    map[string]int64 `json:"action"`
	Hist      map[string]int64 `json:"hist"`
	Series    []SeriesPoint    `json:"series"`
	Platforms map[string]int64 `json:"platforms,omitempty"`
	Versions  map[string]int64 `json:"versions,omitempty"`
	Browsers  map[string]int64 `json:"browsers,omitempty"`
	Models    map[string]int64 `json:"models,omitempty"`
	OS        map[string]int64 `json:"os,omitempty"`
}

type ProvideStats struct {
	PV   int64 `json:"pv"`
	Err4 int64 `json:"err4"`
	Err5 int64 `json:"err5"`
}

type SeriesPoint struct {
	T         string `json:"t"`
	PV        int64  `json:"pv"`
	Err4      int64  `json:"err4"`
	Err5      int64  `json:"err5"`
	ProvidePV int64  `json:"providePv"`
	WebPV     int64  `json:"webPv,omitempty"`
	AppPV     int64  `json:"appPv,omitempty"`
	AndroidPV int64  `json:"androidPv,omitempty"`
	HarmonyPV int64  `json:"harmonyPv,omitempty"`
	IosPV     int64  `json:"iosPv,omitempty"`
}

type TopItem struct {
	Key      string `json:"key"`
	Count    int64  `json:"count"`
	Title    string `json:"title,omitempty"`
	Category string `json:"category,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Year     int64  `json:"year,omitempty"`
}

func QueryOverview(day string) (*Overview, error) {
	return QueryOverviewScope(day, "", "")
}

func QueryOverviewScope(day, module, platform string) (*Overview, error) {
	now := time.Now().In(time.Local)
	target, err := parseDay(day, now)
	cacheLive := err == nil && isLocalToday(target, now)
	cacheKey := overviewCacheKey(day, module, platform)
	if cacheLive {
		if ov, ok := loadOverviewCache(cacheKey); ok {
			return ov, nil
		}
	}
	ov, err := queryOverviewScopeFresh(day, module, platform)
	if cacheLive && err == nil && ov != nil {
		storeOverviewCache(cacheKey, ov)
	}
	return ov, err
}

func queryOverviewScopeFresh(day, module, platform string) (*Overview, error) {
	loc := time.Local
	now := time.Now().In(loc)
	target, err := parseDay(day, now)
	if err != nil {
		return nil, err
	}
	if !isLocalToday(target, now) {
		if target.Before(retentionCutoff(now)) {
			return emptyOverview(target), nil
		}
		if row, ok := loadDailyStats(target.Format("2006-01-02")); ok {
			out := overviewFromDailyScope(row, module, platform)
			if len(out.Series) == 0 {
				out.Series = collectMinuteSeries(target, now)
			}
			return out, nil
		}
		if db.Rdb == nil {
			return emptyOverview(target), nil
		}
	}
	if db.Rdb == nil {
		return nil, fmt.Errorf("redis unavailable")
	}
	dayKey := target.Format("20060102")
	out := &Overview{
		Day:       target.Format("2006-01-02"),
		Client:    map[string]int64{},
		Action:    map[string]int64{},
		Hist:      map[string]int64{},
		Series:    []SeriesPoint{},
		Platforms: map[string]int64{},
		Versions:  map[string]int64{},
		Browsers:  map[string]int64{},
		Models:    map[string]int64{},
		OS:        map[string]int64{},
		Dropped:   0,
	}

	ctx := db.Cxt
	pipe := db.Rdb.Pipeline()

	module = strings.ToLower(strings.TrimSpace(module))
	platform = strings.ToLower(strings.TrimSpace(platform))

	clientCmd := pipe.HGetAll(ctx, clientKey(dayKey))
	var actionCmd *redis.MapStringStringCmd
	var uvCmd *redis.IntCmd
	var pvCmd *redis.StringCmd
	var platCmd *redis.MapStringStringCmd
	var verCmd *redis.MapStringStringCmd
	var androidVerCmd *redis.MapStringStringCmd
	var harmonyVerCmd *redis.MapStringStringCmd
	var iosVerCmd *redis.MapStringStringCmd
	var tvboxDayCmd *redis.MapStringStringCmd
	var browserCmd *redis.MapStringStringCmd
	var osCmd *redis.MapStringStringCmd
	var modelsCmd *redis.MapStringStringCmd

	nMin := minuteSlotCount(target, now)
	slots := queueMinuteSlots(pipe, target, nMin)

	if module == "web" {
		uvCmd = pipe.PFCount(ctx, webUVKey(dayKey))
		pvCmd = pipe.Get(ctx, webPVKey(dayKey))
		browserCmd = pipe.HGetAll(ctx, webBrowsersKey(dayKey))
		osCmd = pipe.HGetAll(ctx, webOSKey(dayKey))
		actionCmd = pipe.HGetAll(ctx, webActionKey(dayKey))
	} else if module == "app" {
		modelsCmd = pipe.HGetAll(ctx, appModelsKey(dayKey))
		if platform != "" && platform != "all" {
			uvCmd = pipe.PFCount(ctx, appUVKey(platform, dayKey))
			pvCmd = pipe.Get(ctx, appPVKey(platform, dayKey))
			verCmd = pipe.HGetAll(ctx, appVersionKey(platform, dayKey))
			actionCmd = pipe.HGetAll(ctx, appPlatformActionKey(platform, dayKey))
		} else {
			uvCmd = pipe.PFCount(ctx, appAllUVKey(dayKey))
			pvCmd = pipe.Get(ctx, appAllPVKey(dayKey))
			platCmd = pipe.HGetAll(ctx, appPlatformsKey(dayKey))
			actionCmd = pipe.HGetAll(ctx, appActionKey(dayKey))
			// App 全部平台无独立版本聚合 key，实时读取三端版本哈希后合并（与日归档 nested merge 口径一致）
			androidVerCmd = pipe.HGetAll(ctx, appVersionKey("android", dayKey))
			harmonyVerCmd = pipe.HGetAll(ctx, appVersionKey("harmony", dayKey))
			iosVerCmd = pipe.HGetAll(ctx, appVersionKey("ios", dayKey))
		}
	} else if module == "tvbox" {
		uvCmd = pipe.PFCount(ctx, tvboxUVKey(dayKey))
		pvCmd = pipe.Get(ctx, tvboxPVKey(dayKey))
		tvboxDayCmd = pipe.HGetAll(ctx, dayAggKey(dayKey))
		actionCmd = pipe.HGetAll(ctx, tvboxActionKey(dayKey))
	} else {
		// 全局概览
		uvCmd = pipe.PFCount(ctx, uvKey(dayKey))
		actionCmd = pipe.HGetAll(ctx, actionKey(dayKey))
		dayCmd := pipe.HGetAll(ctx, dayAggKey(dayKey))
		histCmd := pipe.HGetAll(ctx, histKey(dayKey))
		droppedDayCmd := pipe.Get(ctx, droppedDayKey(dayKey))

		if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
			return nil, err
		}

		out.UV = uvCmd.Val()
		out.Client = parseIntMap(clientCmd.Val())
		out.Action = parseIntMap(actionCmd.Val())
		histVals := parseIntMap(histCmd.Val())
		out.Hist = histVals
		out.P95Ms = EstimateP95(histVals)
		if n, err := droppedDayCmd.Int64(); err == nil {
			out.Dropped = n
		}
		if isLocalToday(target, now) {
			if local := atomic.LoadInt64(&droppedUnsynced); local > 0 {
				out.Dropped += local
			}
		}
		dayVals := parseIntMap(dayCmd.Val())
		out.PV = dayVals["pv"] + dayVals["provide_pv"]
		out.Err4 = dayVals["err4"] + dayVals["provide_err4"]
		out.Err5 = dayVals["err5"] + dayVals["provide_err5"]
		out.Provide.PV = dayVals["provide_pv"]
		out.Provide.Err4 = dayVals["provide_err4"]
		out.Provide.Err5 = dayVals["provide_err5"]

		series, tot := foldMinuteSlots(slots)
		out.Series = series
		if tot.pv > out.PV {
			out.PV = tot.pv
		}
		return out, nil
	}

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	out.UV = uvCmd.Val()
	if pvCmd != nil {
		if pvVal, err := pvCmd.Int64(); err == nil {
			out.PV = pvVal
		}
	}
	if module == "tvbox" {
		if tvboxDayCmd != nil {
			dayVals := parseIntMap(tvboxDayCmd.Val())
			if out.PV == 0 {
				out.PV = dayVals["provide_pv"]
			}
			out.Provide.PV = dayVals["provide_pv"]
			out.Provide.Err4 = dayVals["provide_err4"]
			out.Provide.Err5 = dayVals["provide_err5"]
		}
		series, _ := foldMinuteSlots(slots)
		for i := range series {
			series[i].PV = series[i].ProvidePV
		}
		out.Series = series
		out.Client = parseIntMap(clientCmd.Val())
		out.Action = parseIntMap(actionCmd.Val())
		return out, nil
	}

	out.Client = parseIntMap(clientCmd.Val())
	out.Action = parseIntMap(actionCmd.Val())
	if platCmd != nil {
		out.Platforms = parseIntMap(platCmd.Val())
	}
	if verCmd != nil {
		out.Versions = parseIntMap(verCmd.Val())
	} else if androidVerCmd != nil {
		merged := parseIntMap(androidVerCmd.Val())
		for k, v := range parseIntMap(harmonyVerCmd.Val()) {
			merged[k] += v
		}
		for k, v := range parseIntMap(iosVerCmd.Val()) {
			merged[k] += v
		}
		out.Versions = merged
	}
	if browserCmd != nil {
		out.Browsers = parseIntMap(browserCmd.Val())
	}
	if osCmd != nil {
		out.OS = parseIntMap(osCmd.Val())
	}
	if modelsCmd != nil {
		out.Models = parseIntMap(modelsCmd.Val())
	}

	series, _ := foldMinuteSlots(slots)
	if module == "web" {
		for i := range series {
			series[i].PV = series[i].WebPV
		}
	} else if module == "app" {
		for i := range series {
			switch platform {
			case "android":
				series[i].PV = series[i].AndroidPV
			case "harmony":
				series[i].PV = series[i].HarmonyPV
			case "ios":
				series[i].PV = series[i].IosPV
			default:
				series[i].PV = series[i].AppPV
			}
		}
	}
	out.Series = series
	return out, nil
}

func emptyOverview(day time.Time) *Overview {
	return &Overview{
		Day:       day.Format("2006-01-02"),
		Client:    map[string]int64{},
		Action:    map[string]int64{},
		Hist:      map[string]int64{},
		Series:    []SeriesPoint{},
		Platforms: map[string]int64{},
		Versions:  map[string]int64{},
		Browsers:  map[string]int64{},
		Models:    map[string]int64{},
		OS:        map[string]int64{},
	}
}
