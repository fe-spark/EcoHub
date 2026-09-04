package access

import (
	"encoding/json"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
)

const (
	ttlMinute = 48 * time.Hour
	ttlDay    = 14 * 24 * time.Hour
	slowKeep  = 200
	errorKeep = 200
	zsetKeep  = 5000
)

func minKey(t time.Time) string {
	return config.AccessKeyPrefix + "min:" + t.Format("200601021504")
}
func uvKey(day string) string     { return config.AccessKeyPrefix + "uv:" + day }
func dayAggKey(day string) string { return config.AccessKeyPrefix + "day:" + day }
func clientKey(day string) string { return config.AccessKeyPrefix + "client:" + day }
func actionKey(day string) string { return config.AccessKeyPrefix + "action:" + day }
func histKey(day string) string   { return config.AccessKeyPrefix + "hist:" + day }
func topPathKey(day string) string {
	return config.AccessKeyPrefix + "top:path:" + day
}
func topSearchKey(day string) string {
	return config.AccessKeyPrefix + "top:search:" + day
}
func topPlayKey(day string) string     { return config.AccessKeyPrefix + "top:play:" + day }
func topClassifyKey(day string) string { return config.AccessKeyPrefix + "top:classify:" + day }
func recentDayKey(day string) string   { return config.AccessKeyPrefix + "recent:" + day }
func slowDayKey(day string) string     { return config.AccessKeyPrefix + "slow:" + day }
func errorDayKey(day string) string    { return config.AccessKeyPrefix + "error:" + day }
func droppedKey() string               { return config.AccessKeyPrefix + "meta:dropped" }
func droppedDayKey(day string) string  { return config.AccessKeyPrefix + "meta:dropped:" + day }
func rollupLockKey() string            { return config.AccessKeyPrefix + "lock:daily_rollup" }

// Web 专属 Key
func webPVKey(day string) string          { return config.AccessKeyPrefix + "web:pv:" + day }
func webUVKey(day string) string          { return config.AccessKeyPrefix + "web:uv:" + day }
func webTopPageKey(day string) string     { return config.AccessKeyPrefix + "web:top:page:" + day }
func webTopPlayKey(day string) string     { return config.AccessKeyPrefix + "web:top:play:" + day }
func webTopSearchKey(day string) string   { return config.AccessKeyPrefix + "web:top:search:" + day }
func webTopClassifyKey(day string) string { return config.AccessKeyPrefix + "web:top:classify:" + day }
func webActionKey(day string) string      { return config.AccessKeyPrefix + "web:action:" + day }
func webRecentDayKey(day string) string   { return config.AccessKeyPrefix + "web:recent:" + day }
func webBrowsersKey(day string) string    { return config.AccessKeyPrefix + "web:browsers:" + day }
func webOSKey(day string) string          { return config.AccessKeyPrefix + "web:os:" + day }

// App 专属 Key
func appPVKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":pv:" + day
}
func appUVKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":uv:" + day
}
func appTopPageKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":top:page:" + day
}
func appTopPlayKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":top:play:" + day
}
func appTopSearchKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":top:search:" + day
}
func appTopClassifyKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":top:classify:" + day
}
func appVersionKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":versions:" + day
}
func appAllPVKey(day string) string          { return config.AccessKeyPrefix + "app:all:pv:" + day }
func appAllUVKey(day string) string          { return config.AccessKeyPrefix + "app:all:uv:" + day }
func appAllTopPageKey(day string) string     { return config.AccessKeyPrefix + "app:all:top:page:" + day }
func appAllTopPlayKey(day string) string     { return config.AccessKeyPrefix + "app:all:top:play:" + day }
func appAllTopSearchKey(day string) string   { return config.AccessKeyPrefix + "app:all:top:search:" + day }
func appAllTopClassifyKey(day string) string { return config.AccessKeyPrefix + "app:all:top:classify:" + day }
func appActionKey(day string) string         { return config.AccessKeyPrefix + "app:action:" + day }
func appPlatformActionKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":action:" + day
}
func appPlatformsKey(day string) string { return config.AccessKeyPrefix + "app:platforms:" + day }
func appModelsKey(day string) string    { return config.AccessKeyPrefix + "app:models:" + day }
func appRecentDayKey(day string) string { return config.AccessKeyPrefix + "app:recent:" + day }

