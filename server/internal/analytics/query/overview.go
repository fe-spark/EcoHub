package query

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"server/internal/analytics/config"
	"server/internal/analytics/rollup"
	"server/internal/analytics/store"
	"server/internal/analytics/types"
	"server/internal/infra/db"
	"server/internal/model"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

const (
	overviewCacheTTL = 5 * time.Second
	overviewCacheMax = 64
)

type overviewCacheEntry struct {
	at time.Time
	ov *types.Overview
}

var (
	overviewCache sync.Map
	overviewSF    singleflight.Group
)

// ClearOverviewCache 清空实时概览短时内存缓存
func ClearOverviewCache() {
	overviewCache.Clear()
}

func overviewCacheKey(day, module, platform string) string {
	return strings.TrimSpace(day) + "|" + strings.ToLower(strings.TrimSpace(module)) + "|" + strings.ToLower(strings.TrimSpace(platform))
}

func loadOverviewCache(key string) (*types.Overview, bool) {
	val, ok := overviewCache.Load(key)
	if !ok {
		return nil, false
	}
	ent, ok := val.(overviewCacheEntry)
	if !ok || time.Since(ent.at) >= overviewCacheTTL || ent.ov == nil {
		overviewCache.Delete(key)
		return nil, false
	}
	return ent.ov, true
}

func storeOverviewCache(key string, ov *types.Overview) {
	if ov == nil {
		return
	}
	overviewCache.Store(key, overviewCacheEntry{at: time.Now(), ov: ov})
	kept := 0
	overviewCache.Range(func(k, v any) bool {
		ent, ok := v.(overviewCacheEntry)
		if !ok || time.Since(ent.at) >= overviewCacheTTL {
			overviewCache.Delete(k)
			return true
		}
		kept++
		if kept > overviewCacheMax {
			overviewCache.Delete(k)
		}
		return true
	})
}

// QueryOverviewScope 查询指定日期与终端维度的概览指标与 15 分钟时间序列
func QueryOverviewScope(day, module, platform string) (*types.Overview, error) {
	now := time.Now().In(time.Local)
	target, err := store.ParseDay(day, now)
	if err != nil {
		return nil, err
	}
	module = normalizeModule(module)
	platform = normalizePlatform(platform)
	cacheLive := store.IsLocalToday(target, now)
	cacheKey := overviewCacheKey(target.Format("2006-01-02"), module, platform)
	if cacheLive {
		if ov, ok := loadOverviewCache(cacheKey); ok {
			return ov, nil
		}
	}
	val, sfErr, _ := overviewSF.Do(cacheKey, func() (any, error) {
		if cacheLive {
			if ov, ok := loadOverviewCache(cacheKey); ok {
				return ov, nil
			}
		}
		ov, freshErr := queryOverviewScopeFresh(target, now, module, platform)
		if cacheLive && freshErr == nil && ov != nil {
			storeOverviewCache(cacheKey, ov)
		}
		return ov, freshErr
	})
	if sfErr != nil {
		return nil, sfErr
	}
	return val.(*types.Overview), nil
}

