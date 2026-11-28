package store

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/analytics/enrich"
	"server/internal/analytics/types"
	"server/internal/infra/db"
	"server/internal/infra/syslog"

	"github.com/redis/go-redis/v9"
)

const (
	MaxBoundedHashFields  = 256
	maxBoundedHashDayKeys = 32
)

var (
	hashFieldGateMu sync.Mutex
	hashFieldGate   = map[string]map[string]struct{}{}
	droppedCounter  atomic.Int64
)

// AddDropped 增加内存中未同步至 Redis 的丢弃计数
func AddDropped(n int64) {
	if n > 0 {
		droppedCounter.Add(n)
	}
}

// CurrentUnsyncedDropped 读取当前尚未写入 Redis 的本地丢弃数
func CurrentUnsyncedDropped() int64 {
	return droppedCounter.Load()
}

// AdmitBoundedHashField 限制单个 Redis Hash 的不同字段数量，超出后折叠进 "other"
func AdmitBoundedHashField(redisKey, field string) string {
	field = strings.TrimSpace(field)
	if field == "" {
		return ""
	}
	hashFieldGateMu.Lock()
	defer hashFieldGateMu.Unlock()
	seen := hashFieldGate[redisKey]
	if seen == nil {
		if len(hashFieldGate) >= maxBoundedHashDayKeys {
			hashFieldGate = map[string]map[string]struct{}{}
		}
		seen = make(map[string]struct{}, 32)
		hashFieldGate[redisKey] = seen
	}
	if _, ok := seen[field]; ok {
		return field
	}
	if len(seen) >= MaxBoundedHashFields {
		return "other"
	}
	seen[field] = struct{}{}
	return field
}

// ResetBoundedHashGateForTest 清空有界字段门控（仅限测试）
func ResetBoundedHashGateForTest() {
	hashFieldGateMu.Lock()
	hashFieldGate = map[string]map[string]struct{}{}
	hashFieldGateMu.Unlock()
}