// TVBox 专属 Key
func tvboxPVKey(day string) string          { return config.AccessKeyPrefix + "tvbox:pv:" + day }
func tvboxUVKey(day string) string          { return config.AccessKeyPrefix + "tvbox:uv:" + day }
func tvboxTopPlayKey(day string) string     { return config.AccessKeyPrefix + "tvbox:top:play:" + day }
func tvboxTopSearchKey(day string) string   { return config.AccessKeyPrefix + "tvbox:top:search:" + day }
func tvboxTopClassifyKey(day string) string { return config.AccessKeyPrefix + "tvbox:top:classify:" + day }
func tvboxActionKey(day string) string      { return config.AccessKeyPrefix + "tvbox:action:" + day }
func tvboxRecentDayKey(day string) string   { return config.AccessKeyPrefix + "tvbox:recent:" + day }

func histBucket(ms int64) string {
	switch {
	case ms <= 50:
		return "b50"
	case ms <= 100:
		return "b100"
	case ms <= 200:
		return "b200"
	case ms <= 500:
		return "b500"
	case ms <= 1000:
		return "b1000"
	default:
		return "bInf"
	}
}

func writeEvent(evt *AccessEvent) {
	if evt == nil || db.Rdb == nil {
		return
	}
	if evt.Method == "PAGE" {
		writePageView(evt)
		return
	}
	if !IsProvide(evt) {
		return
	}
	ctx := db.Cxt
	ts := evt.Ts
	if ts.IsZero() {
		ts = time.Now()
	}
	ts = ts.In(time.Local)
	day := ts.Format("20060102")
	pipe := db.Rdb.Pipeline()

	mk := minKey(ts)
	dk := dayAggKey(day)
	if IsProvide(evt) {
		pipe.HIncrBy(ctx, mk, "provide_pv", 1)
		pipe.HIncrBy(ctx, dk, "provide_pv", 1)
		tpk := tvboxPVKey(day)
		pipe.Incr(ctx, tpk)
		pipe.ExpireNX(ctx, tpk, ttlDay)

		uvIdentity := eventUVIdentity(evt)

		if uvIdentity != "" {
			tuk := tvboxUVKey(day)
			pipe.PFAdd(ctx, tuk, uvIdentity)
			pipe.ExpireNX(ctx, tuk, ttlDay)

			uk := uvKey(day)
			pipe.PFAdd(ctx, uk, uvIdentity)
			pipe.ExpireNX(ctx, uk, ttlDay)
		}

		if evt.Status >= 500 {
			pipe.HIncrBy(ctx, mk, "provide_err5", 1)
			pipe.HIncrBy(ctx, dk, "provide_err5", 1)
		} else if evt.Status >= 400 {
			pipe.HIncrBy(ctx, mk, "provide_err4", 1)
			pipe.HIncrBy(ctx, dk, "provide_err4", 1)
		}
		ck := clientKey(day)
		pipe.HIncrBy(ctx, ck, "tvbox", 1)
		pipe.ExpireNX(ctx, ck, ttlDay)
		ak := actionKey(day)
		pipe.HIncrBy(ctx, ak, "provide", 1)
		if evt.Action != "" && evt.Action != "provide" {
			pipe.HIncrBy(ctx, ak, evt.Action, 1)
		}
		pipe.ExpireNX(ctx, ak, ttlDay)
		tvak := tvboxActionKey(day)
		pipe.HIncrBy(ctx, tvak, evt.Action, 1)
		pipe.ExpireNX(ctx, tvak, ttlDay)

		// 记录 TVBox 专属流水（严格保留 100 条）
		payload, err := json.Marshal(evt)
		if err == nil {
			trkDay := tvboxRecentDayKey(day)
			pipe.LPush(ctx, trkDay, payload)
			pipe.LTrim(ctx, trkDay, 0, 99)
			pipe.Expire(ctx, trkDay, ttlDay)
		}

		// TVBox 搜索词记入热搜
		if evt.Resource != "" && (evt.Action == ActionSearch || strings.Contains(evt.Query, "ac=search") || strings.Contains(evt.Query, "wd=")) {
			sk := topSearchKey(day)
			pipe.ZIncrBy(ctx, sk, 1, evt.Resource)
			pipe.ZRemRangeByRank(ctx, sk, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, sk, ttlDay)

			tvsk := tvboxTopSearchKey(day)
			pipe.ZIncrBy(ctx, tvsk, 1, evt.Resource)
			pipe.ZRemRangeByRank(ctx, tvsk, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, tvsk, ttlDay)
		}

		if evt.playMember != "" {
			plk := topPlayKey(day)
			pipe.ZIncrBy(ctx, plk, 1, evt.playMember)
			pipe.ZRemRangeByRank(ctx, plk, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, plk, ttlDay)

			tvplk := tvboxTopPlayKey(day)
			pipe.ZIncrBy(ctx, tvplk, 1, evt.playMember)
			pipe.ZRemRangeByRank(ctx, tvplk, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, tvplk, ttlDay)
		}
		if evt.Action == ActionClassify && evt.Resource != "" {
			tck := topClassifyKey(day)
			pipe.ZIncrBy(ctx, tck, 1, evt.Resource)
			pipe.ZRemRangeByRank(ctx, tck, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, tck, ttlDay)

			tvck := tvboxTopClassifyKey(day)
			pipe.ZIncrBy(ctx, tvck, 1, evt.Resource)
			pipe.ZRemRangeByRank(ctx, tvck, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, tvck, ttlDay)
		}
	}
	pipe.ExpireNX(ctx, mk, ttlMinute)
	pipe.ExpireNX(ctx, dk, ttlDay)

	if n := takeUnsyncedDropped(); n > 0 {
		dk := droppedKey()
		pipe.IncrBy(ctx, dk, n)
		pipe.ExpireNX(ctx, dk, ttlDay)

		dkDay := droppedDayKey(day)
		pipe.IncrBy(ctx, dkDay, n)
		pipe.ExpireNX(ctx, dkDay, ttlDay)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		syslog.Errorf("[Access] Redis 写入失败: %v", err)
	}
}