func queryOverviewScopeFresh(target, now time.Time, module, platform string) (*types.Overview, error) {
	if !store.IsLocalToday(target, now) {
		row, ok, err := rollup.LoadDailyStats(target.Format("2006-01-02"))
		if err != nil {
			return nil, err
		}
		if ok {
			out, err := overviewFromDailyScope(row, module, platform)
			if err != nil {
				return nil, err
			}
			if len(out.Series) == 0 {
				series, _, err := store.LoadBucketSeries(target, now)
				if err != nil {
					return nil, err
				}
				out.Series = applyModuleSeriesPV(series, module, platform)
			}
			return out, nil
		}
		if db.Rdb == nil {
			return emptyOverview(target), nil
		}
	}

	if db.Rdb == nil {
		if !config.IsEnabled() {
			return emptyOverview(target), nil
		}
		return nil, fmt.Errorf("redis unavailable")
	}

	dayKey := target.Format("20060102")
	out := emptyOverview(target)
	ctx := db.Cxt
	pipe := db.Rdb.Pipeline()

	clientCmd := pipe.HGetAll(ctx, store.ClientKey(dayKey))
	bucketCmd := pipe.HGetAll(ctx, store.BucketKey(dayKey))

	var (
		actionCmd     *redis.MapStringStringCmd
		uvCmd         *redis.IntCmd
		pvCmd         *redis.StringCmd
		platCmd       *redis.MapStringStringCmd
		verCmd        *redis.MapStringStringCmd
		platVerCmds   = map[string]*redis.MapStringStringCmd{}
		browserCmd    *redis.MapStringStringCmd
		osCmd         *redis.MapStringStringCmd
		modelsCmd     *redis.MapStringStringCmd
		dayCmd        *redis.MapStringStringCmd
		histCmd       *redis.MapStringStringCmd
		droppedDayCmd *redis.StringCmd
	)

	switch module {
	case types.ModuleWeb:
		uvCmd = pipe.PFCount(ctx, store.WebUVKey(dayKey))
		pvCmd = pipe.Get(ctx, store.WebPVKey(dayKey))
		browserCmd = pipe.HGetAll(ctx, store.WebBrowsersKey(dayKey))
		osCmd = pipe.HGetAll(ctx, store.WebOSKey(dayKey))
		actionCmd = pipe.HGetAll(ctx, store.WebActionKey(dayKey))
	case types.ModuleApp:
		modelsCmd = pipe.HGetAll(ctx, store.AppModelsKey(dayKey))
		if platform != "" && platform != "all" {
			uvCmd = pipe.PFCount(ctx, store.AppUVKey(platform, dayKey))
			pvCmd = pipe.Get(ctx, store.AppPVKey(platform, dayKey))
			verCmd = pipe.HGetAll(ctx, store.AppVersionKey(platform, dayKey))
			actionCmd = pipe.HGetAll(ctx, store.AppPlatformActionKey(platform, dayKey))
		} else {
			uvCmd = pipe.PFCount(ctx, store.AppAllUVKey(dayKey))
			pvCmd = pipe.Get(ctx, store.AppAllPVKey(dayKey))
			platCmd = pipe.HGetAll(ctx, store.AppPlatformsKey(dayKey))
			actionCmd = pipe.HGetAll(ctx, store.AppActionKey(dayKey))
			for _, p := range types.AppPlatforms {
				platVerCmds[p] = pipe.HGetAll(ctx, store.AppVersionKey(p, dayKey))
			}
		}
	case types.ModuleTvbox:
		uvCmd = pipe.PFCount(ctx, store.TvboxUVKey(dayKey))
		pvCmd = pipe.Get(ctx, store.TvboxPVKey(dayKey))
		dayCmd = pipe.HGetAll(ctx, store.DayAggKey(dayKey))
		actionCmd = pipe.HGetAll(ctx, store.TvboxActionKey(dayKey))
	default:
		uvCmd = pipe.PFCount(ctx, store.UVKey(dayKey))
		actionCmd = pipe.HGetAll(ctx, store.ActionKey(dayKey))
		dayCmd = pipe.HGetAll(ctx, store.DayAggKey(dayKey))
		histCmd = pipe.HGetAll(ctx, store.HistKey(dayKey))
		droppedDayCmd = pipe.Get(ctx, store.DroppedDayKey(dayKey))
	}

	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	out.UV = uvCmd.Val()
	out.Client = store.ParseIntMap(clientCmd.Val())
	if actionCmd != nil {
		out.Action = store.ParseIntMap(actionCmd.Val())
	}
	if pvCmd != nil {
		if pvVal, err := pvCmd.Int64(); err == nil {
			out.PV = pvVal
		}
	}

	series, bucketPV := store.FoldBucketMap(target, now, bucketCmd.Val())
	if module == types.ModuleAll {
		histVals := store.ParseIntMap(histCmd.Val())
		out.Hist = histVals
		out.P95Ms = store.EstimateP95(histVals)
		if n, err := droppedDayCmd.Int64(); err == nil {
			out.Dropped = n
		}
		if store.IsLocalToday(target, now) {
			out.Dropped += store.CurrentUnsyncedDropped()
		}
		dayVals := store.ParseIntMap(dayCmd.Val())
		out.PV = dayVals["pv"] + dayVals["provide_pv"]
		if bucketPV > out.PV {
			out.PV = bucketPV
		}
		out.Err4 = dayVals["err4"] + dayVals["provide_err4"]
		out.Err5 = dayVals["err5"] + dayVals["provide_err5"]
		out.Provide = types.ProvideStats{
			PV:   dayVals["provide_pv"],
			Err4: dayVals["provide_err4"],
			Err5: dayVals["provide_err5"],
		}
		out.Series = series
		return out, nil
	}

	if module == types.ModuleTvbox && dayCmd != nil {
		dayVals := store.ParseIntMap(dayCmd.Val())
		if out.PV == 0 {
			out.PV = dayVals["provide_pv"]
		}
		out.Err4 = dayVals["provide_err4"]
		out.Err5 = dayVals["provide_err5"]
		out.Provide = types.ProvideStats{
			PV:   dayVals["provide_pv"],
			Err4: dayVals["provide_err4"],
			Err5: dayVals["provide_err5"],
		}
	}

	if platCmd != nil {
		out.Platforms = store.ParseIntMap(platCmd.Val())
	}
	if verCmd != nil {
		out.Versions = store.ParseIntMap(verCmd.Val())
	} else if len(platVerCmds) > 0 {
		merged := map[string]int64{}
		for _, cmd := range platVerCmds {
			for k, v := range store.ParseIntMap(cmd.Val()) {
				merged[k] += v
			}
		}
		out.Versions = merged
	}
	if browserCmd != nil {
		out.Browsers = store.ParseIntMap(browserCmd.Val())
	}
	if osCmd != nil {
		out.OS = store.ParseIntMap(osCmd.Val())
	}
	if modelsCmd != nil {
		out.Models = store.ParseIntMap(modelsCmd.Val())
	}

	out.Series = applyModuleSeriesPV(series, module, platform)
	return out, nil
}

