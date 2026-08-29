package access

import (
	"encoding/json"
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
	zsetKeep  = 200
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
func topPlayKey(day string) string { return config.AccessKeyPrefix + "top:play:" + day }
func recentKey() string            { return config.AccessKeyPrefix + "recent" }
func recentDayKey(day string) string { return config.AccessKeyPrefix + "recent:" + day }
func slowKey() string              { return config.AccessKeyPrefix + "slow" }
func slowDayKey(day string) string   { return config.AccessKeyPrefix + "slow:" + day }
func errorKey() string             { return config.AccessKeyPrefix + "error" }
func errorDayKey(day string) string  { return config.AccessKeyPrefix + "error:" + day }
func droppedKey() string           { return config.AccessKeyPrefix + "meta:dropped" }

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
		pipe.ExpireNX(ctx, ak, ttlDay)
	} else if httpHealthSample(evt) {
		if evt.Status >= 500 {
			pipe.HIncrBy(ctx, mk, "err5", 1)
			pipe.HIncrBy(ctx, dk, "err5", 1)
		} else if evt.Status >= 400 {
			pipe.HIncrBy(ctx, mk, "err4", 1)
			pipe.HIncrBy(ctx, dk, "err4", 1)
		}
		hk := histKey(day)
		pipe.HIncrBy(ctx, hk, histBucket(evt.LatencyMs), 1)
		pipe.ExpireNX(ctx, hk, ttlDay)
	}
	pipe.ExpireNX(ctx, mk, ttlMinute)
	pipe.ExpireNX(ctx, dk, ttlDay)

	if evt.playMember != "" {
		plk := topPlayKey(day)
		pipe.ZIncrBy(ctx, plk, 1, evt.playMember)
		pipe.ZRemRangeByRank(ctx, plk, 0, int64(-zsetKeep-1))
		pipe.ExpireNX(ctx, plk, ttlDay)
	}

	pk := topPathKey(day)
	pipe.ZIncrBy(ctx, pk, 1, evt.Method+" "+evt.Path)
	pipe.ExpireNX(ctx, pk, ttlDay)

	if RecordRecent(evt) {
		payload, err := json.Marshal(evt)
		if err == nil {
			rk := recentKey()
			pipe.LPush(ctx, rk, payload)
			pipe.LTrim(ctx, rk, 0, int64(config.AccessRecentLimit-1))
			pipe.Expire(ctx, rk, ttlDay)

			rkDay := recentDayKey(day)
			pipe.LPush(ctx, rkDay, payload)
			pipe.LTrim(ctx, rkDay, 0, int64(config.AccessRecentLimit-1))
			pipe.Expire(ctx, rkDay, ttlDay)
		}
	}
	if evt.LatencyMs >= config.AccessSlowMs {
		payload, err := json.Marshal(evt)
		if err == nil {
			sk := slowKey()
			pipe.LPush(ctx, sk, payload)
			pipe.LTrim(ctx, sk, 0, slowKeep-1)
			pipe.Expire(ctx, sk, ttlDay)

			skDay := slowDayKey(day)
			pipe.LPush(ctx, skDay, payload)
			pipe.LTrim(ctx, skDay, 0, slowKeep-1)
			pipe.Expire(ctx, skDay, ttlDay)
		}
	}
	if evt.Status >= 400 {
		payload, err := json.Marshal(evt)
		if err == nil {
			ek := errorKey()
			pipe.LPush(ctx, ek, payload)
			pipe.LTrim(ctx, ek, 0, errorKeep-1)
			pipe.Expire(ctx, ek, ttlDay)

			ekDay := errorDayKey(day)
			pipe.LPush(ctx, ekDay, payload)
			pipe.LTrim(ctx, ekDay, 0, errorKeep-1)
			pipe.Expire(ctx, ekDay, ttlDay)
		}
	}

	if n := takeUnsyncedDropped(); n > 0 {
		dk := droppedKey()
		pipe.IncrBy(ctx, dk, n)
		pipe.ExpireNX(ctx, dk, ttlDay)
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
		client = "unknown"
	}
	pipe.HIncrBy(ctx, ck, client, 1)
	pipe.ExpireNX(ctx, ck, ttlDay)

	ak := actionKey(day)
	pipe.HIncrBy(ctx, ak, evt.Action, 1)
	pipe.ExpireNX(ctx, ak, ttlDay)

	if evt.Action == "search" && evt.Resource != "" {
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
	if evt.IPHash != "" {
		uk := uvKey(day)
		pipe.PFAdd(ctx, uk, evt.IPHash)
		pipe.ExpireNX(ctx, uk, ttlDay)
	}
	if n := takeUnsyncedDropped(); n > 0 {
		dk := droppedKey()
		pipe.IncrBy(ctx, dk, n)
		pipe.ExpireNX(ctx, dk, ttlDay)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		syslog.Errorf("[Access] 页面埋点写入失败: %v", err)
	}
}