// CompactTopIntMap 截断保留 TopK 项并合并尾部至 "other"，同时控制序列化后字节上限
func CompactTopIntMap(src map[string]int64, topK, maxBytes int) map[string]int64 {
	if len(src) == 0 {
		return map[string]int64{}
	}
	type kv struct {
		k string
		v int64
	}
	items := make([]kv, 0, len(src))
	var otherTotal int64
	for k, v := range src {
		if k == "other" {
			otherTotal += v
			continue
		}
		items = append(items, kv{k: k, v: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v != items[j].v {
			return items[i].v > items[j].v
		}
		return items[i].k < items[j].k
	})
	limit := topK
	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	for i := limit; i < len(items); i++ {
		otherTotal += items[i].v
	}
	items = items[:limit]

	build := func(n int, extraOther int64) map[string]int64 {
		out := make(map[string]int64, n+1)
		for i := 0; i < n; i++ {
			out[items[i].k] = items[i].v
		}
		if extraOther > 0 {
			out["other"] += extraOther
		}
		return out
	}
	n := len(items)
	out := build(n, otherTotal)
	for maxBytes > 0 && n > 0 {
		raw, err := json.Marshal(out)
		if err == nil && len(raw) <= maxBytes {
			break
		}
		n--
		otherTotal += items[n].v
		out = build(n, otherTotal)
	}
	return out
}

// WriteBatch 将一批埋点/提供事件通过单一 Redis Pipeline 写入统计指标
func WriteBatch(batch []*types.Event) {
	if db.Rdb == nil || len(batch) == 0 {
		return
	}
	ctx := db.Cxt
	pipe := db.Rdb.Pipeline()
	var queued int
	var hasPageView bool
	var latestDay string

	for _, evt := range batch {
		if evt == nil {
			continue
		}
		enrich.SnapshotEvent(evt)
		ts := evt.Ts
		if ts.IsZero() {
			ts = time.Now()
		}
		ts = ts.In(time.Local)
		day := ts.Format("20060102")
		if evt.Method == "PAGE" {
			appendPageView(pipe, ctx, ts, day, evt)
			queued++
			hasPageView = true
			latestDay = day
			continue
		}
		if !isProvideEvent(evt) {
			continue
		}
		appendProvideEvent(pipe, ctx, ts, day, evt)
		queued++
		latestDay = day
	}
	if queued == 0 {
		return
	}
	dropped := droppedCounter.Swap(0)
	if dropped > 0 {
		dk := DroppedKey()
		pipe.IncrBy(ctx, dk, dropped)
		pipe.ExpireNX(ctx, dk, TTLDay)

		if latestDay != "" {
			dkDay := DroppedDayKey(latestDay)
			pipe.IncrBy(ctx, dkDay, dropped)
			pipe.ExpireNX(ctx, dkDay, TTLDay)
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		AddDropped(dropped)
		if hasPageView {
			syslog.Errorf("[Analytics] 统计埋点批量写入失败(n=%d): %v", queued, err)
		} else {
			syslog.Errorf("[Analytics] Redis 批量写入失败(n=%d): %v", queued, err)
		}
	}
}

func isProvideEvent(evt *types.Event) bool {
	return evt != nil && (evt.Action == types.ActionProvide || strings.HasPrefix(evt.Path, "/api/provide/"))
}

func appendProvideEvent(pipe redis.Pipeliner, ctx context.Context, ts time.Time, day string, evt *types.Event) {
	countCollectSource(pipe, ctx, day, evt.CollectSourceId)

	slot := BucketSlotIndex(ts)
	bk := BucketKey(day)
	dk := DayAggKey(day)

	pipe.HIncrBy(ctx, bk, BucketField(slot, "provide_pv"), 1)
	pipe.HIncrBy(ctx, dk, "provide_pv", 1)
	if evt.LatencyMs > 0 {
		recordLatencyHist(pipe, ctx, day, evt.LatencyMs)
	}
	if evt.Status >= 500 {
		pipe.HIncrBy(ctx, bk, BucketField(slot, "provide_err5"), 1)
		pipe.HIncrBy(ctx, dk, "provide_err5", 1)
	} else if evt.Status >= 400 {
		pipe.HIncrBy(ctx, bk, BucketField(slot, "provide_err4"), 1)
		pipe.HIncrBy(ctx, dk, "provide_err4", 1)
	}
	pipe.ExpireNX(ctx, bk, TTLDay)
	pipe.ExpireNX(ctx, dk, TTLDay)

	tpk := TvboxPVKey(day)
	pipe.Incr(ctx, tpk)
	pipe.ExpireNX(ctx, tpk, TTLDay)

	if evt.UVMember != "" {
		addUV(pipe, ctx, evt.UVMember, UVKey(day), TvboxUVKey(day))
	}

	incrHashField(pipe, ctx, ClientKey(day), "tvbox")
	incrHashField(pipe, ctx, ActionKey(day), "provide")
	if evt.Action != "" && evt.Action != "provide" {
		incrHashField(pipe, ctx, ActionKey(day), evt.Action)
	}
	incrHashField(pipe, ctx, TvboxActionKey(day), evt.Action)

	pushRecentEvent(pipe, ctx, TvboxRecentDayKey(day), evt)

	if evt.Resource != "" && (evt.Action == types.ActionSearch || strings.Contains(evt.Query, "ac=search") || strings.Contains(evt.Query, "wd=")) {
		incrTopZSet(pipe, ctx, evt.Resource, TopSearchKey(day), TvboxTopSearchKey(day))
	}
	if evt.PlayMember != "" {
		incrTopZSet(pipe, ctx, evt.PlayMember, TopPlayKey(day), TvboxTopPlayKey(day))
	}
	if member := enrich.ResolveClassifyMemberMemoryOnly(evt); member != "" {
		incrTopZSet(pipe, ctx, member, TopClassifyKey(day), TvboxTopClassifyKey(day))
	}
}

func appendPageView(pipe redis.Pipeliner, ctx context.Context, ts time.Time, day string, evt *types.Event) {
	countCollectSource(pipe, ctx, day, evt.CollectSourceId)

	slot := BucketSlotIndex(ts)
	bk := BucketKey(day)
	dk := DayAggKey(day)
	pipe.HIncrBy(ctx, bk, BucketField(slot, "pv"), 1)
	pipe.ExpireNX(ctx, bk, TTLDay)
	pipe.HIncrBy(ctx, dk, "pv", 1)
	pipe.ExpireNX(ctx, dk, TTLDay)

	client := evt.ClientType
	if client == "" {
		client = "web"
	}
	incrHashField(pipe, ctx, ClientKey(day), client)
	incrHashField(pipe, ctx, ActionKey(day), evt.Action)

	pageTarget := firstNonEmpty(evt.Page, evt.Path, evt.Action)
	incrTopZSet(pipe, ctx, pageTarget, TopPathKey(day))

	classifyMember := enrich.ResolveClassifyMemberMemoryOnly(evt)
	if client == "web" {
		pipe.HIncrBy(ctx, bk, BucketField(slot, "web_pv"), 1)
		wpk := WebPVKey(day)
		pipe.Incr(ctx, wpk)
		pipe.ExpireNX(ctx, wpk, TTLDay)

		if evt.UVMember != "" {
			addUV(pipe, ctx, evt.UVMember, UVKey(day), WebUVKey(day))
		}
		incrTopZSet(pipe, ctx, pageTarget, WebTopPageKey(day))

		browser := evt.UAFamily
		if browser == "" || browser == "web" {
			browser = "other"
		}
		incrHashField(pipe, ctx, WebBrowsersKey(day), browser)
		if evt.OS != "" {
			incrHashField(pipe, ctx, WebOSKey(day), evt.OS)
		}
		incrHashField(pipe, ctx, WebActionKey(day), evt.Action)

		if evt.Action == types.ActionSearch && evt.Resource != "" {
			incrTopZSet(pipe, ctx, evt.Resource, TopSearchKey(day), WebTopSearchKey(day))
		}
		if evt.PlayMember != "" {
			incrTopZSet(pipe, ctx, evt.PlayMember, TopPlayKey(day), WebTopPlayKey(day))
		}
		if classifyMember != "" {
			incrTopZSet(pipe, ctx, classifyMember, TopClassifyKey(day), WebTopClassifyKey(day))
		}
		pushRecentEvent(pipe, ctx, WebRecentDayKey(day), evt)
		return
	}

	platform := normalizeAppPlatform(client)
	pipe.HIncrBy(ctx, bk, BucketField(slot, "app_pv"), 1)
	pipe.HIncrBy(ctx, bk, BucketField(slot, "app_pv_"+platform), 1)

	incrHashField(pipe, ctx, AppActionKey(day), evt.Action)
	incrHashField(pipe, ctx, AppPlatformActionKey(platform, day), evt.Action)
	incrHashField(pipe, ctx, AppPlatformsKey(day), platform)

	appPlatPV := AppPVKey(platform, day)
	pipe.Incr(ctx, appPlatPV)
	pipe.ExpireNX(ctx, appPlatPV, TTLDay)

	appAllPV := AppAllPVKey(day)
	pipe.Incr(ctx, appAllPV)
	pipe.ExpireNX(ctx, appAllPV, TTLDay)

	if evt.UVMember != "" {
		addUV(pipe, ctx, evt.UVMember, UVKey(day), AppAllUVKey(day), AppUVKey(platform, day))
	}

	incrTopZSet(pipe, ctx, pageTarget, AppAllTopPageKey(day), AppTopPageKey(platform, day))

	if evt.AppVersion != "" {
		verKey := AppVersionKey(platform, day)
		if ver := AdmitBoundedHashField(verKey, evt.AppVersion); ver != "" {
			incrHashField(pipe, ctx, verKey, ver)
		}
	}
	if evt.DeviceModel != "" {
		modelKey := AppModelsKey(day)
		if modelName := AdmitBoundedHashField(modelKey, evt.DeviceModel); modelName != "" {
			incrHashField(pipe, ctx, modelKey, modelName)
		}
	}

	if evt.Action == types.ActionSearch && evt.Resource != "" {
		incrTopZSet(pipe, ctx, evt.Resource, TopSearchKey(day), AppAllTopSearchKey(day), AppTopSearchKey(platform, day))
	}
	if evt.PlayMember != "" {
		incrTopZSet(pipe, ctx, evt.PlayMember, TopPlayKey(day), AppAllTopPlayKey(day), AppTopPlayKey(platform, day))
	}
	if classifyMember != "" {
		incrTopZSet(pipe, ctx, classifyMember, TopClassifyKey(day), AppAllTopClassifyKey(day), AppTopClassifyKey(platform, day))
	}
	pushRecentEvent(pipe, ctx, AppRecentDayKey(day), evt)
}

func normalizeAppPlatform(client string) string {
	switch client {
	case "android", "harmony", "ios":
		return client
	default:
		return "android"
	}
}

func countCollectSource(pipe redis.Pipeliner, ctx context.Context, day, id string) {
	if pipe == nil || id == "" {
		return
	}
	incrHashField(pipe, ctx, CollectSourceKey(day), id)
}

func incrHashField(pipe redis.Pipeliner, ctx context.Context, key, field string) {
	if field == "" {
		return
	}
	pipe.HIncrBy(ctx, key, field, 1)
	pipe.ExpireNX(ctx, key, TTLDay)
}

func addUV(pipe redis.Pipeliner, ctx context.Context, member string, keys ...string) {
	for _, key := range keys {
		pipe.PFAdd(ctx, key, member)
		pipe.ExpireNX(ctx, key, TTLDay)
	}
}

func incrTopZSet(pipe redis.Pipeliner, ctx context.Context, member string, keys ...string) {
	for _, key := range keys {
		pipe.ZIncrBy(ctx, key, 1, member)
		pipe.ZRemRangeByRank(ctx, key, 0, int64(-ZSetKeepLimit-1))
		pipe.ExpireNX(ctx, key, TTLDay)
	}
}

func pushRecentEvent(pipe redis.Pipeliner, ctx context.Context, key string, evt *types.Event) {
	payload, err := json.Marshal(evt)
	if err != nil {
		return
	}
	pipe.LPush(ctx, key, payload)
	pipe.LTrim(ctx, key, 0, RecentKeepLimit-1)
	pipe.Expire(ctx, key, TTLDay)
}

func recordLatencyHist(pipe redis.Pipeliner, ctx context.Context, day string, latencyMs int64) {
	bucket := "bInf"
	switch {
	case latencyMs <= 50:
		bucket = "b50"
	case latencyMs <= 100:
		bucket = "b100"
	case latencyMs <= 200:
		bucket = "b200"
	case latencyMs <= 500:
		bucket = "b500"
	case latencyMs <= 1000:
		bucket = "b1000"
	}
	incrHashField(pipe, ctx, HistKey(day), bucket)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