func overviewFromDailyScope(row model.AccessDailyStats, module, platform string) (*types.Overview, error) {
	out := &types.Overview{
		Day:     row.Day,
		PV:      row.PV,
		UV:      row.UV,
		Err4:    row.Err4,
		Err5:    row.Err5,
		P95Ms:   row.P95Ms,
		Dropped: row.Dropped,
		Provide: types.ProvideStats{
			PV:   row.ProvidePV,
			Err4: row.ProvideErr4,
			Err5: row.ProvideErr5,
		},
		Client:    store.UnmarshalIntMap(row.ClientJSON),
		Action:    store.UnmarshalIntMap(row.ActionJSON),
		Hist:      store.UnmarshalIntMap(row.HistJSON),
		Series:    store.UnmarshalSeries(row.SeriesJSON),
		Platforms: store.UnmarshalIntMap(row.PlatformJSON),
		Versions:  map[string]int64{},
		Browsers:  store.UnmarshalIntMap(row.BrowserJSON),
		OS:        store.UnmarshalIntMap(row.OSJSON),
		Models:    store.UnmarshalIntMap(row.ModelsJSON),
	}

	switch module {
	case types.ModuleWeb:
		out.PV = row.WebPV
		out.UV = row.WebUV
		out.Action = store.UnmarshalIntMap(row.WebActionJSON)
	case types.ModuleApp:
		if platform != "" && platform != "all" {
			platMap := store.UnmarshalIntMap(row.PlatformJSON)
			out.PV = platMap[platform]
			out.UV = store.UnmarshalIntMap(row.PlatformUVJSON)[platform]
			platActions := store.UnmarshalNestedIntMap(row.PlatformActionJSON)
			if act, ok := platActions[platform]; ok {
				out.Action = act
			} else {
				out.Action = map[string]int64{}
			}
		} else {
			out.PV = row.AppPV
			out.UV = row.AppUV
			out.Action = store.UnmarshalIntMap(row.AppActionJSON)
		}
	case types.ModuleTvbox:
		out.PV = row.ProvidePV
		out.UV = row.ProvideUV
		out.Err4 = row.ProvideErr4
		out.Err5 = row.ProvideErr5
		out.Action = store.UnmarshalIntMap(row.TvboxActionJSON)
	}

	out.Series = applyModuleSeriesPV(out.Series, module, platform)
	nestedVer := store.UnmarshalNestedIntMap(row.VersionJSON)
	if platform != "" && platform != "all" {
		if m := nestedVer[platform]; len(m) > 0 {
			out.Versions = m
		}
	} else {
		out.Versions = store.MergeNestedIntMap(nestedVer)
	}
	return out, nil
}

func applyModuleSeriesPV(series []types.SeriesPoint, module, platform string) []types.SeriesPoint {
	if len(series) == 0 || module == types.ModuleAll {
		return series
	}
	out := make([]types.SeriesPoint, len(series))
	copy(out, series)
	for i := range out {
		switch module {
		case types.ModuleWeb:
			out[i].PV = out[i].WebPV
		case types.ModuleTvbox:
			out[i].PV = out[i].ProvidePV
		case types.ModuleApp:
			switch platform {
			case "android":
				out[i].PV = out[i].AndroidPV
			case "harmony":
				out[i].PV = out[i].HarmonyPV
			case "ios":
				out[i].PV = out[i].IosPV
			default:
				out[i].PV = out[i].AppPV
			}
		}
	}
	return out
}

func emptyOverview(day time.Time) *types.Overview {
	return &types.Overview{
		Day:       day.Format("2006-01-02"),
		Client:    map[string]int64{},
		Action:    map[string]int64{},
		Hist:      map[string]int64{},
		Series:    []types.SeriesPoint{},
		Platforms: map[string]int64{},
		Versions:  map[string]int64{},
		Browsers:  map[string]int64{},
		Models:    map[string]int64{},
		OS:        map[string]int64{},
	}
}

func normalizeModule(module string) string {
	switch strings.ToLower(strings.TrimSpace(module)) {
	case types.ModuleWeb, types.ModuleApp, types.ModuleTvbox:
		return strings.ToLower(strings.TrimSpace(module))
	default:
		return types.ModuleAll
	}
}

func normalizePlatform(platform string) string {
	p := strings.ToLower(strings.TrimSpace(platform))
	switch p {
	case "android", "harmony", "ios":
		return p
	default:
		return "all"
	}
}