func writePageView(evt *AccessEvent) {
	ctx := db.Cxt
	ts := evt.Ts
	if ts.IsZero() {
		ts = time.Now()
	}
	ts = ts.In(time.Local)
	day := ts.Format("20060102")
	pipe := db.Rdb.Pipeline()
	mk := minKey(ts)
	dk := dayAggKey(day)
	pipe.HIncrBy(ctx, mk, "pv", 1)
	pipe.ExpireNX(ctx, mk, ttlMinute)
	pipe.HIncrBy(ctx, dk, "pv", 1)
	pipe.ExpireNX(ctx, dk, ttlDay)

	ck := clientKey(day)
	client := evt.ClientType
	if client == "" {
		client = "web"
	}
	pipe.HIncrBy(ctx, ck, client, 1)
	pipe.ExpireNX(ctx, ck, ttlDay)

	ak := actionKey(day)
	pipe.HIncrBy(ctx, ak, evt.Action, 1)
	pipe.ExpireNX(ctx, ak, ttlDay)

	pageTarget := evt.Page
	if pageTarget == "" {
		pageTarget = evt.Path
	}
	if pageTarget == "" {
		pageTarget = evt.Action
	}

	uvIdentity := eventUVIdentity(evt)

	// 记录真实页面热度
	pk := topPathKey(day)
	pipe.ZIncrBy(ctx, pk, 1, pageTarget)
	pipe.ZRemRangeByRank(ctx, pk, 0, int64(-zsetKeep-1))
	pipe.ExpireNX(ctx, pk, ttlDay)

	// 分端统计 (Web vs App)
	isWeb := client == "web"
	if isWeb {
		pipe.HIncrBy(ctx, mk, "web_pv", 1)
		wpk := webPVKey(day)
		pipe.Incr(ctx, wpk)
		pipe.ExpireNX(ctx, wpk, ttlDay)

		if uvIdentity != "" {
			wuk := webUVKey(day)
			pipe.PFAdd(ctx, wuk, uvIdentity)
			pipe.ExpireNX(ctx, wuk, ttlDay)
		}

		wtpk := webTopPageKey(day)
		pipe.ZIncrBy(ctx, wtpk, 1, pageTarget)
		pipe.ZRemRangeByRank(ctx, wtpk, 0, int64(-zsetKeep-1))
		pipe.ExpireNX(ctx, wtpk, ttlDay)

		browser := evt.UAFamily
		if browser == "" || browser == "web" {
			browser = "other"
		}
		wbk := webBrowsersKey(day)
		pipe.HIncrBy(ctx, wbk, browser, 1)
		pipe.ExpireNX(ctx, wbk, ttlDay)

		if evt.OS != "" {
			wok := webOSKey(day)
			pipe.HIncrBy(ctx, wok, evt.OS, 1)
			pipe.ExpireNX(ctx, wok, ttlDay)
		}

		wak := webActionKey(day)
		pipe.HIncrBy(ctx, wak, evt.Action, 1)
		pipe.ExpireNX(ctx, wak, ttlDay)

		if evt.Action == ActionSearch && evt.Resource != "" {
			wsk := webTopSearchKey(day)
			pipe.ZIncrBy(ctx, wsk, 1, evt.Resource)
			pipe.ZRemRangeByRank(ctx, wsk, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, wsk, ttlDay)
		}
		if evt.playMember != "" {
			wplk := webTopPlayKey(day)
			pipe.ZIncrBy(ctx, wplk, 1, evt.playMember)
			pipe.ZRemRangeByRank(ctx, wplk, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, wplk, ttlDay)
		}
		if evt.Action == ActionClassify && evt.Resource != "" {
			wck := webTopClassifyKey(day)
			pipe.ZIncrBy(ctx, wck, 1, evt.Resource)
			pipe.ZRemRangeByRank(ctx, wck, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, wck, ttlDay)
		}
	} else {
		pipe.HIncrBy(ctx, mk, "app_pv", 1)
		platform := client
		if platform != "android" && platform != "harmony" && platform != "ios" {
			platform = "android"
		}
		pipe.HIncrBy(ctx, mk, "app_pv_"+platform, 1)

		aak := appActionKey(day)
		pipe.HIncrBy(ctx, aak, evt.Action, 1)
		pipe.ExpireNX(ctx, aak, ttlDay)

		apak := appPlatformActionKey(platform, day)
		pipe.HIncrBy(ctx, apak, evt.Action, 1)
		pipe.ExpireNX(ctx, apak, ttlDay)

		// 单平台 PV / UV / 热门页面 / 版本
		appPlatformPV := appPVKey(platform, day)
		pipe.Incr(ctx, appPlatformPV)
		pipe.ExpireNX(ctx, appPlatformPV, ttlDay)

		if uvIdentity != "" {
			appPlatformUV := appUVKey(platform, day)
			pipe.PFAdd(ctx, appPlatformUV, uvIdentity)
			pipe.ExpireNX(ctx, appPlatformUV, ttlDay)
		}

		appPlatformTop := appTopPageKey(platform, day)
		pipe.ZIncrBy(ctx, appPlatformTop, 1, pageTarget)
		pipe.ZRemRangeByRank(ctx, appPlatformTop, 0, int64(-zsetKeep-1))
		pipe.ExpireNX(ctx, appPlatformTop, ttlDay)

		if evt.AppVersion != "" {
			appVerKey := appVersionKey(platform, day)
			pipe.HIncrBy(ctx, appVerKey, evt.AppVersion, 1)
			pipe.ExpireNX(ctx, appVerKey, ttlDay)
		}

		if evt.DeviceModel != "" {
			amk := appModelsKey(day)
			pipe.HIncrBy(ctx, amk, evt.DeviceModel, 1)
			pipe.ExpireNX(ctx, amk, ttlDay)
		}

		// App 全端聚合 (All)
		appAllPV := appAllPVKey(day)
		pipe.Incr(ctx, appAllPV)
		pipe.ExpireNX(ctx, appAllPV, ttlDay)

		if uvIdentity != "" {
			appAllUV := appAllUVKey(day)
			pipe.PFAdd(ctx, appAllUV, uvIdentity)
			pipe.ExpireNX(ctx, appAllUV, ttlDay)
		}

		appAllTop := appAllTopPageKey(day)
		pipe.ZIncrBy(ctx, appAllTop, 1, pageTarget)
		pipe.ZRemRangeByRank(ctx, appAllTop, 0, int64(-zsetKeep-1))
		pipe.ExpireNX(ctx, appAllTop, ttlDay)

		appPlatKey := appPlatformsKey(day)
		pipe.HIncrBy(ctx, appPlatKey, platform, 1)
		pipe.ExpireNX(ctx, appPlatKey, ttlDay)

		if evt.Action == ActionSearch && evt.Resource != "" {
			ask := appAllTopSearchKey(day)
			pipe.ZIncrBy(ctx, ask, 1, evt.Resource)
			pipe.ZRemRangeByRank(ctx, ask, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, ask, ttlDay)

			apsk := appTopSearchKey(platform, day)
			pipe.ZIncrBy(ctx, apsk, 1, evt.Resource)
			pipe.ZRemRangeByRank(ctx, apsk, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, apsk, ttlDay)
		}
		if evt.playMember != "" {
			aplk := appAllTopPlayKey(day)
			pipe.ZIncrBy(ctx, aplk, 1, evt.playMember)
			pipe.ZRemRangeByRank(ctx, aplk, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, aplk, ttlDay)

			applk := appTopPlayKey(platform, day)
			pipe.ZIncrBy(ctx, applk, 1, evt.playMember)
			pipe.ZRemRangeByRank(ctx, applk, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, applk, ttlDay)
		}
		if evt.Action == ActionClassify && evt.Resource != "" {
			ack := appAllTopClassifyKey(day)
			pipe.ZIncrBy(ctx, ack, 1, evt.Resource)
			pipe.ZRemRangeByRank(ctx, ack, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, ack, ttlDay)

			apck := appTopClassifyKey(platform, day)
			pipe.ZIncrBy(ctx, apck, 1, evt.Resource)
			pipe.ZRemRangeByRank(ctx, apck, 0, int64(-zsetKeep-1))
			pipe.ExpireNX(ctx, apck, ttlDay)
		}
	}

	if evt.Action == ActionSearch && evt.Resource != "" {
		sk := topSearchKey(day)
		pipe.ZIncrBy(ctx, sk, 1, evt.Resource)
		pipe.ZRemRangeByRank(ctx, sk, 0, int64(-zsetKeep-1))
		pipe.ExpireNX(ctx, sk, ttlDay)
	}
	if evt.playMember != "" {
		plk := topPlayKey(day)
		pipe.ZIncrBy(ctx, plk, 1, evt.playMember)
		pipe.ZRemRangeByRank(ctx, plk, 0, int64(-zsetKeep-1))
		pipe.ExpireNX(ctx, plk, ttlDay)
	}
	if evt.Action == ActionClassify && evt.Resource != "" {
		tck := topClassifyKey(day)
		pipe.ZIncrBy(ctx, tck, 1, evt.Resource)
		pipe.ZRemRangeByRank(ctx, tck, 0, int64(-zsetKeep-1))
		pipe.ExpireNX(ctx, tck, ttlDay)
	}

	// 分端流水（按端单写，保留 100 条）
	payload, err := json.Marshal(evt)
	if err == nil {
		if isWeb {
			wrkDay := webRecentDayKey(day)
			pipe.LPush(ctx, wrkDay, payload)
			pipe.LTrim(ctx, wrkDay, 0, 99)
			pipe.Expire(ctx, wrkDay, ttlDay)
		} else {
			arkDay := appRecentDayKey(day)
			pipe.LPush(ctx, arkDay, payload)
			pipe.LTrim(ctx, arkDay, 0, 99)
			pipe.Expire(ctx, arkDay, ttlDay)
		}
	}

	if uvIdentity != "" {
		uk := uvKey(day)
		pipe.PFAdd(ctx, uk, uvIdentity)
		pipe.ExpireNX(ctx, uk, ttlDay)
	}
	if n := takeUnsyncedDropped(); n > 0 {
		dk := droppedKey()
		pipe.IncrBy(ctx, dk, n)
		pipe.ExpireNX(ctx, dk, ttlDay)

		dkDay := droppedDayKey(day)
		pipe.IncrBy(ctx, dkDay, n)
		pipe.ExpireNX(ctx, dkDay, ttlDay)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		syslog.Errorf("[Access] 页面埋点写入失败: %v", err)
	}
}
